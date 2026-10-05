// Package mcp implements the built-in artifact-store MCP host.
//
// Isolation model (see PLAN.md section 6): the MCP is NOT a global
// singleton. Each run gets its own scoped endpoint and a run-bound token
// at run start; sandboxes connect in using an injected mcp.json. Every
// write/read/list is scoped to that run's namespace and authorized by a
// token whose binding to run_id is verified, so run A can never see run
// B's artifacts. The token is revoked when the run ends.
package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/models"
)

// ErrUnauthorized is returned when a token does not match the run scope.
var ErrUnauthorized = errors.New("mcp: token not bound to run")

// ArtifactInfo is the listing entry returned by list_artifacts.
type ArtifactInfo struct {
	Name string `json:"name"`
	Node string `json:"node"`
	Size int    `json:"size"`
	// Note explains a synthesized entry (currently only the folded feedback
	// ledger), so a listing that stands in for many products says so.
	Note string `json:"note,omitempty"`
}

// Feedback ledger product names. Per-round products are written by the
// platform, never by an agent, and are folded behind the index in listings.
const (
	FeedbackIndexArtifactName = "feedback_index.json"
	FeedbackArtifactPrefix    = "feedback."
)

// IsFeedbackArtifactName reports whether name belongs to the feedback ledger.
func IsFeedbackArtifactName(name string) bool {
	name = strings.TrimSpace(name)
	return name == FeedbackIndexArtifactName || strings.HasPrefix(name, FeedbackArtifactPrefix)
}

// Store is the persistence backend the MCP writes through. Implemented by
// the artifact service so writes land in the platform product store.
type Store interface {
	Save(runID, nodeID, name, kind, content string) (id string, err error)
	Get(runID, name string) (content string, ok bool)
	List(runID string) []ArtifactInfo
}

// ArtifactDeleter is an optional Store capability used to drop stale
// node_complete.json when ClearOutcome runs for a new attempt/iteration.
type ArtifactDeleter interface {
	Delete(runID, name string) error
}

