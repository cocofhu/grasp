package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// NodeOutcomeArtifactName is the optional audit artifact written on node_complete.
const NodeOutcomeArtifactName = "node_complete.json"

// OutcomeStatus is the agent-reported completion status.
const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"
)

// OutcomeCheck is an optional agent self-attestation entry (platform does not
// interpret business semantics).
type OutcomeCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// NodeOutcome is the agent-reported completion mark for the active node.
type NodeOutcome struct {
	Status  string         `json:"status"`
	Summary string         `json:"summary,omitempty"`
	Error   string         `json:"error,omitempty"`
	Outputs map[string]any `json:"outputs,omitempty"`
	Checks  []OutcomeCheck `json:"checks,omitempty"`
}

// OutcomeValidateIn is the input to OutcomeValidator.
type OutcomeValidateIn struct {
	RunID    string
	NodeID   string
	NodeType string
	Outcome  NodeOutcome
}

// OutcomeValidateOut is the validator result.
type OutcomeValidateOut struct {
	Accept       bool
	Message      string
	OutputsPatch map[string]any
}

// OutcomeValidator validates a reported (or engine-finalized) node outcome.
// Default checks always run first; RPC validators are chained after Default
// via ChainedOutcomeValidator and must not replace Default.
type OutcomeValidator interface {
	Validate(ctx context.Context, in OutcomeValidateIn) (OutcomeValidateOut, error)
}

// DefaultOutcomeValidator validates the node_complete payload shape.
// It does not run git/glab (submit_mr verifyMR) or business RPC.
// Product existence and quality gates remain engine DefaultChecks.
type DefaultOutcomeValidator struct{}

func (DefaultOutcomeValidator) Validate(_ context.Context, in OutcomeValidateIn) (OutcomeValidateOut, error) {
	st := strings.TrimSpace(strings.ToLower(in.Outcome.Status))
	if st != OutcomeSuccess && st != OutcomeFailed {
		return OutcomeValidateOut{
			Accept:  false,
			Message: "status 必须是 success 或 failed",
		}, nil
	}
	return OutcomeValidateOut{Accept: true}, nil
}

// ChainedOutcomeValidator runs Default first; only on Accept does it call RPC.
// RPC may be nil (no business validator configured).
type ChainedOutcomeValidator struct {
	Default OutcomeValidator // required
	RPC     OutcomeValidator // optional
}

func (c ChainedOutcomeValidator) Validate(ctx context.Context, in OutcomeValidateIn) (OutcomeValidateOut, error) {
	if c.Default == nil {
		return OutcomeValidateOut{Accept: false, Message: "default outcome validator missing"}, nil
	}
	out, err := c.Default.Validate(ctx, in)
	if err != nil || !out.Accept {
		return out, err
	}
	if c.RPC == nil {
		return out, nil
	}
	rpcOut, rpcErr := c.RPC.Validate(ctx, in)
	if rpcErr != nil || !rpcOut.Accept {
		return rpcOut, rpcErr
	}
	// Merge patches: Default first, RPC overrides.
	if len(out.OutputsPatch) > 0 || len(rpcOut.OutputsPatch) > 0 {
		merged := map[string]any{}
		for k, v := range out.OutputsPatch {
			merged[k] = v
		}
		for k, v := range rpcOut.OutputsPatch {
			merged[k] = v
		}
		rpcOut.OutputsPatch = merged
	}
	if rpcOut.Message == "" {
		rpcOut.Message = out.Message
	}
	return rpcOut, nil
}

// SetOutcomeValidator wires the chained validator used by node_complete and
// engine post-default RPC. Nil resets to Default-only.
func (h *Host) SetOutcomeValidator(v OutcomeValidator) {
	h.mu.Lock()
	h.outcomeValidator = v
	h.mu.Unlock()
}

// outcomeValidatorOrDefault returns the configured validator or Default-only chain.
func (h *Host) outcomeValidatorOrDefault() OutcomeValidator {
	h.mu.RLock()
	v := h.outcomeValidator
	h.mu.RUnlock()
	if v != nil {
		return v
	}
	return ChainedOutcomeValidator{Default: DefaultOutcomeValidator{}}
}

// SetRPCOutcomeValidator installs RPC as the second stage after Default.
// Passing nil clears RPC (Default-only).
func (h *Host) SetRPCOutcomeValidator(rpc OutcomeValidator) {
	h.SetOutcomeValidator(ChainedOutcomeValidator{
		Default: DefaultOutcomeValidator{},
		RPC:     rpc,
	})
}