// Host manages per-run scoped endpoints and tokens.
type Host struct {
	mu         sync.RWMutex
	tokens     map[string]string                    // runID -> token
	active     map[string]string                    // runID -> currently-executing nodeID
	activeCaps map[string]*models.AgentCapabilities // runID -> active node's Agent capabilities
	// activeReview marks runs whose currently-executing node is in the post-run
	// ReAct review phase. While set, ask_question is permitted for the node
	// (beyond the react clarify node) so a review can raise follow-up choices.
	activeReview map[string]bool
	reviewSrc    ReviewPhaseSource
	store        Store
	// pending holds structured questions raised via the ask_question tool,
	// keyed runID -> nodeID. The engine drains them (TakePendingQuestions)
	// right after the react turn returns and persists them on the agent
	// message; they never outlive the turn that produced them.
	pending map[string]map[string][]models.ReactQuestion
	// pendingForms holds ask_form payloads for preflight (mirror of pending).
	pendingForms map[string]map[string][]models.ReactForm
	// calls buffers the built-in MCP tool invocations made during a node's
	// execution, keyed runID -> nodeID. The engine drains them (TakeMcpCalls)
	// in saveState so each StateRun row carries its own tool-call trace.
	calls map[string]map[string][]models.McpCall
	// history exposes read-only run history to the list_run_history /
	// get_history_detail tools (injected in main). Nil ⇒ tools unavailable.
	history HistoryProvider
	// tokenSrc is the persistence-backed fallback for authorize: given a runID
	// it returns the run's persisted MCP token and whether the run still has a
	// live sandbox. It lets a token outlive the in-memory registration (run
	// finished / server restarted) for exactly as long as a sandbox for the run
	// exists — i.e. token lifetime tracks sandbox lifetime. Nil ⇒ no fallback.
	tokenSrc RunTokenSource
	// activeSrc is the persistence-backed fallback for ActiveNode/ActiveCaps:
	// given a runID it resolves the run's current node id + capabilities from
	// the DB. It mirrors tokenSrc for the capability gate: when the in-memory
	// SetActiveNode registration is missing — server restarted mid-run, or the
	// MCP call is served by a replica that never executed the node (e.g. a
	// review sandbox kept alive during waiting_human) — the gate would
	// otherwise see no capabilities and wrongly reject set_preview / set_*.
	// Nil ⇒ no fallback. See ActiveCaps.
	activeSrc ActiveNodeSource
	// Preview port registrations (set_preview); keyed runID|nodeID.
	previewMem   map[string][]PreviewPort
	previewBase  string
	previewStore PreviewStore
	previewOps   PreviewSandboxOps
	// previewReady signals healthy set_preview completion (keyed runID|nodeID).
	// Closed channel = ready; absent entry = not yet signaled.
	previewReady map[string]chan struct{}
	// outcomes buffers node_complete marks keyed runID -> nodeID. The engine
	// drains them via TakeOutcome after the agent turn (destructive).
	outcomes map[string]map[string]NodeOutcome
	// outcomeAllowed gates clarify-dialogue visibility of node_complete: while
	// the active Agent is a clarify Agent and this flag is false, tools/list
	// omits the tool and tools/call treats it as unknown. Auto Agents ignore it.
	// toolsListGen increments whenever the flag flips so tests (and future SSE
	// clients) can observe a tools/list_changed refresh signal.
	outcomeAllowed map[string]bool
	toolsListGen   map[string]int
	// outcomeValidator is DefaultThenRPC (see ChainedOutcomeValidator). Nil
	// means DefaultOutcomeValidator only.
	outcomeValidator OutcomeValidator
	// afterWrite is an optional hook invoked after a successful store.Save from
	// WriteArtifact (e.g. engine syncs mapped primary outputs + pending BodyMd).
	// Nil ⇒ no-op. Must not call back into WriteArtifact.
	afterWrite AfterWriteFunc
	// artifactPreview pins a run artifact onto the react conversation (set_artifact_preview).
	artifactPreview ArtifactPreviewFunc
	// projectAudit records structured, redacted MCP tool calls into project audit.
	// Nil ⇒ no project audit (debug McpCalls still recorded separately).
	projectAudit ProjectAuditHook
	// pageBridge runs page_* tools on the turn owner's direct preview page.
	// Nil ⇒ page tools are unlisted and fail.
	pageBridge PageBridge
	// liveUpdater records live_update reports (Live variants on preview-capable Agents).
	liveUpdater LiveUpdater
}

// AfterWriteFunc is called after WriteArtifact successfully persists content.
// runID/nodeID/name/content/kind mirror the Save arguments.
type AfterWriteFunc func(runID, nodeID, name, content, kind string)

// ArtifactPreviewFunc pins an existing artifact name onto a react conversation.
type ArtifactPreviewFunc func(runID, nodeID, name string) error

// ProjectAuditHook records a project-scoped MCP tool invocation (already
// intended for redaction by the implementation). nodeID is ActiveNode at call time.
type ProjectAuditHook func(runID, nodeID, tool string, args map[string]any, resultText string, isError bool)

// RunTokenSource resolves a run's persisted MCP token and whether the run still
// has a live sandbox. ok is false when the run is unknown.
type RunTokenSource func(runID string) (token string, sandboxAlive bool, ok bool)

// ActiveNodeSource resolves a run's current node id and capabilities from
// persistence. ok is false when the run has no resolvable active node.
type ActiveNodeSource func(runID string) (nodeID string, caps *models.AgentCapabilities, ok bool)

// NewHost builds a host backed by the given store.
func NewHost(store Store) *Host {
	return &Host{
		tokens:         map[string]string{},
		active:         map[string]string{},
		activeCaps:     map[string]*models.AgentCapabilities{},
		activeReview:   map[string]bool{},
		pending:        map[string]map[string][]models.ReactQuestion{},
		pendingForms:   map[string]map[string][]models.ReactForm{},
		calls:          map[string]map[string][]models.McpCall{},
		previewMem:     map[string][]PreviewPort{},
		outcomes:       map[string]map[string]NodeOutcome{},
		outcomeAllowed: map[string]bool{},
		toolsListGen:   map[string]int{},
		store:          store,
		outcomeValidator: ChainedOutcomeValidator{
			Default: DefaultOutcomeValidator{},
		},
	}
}

// SetHistoryProvider wires the read-only run-history source used by the
// list_run_history / get_history_detail tools.
func (h *Host) SetHistoryProvider(p HistoryProvider) { h.history = p }

// SetRunTokenSource wires the persistence-backed authorize fallback (see
// RunTokenSource / authorize).
func (h *Host) SetRunTokenSource(src RunTokenSource) { h.tokenSrc = src }

// SetActiveNodeSource wires the persistence-backed ActiveNode/ActiveCaps
// fallback (see ActiveNodeSource / ActiveCaps).
func (h *Host) SetActiveNodeSource(src ActiveNodeSource) { h.activeSrc = src }

// SetAfterWriteArtifact wires a post-Save hook for WriteArtifact (engine sync).
func (h *Host) SetAfterWriteArtifact(fn AfterWriteFunc) {
	h.mu.Lock()
	h.afterWrite = fn
	h.mu.Unlock()
}

// SetArtifactPreviewHook wires persistence + WS notify for set_artifact_preview.
func (h *Host) SetArtifactPreviewHook(fn ArtifactPreviewFunc) {
	h.mu.Lock()
	h.artifactPreview = fn
	h.mu.Unlock()
}

// SetProjectAuditHook wires project-scoped MCP audit recording.
func (h *Host) SetProjectAuditHook(fn ProjectAuditHook) {
	h.mu.Lock()
	h.projectAudit = fn
	h.mu.Unlock()
}

// recordMcpCall appends a tool-call trace entry for (runID, nodeID).
func (h *Host) recordMcpCall(runID, nodeID string, call models.McpCall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.calls[runID] == nil {
		h.calls[runID] = map[string][]models.McpCall{}
	}
	h.calls[runID][nodeID] = append(h.calls[runID][nodeID], call)
}

// TakeMcpCalls returns and clears the buffered tool-call trace for
// (runID, nodeID); draining is destructive so each execution gets its own set.
func (h *Host) TakeMcpCalls(runID, nodeID string) []models.McpCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	byNode := h.calls[runID]
	if byNode == nil {
		return nil
	}
	cs := byNode[nodeID]
	delete(byNode, nodeID)
	return cs
}

// PeekMcpCalls returns the buffered tool-call trace for (runID, nodeID) without
// clearing (CAPA A7: distinguish empty MCP tool surface vs missing node_complete).
func (h *Host) PeekMcpCalls(runID, nodeID string) []models.McpCall {
	h.mu.RLock()
	defer h.mu.RUnlock()
	byNode := h.calls[runID]
	if byNode == nil {
		return nil
	}
	cs := byNode[nodeID]
	if len(cs) == 0 {
		return nil
	}
	out := make([]models.McpCall, len(cs))
	copy(out, cs)
	return out
}

// SetActiveNode records which node a run is currently executing and that
// node's Agent capabilities, so MCP writes are attributed to the producing node
// and every tool is gated by what the Agent declares. Runs execute one node at
// a time, so a single value per run is sufficient.
func (h *Host) SetActiveNode(runID, nodeID string, caps *models.AgentCapabilities) {
	h.mu.Lock()
	h.active[runID] = nodeID
	h.activeCaps[runID] = caps
	h.mu.Unlock()
}

// ActiveNode returns the run's currently-executing node, or "mcp" if unknown.
// When the in-memory registration is missing it falls back to activeSrc (see
// ActiveCaps for why the fallback matters); resolved values are re-cached so
// repeat calls in the same process stay cheap.
func (h *Host) ActiveNode(runID string) string {
	h.mu.RLock()
	n := h.active[runID]
	src := h.activeSrc
	h.mu.RUnlock()
	if n != "" {
		return n
	}
	if src != nil {
		if nodeID, caps, ok := src(runID); ok && nodeID != "" {
			h.SetActiveNode(runID, nodeID, caps)
			return nodeID
		}
	}
	return "mcp"
}