// PeekOutcome returns the buffered outcome for (runID, nodeID) without clearing.
func (h *Host) PeekOutcome(runID, nodeID string) (NodeOutcome, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	byNode := h.outcomes[runID]
	if byNode == nil {
		return NodeOutcome{}, false
	}
	o, ok := byNode[nodeID]
	return o, ok
}

// TakeOutcome returns and clears the buffered outcome for (runID, nodeID).
func (h *Host) TakeOutcome(runID, nodeID string) (NodeOutcome, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	byNode := h.outcomes[runID]
	if byNode == nil {
		return NodeOutcome{}, false
	}
	o, ok := byNode[nodeID]
	if ok {
		delete(byNode, nodeID)
	}
	return o, ok
}

// ClearOutcome drops any buffered node_complete mark for (runID, nodeID).
// Call only at a new attempt / new iteration (or equivalent fresh node visit)
// so a mark from a failed/retried turn cannot satisfy ensureOutcome or
// TakeOutcome later. Same-visit react multi-round replies must NOT call this.
// Best-effort: also deletes the audit node_complete.json so artifact adoption
// cannot revive a stale mark across attempts.
func (h *Host) ClearOutcome(runID, nodeID string) {
	h.mu.Lock()
	var (
		o  NodeOutcome
		ok bool
	)
	if byNode := h.outcomes[runID]; byNode != nil {
		o, ok = byNode[nodeID]
		if ok {
			delete(byNode, nodeID)
		}
	}
	h.mu.Unlock()
	if ok {
		log.Info().Str("run_id", runID).Str("node_id", nodeID).
			Str("status", o.Status).Str("summary", o.Summary).
			Msg("cleared stale node_complete mark before new attempt")
	}
	h.clearOutcomeArtifact(runID)
}

// SetOutcomeAllowed controls whether Grasp may see/call node_complete for this
// run. Flipping the flag bumps ToolsListGeneration (list_changed signal).
// Non-Grasp nodes ignore the flag and always expose the tool.
func (h *Host) SetOutcomeAllowed(runID string, allowed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.outcomeAllowed[runID]
	if prev == allowed {
		return
	}
	h.outcomeAllowed[runID] = allowed
	h.toolsListGen[runID]++
	log.Info().Str("run_id", runID).Bool("allowed", allowed).
		Int("tools_list_gen", h.toolsListGen[runID]).
		Msg("grasp outcome tool surface refreshed")
}

// OutcomeAllowed reports whether Grasp Phase2 has opened the outcome tool for
// this run. Defaults to false (Phase1 zero-visibility).
func (h *Host) OutcomeAllowed(runID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.outcomeAllowed[runID]
}

// ToolsListGeneration is a monotonic counter bumped when the Grasp outcome
// tool surface changes. Tests (and future SSE clients) treat a bump as a
// tools/list_changed refresh signal.
func (h *Host) ToolsListGeneration(runID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.toolsListGen[runID]
}

// hideNodeComplete reports whether tools/list and tools/call must treat
// node_complete as absent for this run (Grasp Phase1 only).
func (h *Host) hideNodeComplete(runID string) bool {
	if !isGrasp(h.ActiveNodeType(runID)) {
		return false
	}
	return !h.OutcomeAllowed(runID)
}