// ActiveCaps returns the capabilities of the run's currently-executing node,
// or nil when unknown (nil grants nothing).
//
// The in-memory value (SetActiveNode) is authoritative while the engine drives
// the node here. When it is missing — server restarted mid-run, or this
// replica never executed the node yet still serves the sandbox's MCP call —
// falling back to activeSrc restores the authoritative capabilities from the
// DB, so it re-gates correctly rather than weakening the gate.
func (h *Host) ActiveCaps(runID string) *models.AgentCapabilities {
	h.mu.RLock()
	nodeID := h.active[runID]
	caps := h.activeCaps[runID]
	src := h.activeSrc
	h.mu.RUnlock()
	if nodeID != "" {
		return caps
	}
	if src != nil {
		if id, c, ok := src(runID); ok && id != "" {
			h.SetActiveNode(runID, id, c)
			return c
		}
	}
	return nil
}

// SetActiveReview marks (or clears) whether a run's active node is in the
// post-run ReAct review phase, gating ask_question beyond the react node.
func (h *Host) SetActiveReview(runID string, on bool) {
	h.mu.Lock()
	if on {
		h.activeReview[runID] = true
	} else {
		delete(h.activeReview, runID)
	}
	h.mu.Unlock()
}

// ReviewPhaseSource resolves from persistence whether a run's active node has
// an open post-run review dialogue.
type ReviewPhaseSource func(runID string) bool

// SetReviewPhaseSource wires the persistence-backed InReviewPhase fallback.
func (h *Host) SetReviewPhaseSource(src ReviewPhaseSource) {
	h.mu.Lock()
	h.reviewSrc = src
	h.mu.Unlock()
}

// InReviewPhase reports whether a run's active node is in the review phase.
// The in-memory flag is lost on restart (or never set on a replica that did
// not drive the node); the review toolset depends on it, so a miss falls back
// to reviewSrc. The fallback is not cached: the flag is per run, and a cached
// answer could outlive the node whose review it described.
func (h *Host) InReviewPhase(runID string) bool {
	h.mu.RLock()
	on := h.activeReview[runID]
	src := h.reviewSrc
	h.mu.RUnlock()
	if on || src == nil {
		return on
	}
	return src(runID)
}

// SetPendingQuestions records the structured questions the agent raised this
// turn for (runID, nodeID). Replaces any prior pending set for that node.
func (h *Host) SetPendingQuestions(runID, nodeID string, qs []models.ReactQuestion) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending[runID] == nil {
		h.pending[runID] = map[string][]models.ReactQuestion{}
	}
	h.pending[runID][nodeID] = qs
}

// TakePendingQuestions returns and clears the pending questions for
// (runID, nodeID). Reading is destructive so a subsequent turn starts clean.
func (h *Host) TakePendingQuestions(runID, nodeID string) []models.ReactQuestion {
	h.mu.Lock()
	defer h.mu.Unlock()
	byNode := h.pending[runID]
	if byNode == nil {
		return nil
	}
	qs := byNode[nodeID]
	delete(byNode, nodeID)
	return qs
}

// SetPendingForms records plaintext forms the agent raised this turn via
// ask_form for (runID, nodeID). Replaces any prior pending set for that node.
func (h *Host) SetPendingForms(runID, nodeID string, forms []models.ReactForm) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pendingForms[runID] == nil {
		h.pendingForms[runID] = map[string][]models.ReactForm{}
	}
	h.pendingForms[runID][nodeID] = forms
}

// TakePendingForms returns and clears pending forms for (runID, nodeID).
func (h *Host) TakePendingForms(runID, nodeID string) []models.ReactForm {
	h.mu.Lock()
	defer h.mu.Unlock()
	byNode := h.pendingForms[runID]
	if byNode == nil {
		return nil
	}
	forms := byNode[nodeID]
	delete(byNode, nodeID)
	return forms
}

// RegisterRun provisions a scoped endpoint for a run and returns its token.
// Injected (along with the endpoint URL) into every sandbox of this run.
func (h *Host) RegisterRun(runID string) string {
	tok := newToken()
	h.mu.Lock()
	h.tokens[runID] = tok
	h.mu.Unlock()
	return tok
}

// RestoreRun re-binds a previously-issued token to a run without minting a new
// one. Used on resume after a restart (the token was persisted on the Run) so
// in-sandbox MCP calls for the resumed run authorize again. No-op for an empty
// token.
func (h *Host) RestoreRun(runID, token string) {
	if token == "" {
		return
	}
	h.mu.Lock()
	h.tokens[runID] = token
	h.mu.Unlock()
}