// listedTools returns the MCP tools/list payload, omitting node_complete when
// Grasp Phase1 must not know about it.
func (h *Host) listedTools(runID string) []map[string]any {
	all := artifactTools()
	if !h.hideNodeComplete(runID) {
		return all
	}
	out := make([]map[string]any, 0, len(all))
	for _, t := range all {
		if name, _ := t["name"].(string); name == "node_complete" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// clearOutcomeArtifact drops the audit node_complete.json when the store
// supports deletion (production ArtifactService). No-op for stores that do not.
func (h *Host) clearOutcomeArtifact(runID string) {
	if h == nil || h.store == nil || runID == "" {
		return
	}
	d, ok := h.store.(ArtifactDeleter)
	if !ok {
		return
	}
	if err := d.Delete(runID, NodeOutcomeArtifactName); err != nil {
		log.Warn().Err(err).Str("run_id", runID).
			Msg("clear node_complete.json before new attempt failed")
	}
}

// PeekOutcomeArtifact reads and classifies the audit node_complete.json for a
// run without authorizing a token (engine / ensure paths). Missing → absent.
func (h *Host) PeekOutcomeArtifact(runID string) (NodeOutcome, OutcomeArtifactState) {
	if h == nil || h.store == nil || runID == "" {
		return NodeOutcome{}, OutcomeArtifactAbsent
	}
	content, ok := h.store.Get(runID, NodeOutcomeArtifactName)
	if !ok {
		return NodeOutcome{}, OutcomeArtifactAbsent
	}
	return ClassifyOutcomeArtifact(content)
}

// RestoreOutcomeFromArtifact adopts a parseable node_complete.json into the
// Host outcome buffer when memory mark is missing. Returns true when restored.
func (h *Host) RestoreOutcomeFromArtifact(runID, nodeID string) bool {
	o, state := h.PeekOutcomeArtifact(runID)
	if state != OutcomeArtifactAdoptable {
		return false
	}
	if nodeID == "" {
		nodeID = h.ActiveNode(runID)
	}
	if nodeID == "" || nodeID == "mcp" {
		return false
	}
	h.storeOutcome(runID, nodeID, o)
	log.Info().Str("run_id", runID).Str("node_id", nodeID).
		Str("status", o.Status).
		Msg("restored node_complete mark from artifact")
	return true
}

// OutcomeArtifactState classifies node_complete.json for CAPA A7 evidence.
type OutcomeArtifactState int

const (
	OutcomeArtifactAbsent OutcomeArtifactState = iota
	OutcomeArtifactAdoptable
	OutcomeArtifactCorrupt
)

// ClassifyOutcomeArtifact parses audit JSON. Only status success|failed is adoptable.
func ClassifyOutcomeArtifact(content string) (NodeOutcome, OutcomeArtifactState) {
	content = strings.TrimSpace(content)
	if content == "" {
		return NodeOutcome{}, OutcomeArtifactAbsent
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return NodeOutcome{}, OutcomeArtifactCorrupt
	}
	o, err := ParseNodeOutcome(raw)
	if err != nil {
		return NodeOutcome{}, OutcomeArtifactCorrupt
	}
	return o, OutcomeArtifactAdoptable
}

// HasOutcome reports whether a non-cleared outcome exists for the node.
func (h *Host) HasOutcome(runID, nodeID string) bool {
	_, ok := h.PeekOutcome(runID, nodeID)
	return ok
}

// storeOutcome locks and writes the outcome buffer (last write wins).
func (h *Host) storeOutcome(runID, nodeID string, o NodeOutcome) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.outcomes[runID] == nil {
		h.outcomes[runID] = map[string]NodeOutcome{}
	}
	h.outcomes[runID][nodeID] = o
}

// ValidateOutcome runs the chained validator (Default then optional RPC).
func (h *Host) ValidateOutcome(ctx context.Context, in OutcomeValidateIn) (OutcomeValidateOut, error) {
	return h.outcomeValidatorOrDefault().Validate(ctx, in)
}

// ParseNodeOutcome builds a NodeOutcome from MCP tool arguments.
func ParseNodeOutcome(args map[string]any) (NodeOutcome, error) {
	st := strings.TrimSpace(strings.ToLower(asString(args["status"])))
	if st != OutcomeSuccess && st != OutcomeFailed {
		return NodeOutcome{}, fmt.Errorf("status 必须是 success 或 failed")
	}
	o := NodeOutcome{
		Status:  st,
		Summary: strings.TrimSpace(asString(args["summary"])),
		Error:   strings.TrimSpace(asString(args["error"])),
	}
	if raw, ok := args["outputs"].(map[string]any); ok && len(raw) > 0 {
		o.Outputs = raw
	}
	if checks := parseOutcomeChecks(args["checks"]); len(checks) > 0 {
		o.Checks = checks
	}
	return o, nil
}

func parseOutcomeChecks(v any) []OutcomeCheck {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]OutcomeCheck, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(asString(m["name"]))
		if name == "" {
			continue
		}
		out = append(out, OutcomeCheck{
			Name:   name,
			Passed: asBool(m["passed"]),
			Detail: strings.TrimSpace(asString(m["detail"])),
		})
	}
	return out
}

// MergeOutcomeOutputs copies outcome.outputs into dst (creating dst if nil).
func MergeOutcomeOutputs(dst map[string]any, o NodeOutcome) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range o.Outputs {
		dst[k] = v
	}
	if o.Summary != "" {
		dst["outcome_summary"] = o.Summary
	}
	if o.Status != "" {
		dst["outcome_status"] = o.Status
	}
	return dst
}

// OutcomeJSON returns a stable JSON encoding for audit artifacts.
func OutcomeJSON(o NodeOutcome) string {
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