// UnregisterRun revokes the run token and closes the endpoint. This is the sole
// mechanism that expires a token cached by authorize's fallback, so every path
// that tears down a run's last live sandbox MUST call it (see authorize).
func (h *Host) UnregisterRun(runID string) {
	h.mu.Lock()
	delete(h.tokens, runID)
	delete(h.active, runID)
	delete(h.activeCaps, runID)
	delete(h.activeReview, runID)
	delete(h.pending, runID)
	delete(h.calls, runID)
	delete(h.outcomes, runID)
	h.mu.Unlock()
}

// authorize verifies the token is the one bound to runID. The fast path is the
// in-memory registration (run actively executing here). When that misses — the
// run finished (finish → UnregisterRun) or the server restarted with an empty
// map — it falls back to the persisted token, honoring it for as long as the
// run still has a live sandbox. This keeps the token valid for the whole
// sandbox lifetime (in-turn + post-run debug retention) so a still-alive
// sandbox agent can keep writing products instead of getting 401.
//
// INVARIANT: a successful fallback re-caches the token into h.tokens, after
// which the fast path serves it WITHOUT re-checking sandboxAlive. Expiry is
// therefore driven entirely by UnregisterRun, so every path that destroys a
// run's last live sandbox must call it; otherwise the token would stay valid
// until restart even after the sandbox is gone.
func (h *Host) authorize(runID, token string) bool {
	if token == "" {
		return false
	}
	h.mu.RLock()
	want, ok := h.tokens[runID]
	src := h.tokenSrc
	h.mu.RUnlock()
	if ok && want == token {
		return true
	}
	if src != nil {
		if persisted, alive, ok := src(runID); ok && alive && persisted != "" && persisted == token {
			h.mu.Lock()
			h.tokens[runID] = persisted
			h.mu.Unlock()
			return true
		}
	}
	return false
}

// WriteArtifact persists content under the run namespace. nodeID records
// which state produced it. Returns the artifact id.
//
// Before Save: empty kind is inferred (reserved name → expected kind, else
// extension); kind=image is always rejected (use artifact-upload →
// upload_image_artifact); an explicit kind that disagrees with a
// reserved/contract name fails without writing.
func (h *Host) WriteArtifact(runID, token, nodeID, name, content, kind string) (string, error) {
	if !h.authorize(runID, token) {
		return "", ErrUnauthorized
	}
	resolved, err := ResolveWriteArtifactKind(name, kind)
	if err != nil {
		return "", err
	}
	kind = resolved
	id, err := h.store.Save(runID, nodeID, name, kind, content)
	if err != nil {
		return "", err
	}
	h.mu.RLock()
	hook := h.afterWrite
	h.mu.RUnlock()
	if hook != nil {
		hook(runID, nodeID, name, content, kind)
	}
	return id, nil
}

// UploadImageArtifact persists a base64-encoded image under the run namespace.
// It bypasses write_artifact's kind=image rejection and is intended for the
// sandbox artifact-upload CLI (tools/call upload_image_artifact), not agents.
func (h *Host) UploadImageArtifact(runID, token, nodeID, name, content string) (string, error) {
	if !h.authorize(runID, token) {
		return "", ErrUnauthorized
	}
	name = strings.TrimSpace(name)
	content = strings.TrimSpace(content)
	if err := ValidateImageArtifactUpload(name); err != nil {
		return "", err
	}
	if content == "" {
		return "", fmt.Errorf("upload_image_artifact: content 不能为空")
	}
	raw, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return "", fmt.Errorf("upload_image_artifact: content 不是合法 base64")
	}
	if mime := http.DetectContentType(raw); !strings.HasPrefix(mime, "image/") {
		return "", fmt.Errorf("upload_image_artifact: 只接受图片，检测到 %s；HTML/文本产物请改用 write_artifact", mime)
	}
	if strings.TrimSpace(nodeID) == "" {
		nodeID = "artifact-upload"
	}
	id, err := h.store.Save(runID, nodeID, name, "image", content)
	if err != nil {
		return "", err
	}
	h.mu.RLock()
	hook := h.afterWrite
	h.mu.RUnlock()
	if hook != nil {
		hook(runID, nodeID, name, content, "image")
	}
	return id, nil
}

// artifactWriterNode returns the last recorded writer for name, or "".
func (h *Host) artifactWriterNode(runID, token, name string) string {
	infos, err := h.ListArtifacts(runID, token)
	if err != nil {
		return ""
	}
	for _, info := range infos {
		if info.Name == name && strings.TrimSpace(info.Node) != "" {
			return info.Node
		}
	}
	return ""
}

// ReadArtifact returns a previously-written artifact within the same run.
func (h *Host) ReadArtifact(runID, token, name string) (string, error) {
	if !h.authorize(runID, token) {
		return "", ErrUnauthorized
	}
	content, ok := h.store.Get(runID, name)
	if !ok {
		return "", errors.New("mcp: artifact not found")
	}
	return content, nil
}

// deleteArtifactIfPresent drops name when the store supports ArtifactDeleter.
// Used to clear leftover root_cause.json after work_kind leaves bug.
func (h *Host) deleteArtifactIfPresent(runID, token, name string) {
	if !h.authorize(runID, token) {
		return
	}
	h.DeleteArtifact(runID, name)
}

// DeleteArtifact removes a stored artifact by name when the store supports it.
// No-op when unauthorized capability is missing or the name is absent.
func (h *Host) DeleteArtifact(runID, name string) {
	d, ok := h.store.(ArtifactDeleter)
	if !ok {
		return
	}
	_ = d.Delete(runID, name)
}

// ListArtifacts lists products of the current run only, with the per-round
// feedback ledger folded behind its index.
func (h *Host) ListArtifacts(runID, token string) ([]ArtifactInfo, error) {
	if !h.authorize(runID, token) {
		return nil, ErrUnauthorized
	}
	return foldFeedbackArtifacts(h.store.List(runID)), nil
}

// foldFeedbackArtifacts collapses feedback.* into the index entry.
//
// A long review can produce dozens of rounds; listing each one would bury the
// actual deliverables in bookkeeping. The index is the documented entry point,
// and it names every round for read_artifact, so nothing becomes unreachable.
func foldFeedbackArtifacts(in []ArtifactInfo) []ArtifactInfo {
	out := make([]ArtifactInfo, 0, len(in))
	folded, indexAt := 0, -1
	for _, a := range in {
		switch {
		case a.Name == FeedbackIndexArtifactName:
			indexAt = len(out)
			out = append(out, a)
		case strings.HasPrefix(a.Name, FeedbackArtifactPrefix):
			folded++
		default:
			out = append(out, a)
		}
	}
	if folded == 0 {
		return out
	}
	entry := ArtifactInfo{
		Name: FeedbackIndexArtifactName,
		Note: fmt.Sprintf("人工反馈台账:已折叠 %d 轮单轮产物,读本索引取轮次清单与产物名", folded),
	}
	if indexAt >= 0 {
		entry.Node = out[indexAt].Node
		entry.Size = out[indexAt].Size
		out[indexAt] = entry
		return out
	}
	return append(out, entry)
}

// PlanIncomplete returns descriptions of the run plan's not-yet-done leaf items
// (empty when the plan is complete). It errors when there is no plan or it does
// not parse; the implement-node completion loop treats those as "nothing to
// enforce".
func (h *Host) PlanIncomplete(runID, token string) ([]string, error) {
	if !h.authorize(runID, token) {
		return nil, ErrUnauthorized
	}
	content, ok := h.store.Get(runID, PlanArtifactName)
	if !ok {
		return nil, errors.New("mcp: no plan")
	}
	var doc planDoc
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	return planIncomplete(doc), nil
}

// newToken mints a per-run MCP bearer token. crypto/rand failing is
// catastrophic and must never yield a weak/predictable (all-zero) token that
// would let any sandbox forge another run's identity — so we panic rather than
// silently degrade authorization.
func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("mcp: crypto/rand unavailable, cannot mint run token: %v", err))
	}
	return hex.EncodeToString(b)
}
