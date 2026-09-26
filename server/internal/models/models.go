package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// EnvEntry is one project sandbox OS environment variable (key/value + secret flag).
// Secret only affects API/UI read masking; the DB stores plaintext for runtime injection.
// Enabled=nil means default ON (legacy JSON without the field stays injectable).
type EnvEntry struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Secret  bool   `json:"secret,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// IsEnabled reports whether this entry should be injected into the sandbox.
// Missing Enabled (nil) defaults to true so upgrades stay opt-out, not silent.
func (e EnvEntry) IsEnabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

// ProjectVariable is a project-level workflow variable definition (vars.* namespace).
// Shape aligns with Variable, plus an optional Secret flag for read masking.
type ProjectVariable struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // string | paragraph | number | bool | select
	Value    any    `json:"value,omitempty"`
	Desc     string `json:"desc,omitempty"`
	Ask      bool   `json:"ask,omitempty"`
	Required bool   `json:"required,omitempty"`
	Editable bool   `json:"editable,omitempty"`
	Options  string `json:"options,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
}

// Project is a workspace that owns workflows and holds workflow variable
// defaults. Project-level OS env lives in the shared Agent config (extend
// layer); Run.SandboxEnv remains the per-run snapshot.
type Project struct {
	ID          string `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"uniqueIndex" json:"name"`
	Description string `json:"description"`
	// SandboxEnv is deprecated legacy storage cleared by MigrateProjectSandboxEnvOnce.
	// Kept on the model so upgrades can read then wipe; API no longer exposes it.
	SandboxEnv []EnvEntry        `gorm:"serializer:json" json:"-"`
	Variables  []ProjectVariable `gorm:"serializer:json" json:"variables"`
	// PmLeaderEnabled toggles the project-level PM Leader consult entry.
	PmLeaderEnabled bool `json:"pmLeaderEnabled"`
	// PmLeaderAgent is the bound Agent config name (agent_profile). Empty when
	// unbound. Enabling requires a non-empty, existing agent name.
	PmLeaderAgent string `json:"pmLeaderAgent,omitempty"`
	// PmEnabledMcps lists enabled PM-only MCP ids (pm-progress, pm-workflow-read, pm-workflow-write, pm-agent-fs, pm-prd-manager).
	// nil/omitted → defaults; explicit empty slice → none.
	// nil/unset means both enabled by default; explicit empty disables all.
	PmEnabledMcps []string `gorm:"serializer:json" json:"pmEnabledMcps,omitempty"`
	// PmGateAutoVar is the project variable name that gates "auto-invoke PM on
	// human gates". Empty disables the capability. Runtime requires the named
	// var to exist in the run and be truthy; save-time existence/type checks
	// are intentionally not enforced.
	PmGateAutoVar string `json:"gateAutoVar,omitempty"`
	// PmGateAutoPrompt is an optional prompt appended after the system default
	// gate-auto guidance when invoking the PM Leader.
	PmGateAutoPrompt string `json:"gateAutoPrompt,omitempty"`
	// NotifyPolicy is the project-level Run→IM notification default
	// (enabled kill-switch + defaultEvents). See ProjectNotifyPolicy.
	NotifyPolicy ProjectNotifyPolicy `gorm:"serializer:json" json:"notifyPolicy"`
	// UnknownModelDisplayName is an optional project-level display alias for the
	// 「未知/未分桶」token bucket. Empty means use the default label. Does not
	// change persisted UsageByModel keys or merge with real model buckets.
	UnknownModelDisplayName string    `json:"unknownModelDisplayName,omitempty"`
	CreatedAt               time.Time `json:"createdAt"`
	UpdatedAt               time.Time `json:"updatedAt"`
}

// WorkflowDef is the editable workflow (draft or published head).
type WorkflowDef struct {
	ID string `gorm:"primaryKey" json:"id"`
	// ProjectID is required by the API/service layer. It is intentionally not
	// tagged not null: SQLite rejects ALTER TABLE … ADD COLUMN … NOT NULL with
	// no non-NULL default when the table already has rows (preview PVC upgrade).
	// database.ensureDefaultProject backfills legacy rows after AutoMigrate.
	ProjectID   string `gorm:"index;uniqueIndex:idx_wf_proj_name" json:"projectId"`
	Name        string `gorm:"uniqueIndex:idx_wf_proj_name" json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"` // draft | published
	Version     int    `json:"version"`
	NeedsRepo   bool   `json:"needsRepo"`
	// ShowOnHome gates Home cards + Home pipeline search. Missing/zero = false
	// so existing rows stay hidden after AutoMigrate (plan g1.1).
	ShowOnHome bool `json:"showOnHome"`
	// NotifyPolicy is the workflow-level override (off|inherit|custom + events).
	// Zero value (empty mode) is treated as inherit at resolve time.
	NotifyPolicy WorkflowNotifyPolicy `gorm:"serializer:json" json:"notifyPolicy"`
	Graph        Graph                `gorm:"serializer:json" json:"-"`
	CreatedAt    time.Time            `json:"-"`
	UpdatedAt    time.Time            `json:"updatedAt"`
	LastRunAt    *time.Time           `json:"lastRunAt,omitempty"`
}

// WorkflowVersion is an immutable published snapshot pinned by in-flight runs.
type WorkflowVersion struct {
	ID          uint      `gorm:"primaryKey" json:"-"`
	WorkflowID  string    `gorm:"index" json:"workflowId"`
	Version     int       `json:"version"`
	Graph       Graph     `gorm:"serializer:json" json:"-"`
	PublishedAt time.Time `json:"publishedAt"`
}

// Run is one workflow execution (FSM instance).
type Run struct {
	ID              string         `gorm:"primaryKey" json:"id"`
	WorkflowID      string         `gorm:"index" json:"workflowId"`
	WorkflowName    string         `json:"workflowName"`
	WorkflowVersion int            `json:"workflowVersion"`
	Status          string         `json:"status"` // queued|running|waiting_human|completed|failed|cancelled
	Trigger         string         `json:"trigger"`
	Inputs          map[string]any `gorm:"serializer:json" json:"inputs"`
	Tags            []string       `gorm:"serializer:json" json:"tags"`
	// Priority is the admission weight: high=3, normal=2, low=1 (default 2).
	// API/frontend expose string labels; claim sorts by Priority DESC then
	// remaining-human_gate then FIFO (see engine.claimNextQueued). List UI order
	// is independent and must not follow the claim secondary key.
	Priority int `gorm:"not null;default:2;index" json:"-"`
	// McpToken is the run-scoped artifact-store MCP token, persisted so a run
	// paused for human input (gate / react) can be resumed after a server
	// restart: loadCtx re-registers it with the MCP host from here instead of
	// failing every subsequent artifact write with ErrUnauthorized.
	McpToken string `json:"-"`
	// SandboxEnv is the immutable run-scoped sandbox OS env snapshot taken at
	// StartRun (optional). Injected into this Run's pipeline node sandboxes
	// after Agent env and before platform reserved/auth write-backs. Plaintext
	// in DB for injection; GET/audit must mask Secret entries.
	SandboxEnv []EnvEntry `gorm:"serializer:json" json:"sandboxEnv,omitempty"`
	// FirstMessage is the launcher's opening message (text + attachments) for an
	// approve-first pipeline. The engine delivers it into the approve node's
	// sandbox once the node parks, so the caller does not have to poll for the
	// pause and re-send. FirstMessageDeliveredAt is the delivery latch: a
	// conditional UPDATE on it guarantees exactly-once delivery.
	FirstMessage            *CompositeText `gorm:"serializer:json" json:"-"`
	FirstMessageDeliveredAt *time.Time     `json:"-"`
	Attempt                 int            `json:"attempt"`
	Progress                float64        `json:"progress"`
	Branch                  string         `json:"branch,omitempty"`
	Title                   string         `json:"title,omitempty"`
	Trace                   []TraceEntry   `gorm:"serializer:json" json:"trace"`
	// Checkpoints holds variable snapshots keyed by checkpoint node id, used
	// to restore state on rollback.
	Checkpoints map[string]map[string]any `gorm:"serializer:json" json:"-"`
	Graph       Graph                     `gorm:"serializer:json" json:"-"`
	StartedAt   time.Time                 `json:"startedAt"`
	DurationSec int                       `json:"durationSec"`
	CreatedAt   time.Time                 `json:"-"`
}

const (
	MaxRunTags     = 8
	MaxRunTagRunes = 32
)

var ErrInvalidRunTag = errors.New("invalid run tag")

func NormalizeRunTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > MaxRunTagRunes {
			return nil, fmt.Errorf("%w %q: exceeds %d characters", ErrInvalidRunTag, tag, MaxRunTagRunes)
		}
		for _, r := range tag {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
			switch r {
			case '_', '-', '.', '/':
				continue
			default:
				return nil, fmt.Errorf("%w %q: unsupported character %q", ErrInvalidRunTag, tag, r)
			}
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) > MaxRunTags {
			return nil, fmt.Errorf("too many run tags: max %d", MaxRunTags)
		}
	}
	return out, nil
}

// TokenUsage is per-node-execution LLM token accounting (input/output/cache).
// A nil *TokenUsage means "not reported" (UI shows —); a non-nil value — even
// when every counter is 0 — means the provider explicitly reported usage.
type TokenUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

// Total returns input+output+cacheRead+cacheWrite.
func (u TokenUsage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// AddTokenUsage returns dst+src component-wise. nil sources are ignored; the
// first non-nil source establishes presence (including an all-zero report).
func AddTokenUsage(dst, src *TokenUsage) *TokenUsage {
	if src == nil {
		return dst
	}
	if dst == nil {
		cp := *src
		return &cp
	}
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	return dst
}

// CloneTokenUsage returns a shallow copy, or nil when src is nil.
func CloneTokenUsage(src *TokenUsage) *TokenUsage {
	if src == nil {
		return nil
	}
	cp := *src
	return &cp
}

// StateRun is a single node execution record. It is genuinely append-only: the
// FSM writes a fresh row every time it (re-)enters a node, so a node that runs
// multiple times (loop-back / gate revise / rollback retry) keeps every past
// execution's output, events, and duration instead of overwriting them.
// Iteration is the 1-based per-node execution index (1 = first visit).
type StateRun struct {
	ID        uint           `gorm:"primaryKey" json:"-"`
	RunID     string         `gorm:"index" json:"runId"`
	NodeID    string         `json:"nodeId"`
	NodeType  string         `json:"nodeType"`
	Iteration int            `json:"iteration"`
	Status    string         `json:"status"`
	OutputMd  string         `json:"outputMd"`
	Outputs   map[string]any `gorm:"serializer:json" json:"outputs"`
	// VarsSnapshot is the global-variable state captured when this execution
	// finished (post-node), so the run timeline can show each card's variables
	// at that moment for debugging.
	VarsSnapshot map[string]any `gorm:"serializer:json" json:"varsSnapshot,omitempty"`
	// VarsBefore is the global-variable state captured when this execution
	// STARTED (pre-node). A manual resume restores it so a re-run of the node
	// sees exactly the state it had at that time, rather than values that later
	// nodes have since mutated.
	VarsBefore map[string]any `gorm:"serializer:json" json:"varsBefore,omitempty"`
	Events     []AcpEvent     `gorm:"serializer:json" json:"events"`
	// McpCalls records the built-in MCP tool invocations made during this
	// execution (tool name + truncated in/out), so the run timeline can show
	// what the agent asked the platform to do — for debugging.
	McpCalls []McpCall `gorm:"serializer:json" json:"mcpCalls,omitempty"`
	// Usage is the per-execution token total accumulated from prompt_done.usage
	// across chat turns in this StateRun. nil = provider never reported usage.
	Usage *TokenUsage `gorm:"serializer:json" json:"usage,omitempty"`
	// UsageByModel is the per-model breakdown after ingest weak-key merge /
	// ACP_BRIDGE_MODEL backfill. nil = legacy / not reported by model; when
	// Usage is set but UsageByModel is nil, readers map Usage →「未知/未分桶」.
	UsageByModel TokenUsageByModel `gorm:"serializer:json" json:"usageByModel,omitempty"`
	Error        string            `json:"error,omitempty"`
	Attempt      int               `json:"attempt"`
	StartedAt    *time.Time        `json:"startedAt,omitempty"`
	DurationSec  int               `json:"durationSec"`
}

// McpCall is one built-in MCP tool invocation captured during a node execution.
// Args/Result are truncated compact strings (debugging aid, not full fidelity).
type McpCall struct {
	At      string `json:"at"`
	Tool    string `json:"tool"`
	Args    string `json:"args,omitempty"`
	Result  string `json:"result,omitempty"`
	IsError bool   `json:"isError,omitempty"`
}

// RunVariable is a global variable's live value within a run.
type RunVariable struct {
	ID    uint   `gorm:"primaryKey" json:"-"`
	RunID string `gorm:"index" json:"runId"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `gorm:"serializer:json" json:"value"`
}

// Artifact is a platform-stored run product written via the artifact-store MCP.
type Artifact struct {
	ID           string    `gorm:"primaryKey" json:"id"`
	RunID        string    `gorm:"index" json:"runId"`
	NodeID       string    `json:"nodeId"`
	WorkflowID   string    `gorm:"index" json:"workflowId"`
	WorkflowName string    `json:"workflowName"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	SizeBytes    int       `json:"sizeBytes"`
	Content      string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	// UpdatedAt bumps on every Save upsert so clients can detect external edits
	// (ETag / concurrency banners) without a content hash round-trip.
	UpdatedAt time.Time `json:"updatedAt"`
	// Revision counts same-name overwrites (1 on first write).
	Revision int `json:"revision"`
	// RunTitle is populated by list queries that LEFT JOIN runs (read-only).
	RunTitle string `gorm:"->;column:run_title" json:"runTitle,omitempty"`
}

// ArtifactVersion is a superseded Artifact.Content snapshot. One row per
// same-name overwrite; the live Artifact row is always the highest revision.
type ArtifactVersion struct {
	ID         uint   `gorm:"primaryKey" json:"-"`
	ArtifactID string `gorm:"index:idx_artver_art_rev,priority:1" json:"artifactId"`
	RunID      string `gorm:"index" json:"runId"`
	// NodeID is the node that wrote THIS version, not the one that replaced it.
	NodeID    string `json:"nodeId"`
	Revision  int    `gorm:"index:idx_artver_art_rev,priority:2" json:"revision"`
	Kind      string `json:"kind"`
	SizeBytes int    `json:"sizeBytes"`
	Content   string `json:"-"`
	// CreatedAt is when this version was superseded (the old row's UpdatedAt).
	CreatedAt time.Time `json:"createdAt"`
}

// Gate is a pending human decision (set when a human_gate node pauses).
// Iteration is the per-node execution index this gate belongs to, so a run that
// loops back onto the same gate opens a fresh decision each time (each visit is
// re-approvable) rather than being blocked by the previous resolution.
type Gate struct {
	ID           uint         `gorm:"primaryKey" json:"-"`
	RunID        string       `gorm:"index" json:"runId"`
	NodeID       string       `json:"nodeId"`
	Iteration    int          `json:"iteration"`
	WorkflowID   string       `gorm:"index" json:"workflowId"`
	WorkflowName string       `json:"workflowName"`
	Title        string       `json:"title"`
	BodyMd       string       `json:"bodyMd"`
	Actions      []GateAction `gorm:"serializer:json" json:"actions"`
	Form         []GateField  `gorm:"serializer:json" json:"form"`
	// UpstreamNodeID + UpstreamIteration bind the gate preview to the upstream
	// execution that was current when the gate opened (page/page.html ref
	// preferred). Empty on legacy gates and gates with no body_template refs.
	UpstreamNodeID    string    `json:"upstreamNodeId,omitempty"`
	UpstreamIteration int       `json:"upstreamIteration,omitempty"`
	Resolved          bool      `json:"resolved"`
	RequestedAt       time.Time `json:"requestedAt"`
}

// GateAction is a button on a human gate. Goto, when set, routes the run
// directly to that node id when this action is chosen (branch-style routing);
// when empty the engine falls back to edge guards evaluated against `action`.
// RequireForm, when set, forces every gate form field to be filled before this
// action can be submitted (e.g. a "reject" action that mandates a comment).
type GateAction struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Goto        string `json:"goto,omitempty"`
	RequireForm bool   `json:"requireForm,omitempty"`
}

// GateField is a form field on a human gate.
type GateField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required,omitempty"`
}

// ReactConversation persists a react node's platform-native multi-turn
// dialogue (replaces auto-coder's host @bot mechanism).
type ReactConversation struct {
	ID     uint   `gorm:"primaryKey" json:"-"`
	RunID  string `gorm:"index" json:"runId"`
	NodeID string `json:"nodeId"`
	// Iteration is the per-node execution index this dialogue belongs to, so a
	// loop-back onto the same react node opens a fresh conversation each visit.
	Iteration int            `json:"iteration"`
	Done      bool           `json:"done"`
	Messages  []ReactMessage `gorm:"serializer:json" json:"turns"`
	// PreviewArtifact is the artifact name pinned by set_artifact_preview for the ReAct UI.
	PreviewArtifact string `json:"previewArtifact,omitempty"`
}

// Turns returns the transcript as a non-nil slice so JSON clients always get [].
func (c ReactConversation) Turns() []ReactMessage {
	if c.Messages == nil {
		return []ReactMessage{}
	}
	return c.Messages
}

// ReactMessage is one turn in a react conversation.
type ReactMessage struct {
	Role string `json:"role"` // agent | human
	Text string `json:"text"`
	At   string `json:"at"`
	// Images are optional base64 attachments the human sent with this turn.
	// Persisted so the run detail can re-render thumbnails after a refresh.
	Images []PromptImage `json:"images,omitempty"`
	// Questions are structured multiple-choice questions the agent raised this
	// turn via the ask_question MCP tool (clarify-only). Persisted so the UI can
	// render selectable choices and re-render them after a refresh. Empty on a
	// turn where the agent asked nothing structured — an agent turn with no
	// questions signals the clarification is finished.
	Questions []ReactQuestion `json:"questions,omitempty"`
	// Forms are plaintext env-collection forms raised via ask_form (preflight).
	// Persisted like Questions so the inbox can re-render after refresh. A turn
	// with Forms (or Questions) keeps the dialogue paused for human input.
	Forms []ReactForm `json:"forms,omitempty"`
	// Annotations are the precise references a human attached to this turn
	// during a post-run ReAct review: a JSON path into a structured product
	// (e.g. functional_requirements[f3].priority) or a DOM CSS selector into a
	// visual page, each with an optional note. Rendered into the review prompt
	// so the agent edits exactly the cited spot. Persisted for re-render.
	Annotations []ReactAnnotation `json:"annotations,omitempty"`
	// Interrupted marks an agent turn that was stopped mid-stream by 轮级 Cancel.
	// Partial narration is retained; the session stays parked for further edits.
	Interrupted bool `json:"interrupted,omitempty"`
}

// ReactAnnotation is one precise reference a human attached to a review turn.
// JSONPath (structured product field), Selector (visual DOM element), and/or
// Quote (paragraph excerpt from a text selection) may be set; Note carries the
// human's instruction for that spot. Truncated marks soft-capped quotes.
// URL is the page location.href at DOM pick time (SPA navigations).
// TagName / Text / OuterHTML describe a picked DOM element so the agent can
// tell repeated components apart; they are clipped when rendered.
type ReactAnnotation struct {
	JSONPath  string `json:"jsonPath,omitempty"`
	Selector  string `json:"selector,omitempty"`
	URL       string `json:"url,omitempty"`
	Label     string `json:"label,omitempty"`
	Note      string `json:"note,omitempty"`
	Quote     string `json:"quote,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	TagName   string `json:"tagName,omitempty"`
	Text      string `json:"text,omitempty"`
	OuterHTML string `json:"outerHTML,omitempty"`
}

// Match web PICK_TEXT_MAX / PICK_HTML_MAX; clients are not trusted to clip.
const (
	annotationTextMaxRunes = 120
	annotationHTMLMaxRunes = 1024
)

func clipRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func writePickContext(b *strings.Builder, a ReactAnnotation) {
	tag := strings.TrimSpace(a.TagName)
	text := clipRunes(strings.Join(strings.Fields(a.Text), " "), annotationTextMaxRunes)
	switch {
	case tag != "" && text != "":
		fmt.Fprintf(b, "   标签: %s · 可见文本: 「%s」\n", tag, text)
	case tag != "":
		fmt.Fprintf(b, "   标签: %s\n", tag)
	case text != "":
		fmt.Fprintf(b, "   可见文本: 「%s」\n", text)
	}
	if html := strings.TrimSpace(a.OuterHTML); html != "" {
		html = clipRunes(strings.Join(strings.Fields(html), " "), annotationHTMLMaxRunes)
		fmt.Fprintf(b, "   HTML: %s\n", html)
	}
}

// RenderAnnotations renders review annotations into a structured prompt block
// the agent can act on precisely, or "" when there are none.
func RenderAnnotations(anns []ReactAnnotation) string {
	if len(anns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 本轮标注(请精确修改被引用处)\n")
	for i, a := range anns {
		quote := strings.TrimSpace(a.Quote)
		ref := strings.TrimSpace(a.JSONPath)
		kind := "字段路径"
		if ref == "" {
			ref = strings.TrimSpace(a.Selector)
			kind = "页面元素"
		}
		label := strings.TrimSpace(a.Label)
		if label != "" {
			label = "(" + label + ")"
		}
		note := strings.TrimSpace(a.Note)
		if note == "" {
			note = "(见下方文字说明)"
		}
		pageURL := strings.TrimSpace(a.URL)
		if quote != "" {
			if ref == "" {
				fmt.Fprintf(&b, "%d. [段落摘录] 引用原文: 「%s」", i+1, quote)
				if a.Truncated {
					b.WriteString(" (已截断)")
				}
				if label != "" {
					fmt.Fprintf(&b, " %s", label)
				}
				fmt.Fprintf(&b, " → %s\n", note)
				continue
			}
			fmt.Fprintf(&b, "%d. [%s] `%s`%s\n   引用原文: 「%s」", i+1, kind, ref, label, quote)
			if a.Truncated {
				b.WriteString(" (已截断)")
			}
			fmt.Fprintf(&b, " → %s\n", note)
			if kind == "页面元素" {
				if pageURL != "" {
					fmt.Fprintf(&b, "   页面 URL: %s\n", pageURL)
				}
				writePickContext(&b, a)
			}
			continue
		}
		if ref == "" {
			fmt.Fprintf(&b, "%d. [未绑定] %s → %s\n", i+1, label, note)
			continue
		}
		fmt.Fprintf(&b, "%d. [%s] `%s`%s → %s\n", i+1, kind, ref, label, note)
		if kind == "页面元素" {
			if pageURL != "" {
				fmt.Fprintf(&b, "   页面 URL: %s\n", pageURL)
			}
			writePickContext(&b, a)
		}
	}
	return b.String()
}

// ReactQuestion is one structured question the agent raised during
// clarification, rendered by the UI as a single/multi choice card.
type ReactQuestion struct {
	ID            string        `json:"id"`
	Prompt        string        `json:"prompt"`
	Options       []ReactOption `json:"options"`
	AllowMultiple bool          `json:"allowMultiple,omitempty"`
}

// ReactForm is one plaintext form the agent raised via ask_form (preflight).
type ReactForm struct {
	Title  string           `json:"title,omitempty"`
	Fields []ReactFormField `json:"fields"`
}

// ReactFormField is one input in a ReactForm. Type is text|url only (default text);
// passwords are ordinary plaintext — no password type / secret flag.
type ReactFormField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type,omitempty"` // text|url
	Placeholder string `json:"placeholder,omitempty"`
	Value       string `json:"value,omitempty"`
	Required    bool   `json:"required,omitempty"`
	// Why is the hover reason (plan gap explanation); agents may send "why" or "reason".
	Why string `json:"why,omitempty"`
}

// ReactOption is one selectable answer of a ReactQuestion.
type ReactOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Recommended marks this option as the agent's suggested choice. For
	// single-select (!AllowMultiple) at most one option should be recommended;
	// for multi-select, one or more may be recommended. The UI highlights them
	// and auto-select (auto_var) prefers all recommended options. When none are
	// recommended, the first option is chosen as the fallback.
	Recommended bool `json:"recommended,omitempty"`
	// DemoHtml is an optional self-contained HTML document (<!doctype html>…) for
	// UI/layout visual decisions. Rendered in the clarify card iframe preview.
	DemoHtml string `json:"demoHtml,omitempty"`
}

// SelectRecommendedOptions resolves the auto-selected option set for a question.
// When AllowMultiple is true, every recommended option is returned; when false,
// at most the first recommended option is returned. With zero recommendations
// the first option is the fallback. Returns false when the question has no
// options at all.
func SelectRecommendedOptions(q ReactQuestion) ([]ReactOption, bool) {
	if len(q.Options) == 0 {
		return nil, false
	}
	if q.AllowMultiple {
		picked := make([]ReactOption, 0, len(q.Options))
		for _, o := range q.Options {
			if o.Recommended {
				picked = append(picked, o)
			}
		}
		if len(picked) == 0 {
			return []ReactOption{q.Options[0]}, true
		}
		return picked, true
	}
	for _, o := range q.Options {
		if o.Recommended {
			return []ReactOption{o}, true
		}
	}
	return []ReactOption{q.Options[0]}, true
}

// Choice-summary prefixes emitted by the clarify UI (zh-CN + en) and
// FormatChoiceReply. Used to recognize a structured "我的选择" human turn.
const (
	ChoiceReplyPrefixZH = "我的选择:"
	ChoiceReplyPrefixEN = "My choices:"
)

// Skip-envelope first lines (zh-CN + en). Composer send while ask_question is
// unanswered wraps human.text as three lines: skip label / user-reply label /
// original body. Must not be treated as IsChoiceReply.
const (
	SkipReplyPrefixZH = "回答已跳过"
	SkipReplyPrefixEN = "Answers skipped"
)

// IsChoiceReply reports whether text is a structured choice-card summary
// (not free-text). Prefix match is exact at the start — same as the UI.
func IsChoiceReply(text string) bool {
	return strings.HasPrefix(text, ChoiceReplyPrefixZH) || strings.HasPrefix(text, ChoiceReplyPrefixEN)
}

// IsSkipReply reports whether text is a composer skip envelope (not a choice
// summary). Match requires the label followed by a newline so a free-text line
// that merely mentions the phrase is not treated as skip.
func IsSkipReply(text string) bool {
	return strings.HasPrefix(text, SkipReplyPrefixZH+"\n") || strings.HasPrefix(text, SkipReplyPrefixEN+"\n")
}

// FormatChoiceReply builds the human reply text an auto-select would submit,
// matching the UI's "我的选择:\n- 问题 → 选项" format so the transcript reads the
// same whether the choice came from a human click or an automatic pick. Each
// question resolves to its recommended option set (or the first as fallback);
// multi-select labels are joined with "、", matching ClarifyChat.submitChoices.
func FormatChoiceReply(questions []ReactQuestion) string {
	lines := make([]string, 0, len(questions))
	for _, q := range questions {
		opts, ok := SelectRecommendedOptions(q)
		if !ok {
			continue
		}
		labels := make([]string, len(opts))
		for i, o := range opts {
			labels[i] = o.Label
		}
		lines = append(lines, "- "+q.Prompt+" → "+strings.Join(labels, "、"))
	}
	if len(lines) == 0 {
		return ""
	}
	return "我的选择:\n" + strings.Join(lines, "\n")
}

// PromptImage is a chat/run attachment. New persistence uses Ref (blob:{id})
// with bytes in BlobStore; Data is base64 only on inbound requests and when
// hydrating for ACP wire format. Name is optional; when empty the Bridge falls
// back to attachment-N.ext.
type PromptImage struct {
	Data      string `json:"data,omitempty"`      // base64 (no data: prefix); cleared before DB write
	Ref       string `json:"ref,omitempty"`       // blob:{id} after externalization
	MimeType  string `json:"mimeType"`            // e.g. image/png or application/pdf
	Name      string `json:"name,omitempty"`      // original filename when known
	SizeBytes int64  `json:"sizeBytes,omitempty"` // decoded byte length when known
}

// Sandbox is a tracked sandbox container. Purposes share this table:
//   - "test":  long-lived interactive sandbox for Agent chat-testing.
//   - "run":   per-run workflow node sandbox, recorded for its (short) lifetime
//     so it shows in the sandbox UI; the runtime provider owns its lifecycle.
//   - "agent": thread-bound agent session sandbox (consult / cron / future roles).
//   - "pm":    legacy alias of "agent" (older PM Leader consult rows).
type Sandbox struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	Name           string `gorm:"uniqueIndex" json:"name"` // docker container name
	Profile        string `json:"profile"`                 // bound Agent name
	Purpose        string `json:"purpose"`                 // "test" | "run" | "agent" | "pm"(legacy)
	Status         string `json:"status"`                  // pulling|creating|running|stopped|error
	Host           string `json:"-"`
	ACPPort        int    `json:"-"`
	CodeServerPort int    `json:"-"`
	RepoURL        string `json:"repoUrl,omitempty"`
	// Origin attribution (populated for purpose="run"): which run/workflow/node
	// produced this sandbox. RunID for test sandboxes is a synthetic id used by
	// the artifact-store MCP. For purpose="agent"|"pm", RunID is a synthetic
	// agent-{project}-{thread} id used by platform MCP token binding, and
	// ProjectID / ThreadID identify the session thread.
	RunID        string `json:"runId,omitempty"`
	WorkflowID   string `json:"workflowId,omitempty"`
	WorkflowName string `json:"workflowName,omitempty"`
	NodeID       string `json:"nodeId,omitempty"`
	ProjectID    string `json:"projectId,omitempty"`
	ThreadID     string `json:"threadId,omitempty"`
	Token        string `json:"-"`
	// HomeDir is the host cursor-home dir bind-mounted at /root/.cursor (run
	// sandboxes only). Retained while the container lives so it can be cleaned
	// up when the sandbox is finally destroyed.
	HomeDir string `json:"-"`
	// Error carries the startup failure reason when Status == "error".
	Error     string     `json:"error,omitempty"`
	DestroyAt *time.Time `json:"destroyAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// SandboxLog is an archived snapshot of a sandbox container's stdout/stderr
// (docker logs), captured just before the container is torn down so the raw
// startup/exec output survives for post-mortem troubleshooting (e.g. a failed
// git clone). Keyed by container name; run sandboxes also carry run/node ids.
type SandboxLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex" json:"name"` // docker container name
	RunID     string    `gorm:"index" json:"runId,omitempty"`
	NodeID    string    `json:"nodeId,omitempty"`
	Profile   string    `json:"profile,omitempty"`
	Content   string    `json:"content"` // captured docker logs (tail-capped)
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Session is a server-side login session persisted in SQLite/MySQL.
type Session struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"index" json:"username"`
	ExpiresAt time.Time `gorm:"index" json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// WorkflowAPIKey is a per-workflow external API credential. Plaintext is shown
// only once at creation; only KeyHash is persisted (bcrypt). Revoked keys
// (RevokedAt set) are rejected immediately.
type WorkflowAPIKey struct {
	ID         string     `gorm:"primaryKey" json:"id"`
	WorkflowID string     `gorm:"index" json:"workflowId"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"keyPrefix"` // masked display suffix (last 4 chars)
	KeyHash    string     `json:"-"`
	CreatedAt  time.Time  `json:"createdAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// Setting is a single UI-editable platform knob (key/value), forming a DB
// override layer above the read-only config file. Value is a JSON-encoded
// scalar so ints/bools/strings share one table. Only a small fixed set of keys
// is used (platform scheduling params); see services.SettingsService.
type Setting struct {
	Key       string    `gorm:"primaryKey" json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RunPreviewPort records a preview proxy registration for an app_preview node.
type RunPreviewPort struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	RunID       string `gorm:"index:idx_preview_run_node_item,unique" json:"runId"`
	NodeID      string `gorm:"index:idx_preview_run_node_item,unique" json:"nodeId"`
	ItemKey     string `gorm:"index:idx_preview_run_node_item,unique" json:"-"`
	Kind        string `gorm:"default:port" json:"kind"`
	Port        int    `json:"port"`
	ExternalURL string `json:"externalUrl,omitempty"`
	Label       string `json:"label,omitempty"`
	ProxyURL    string `json:"proxyUrl"`
	SandboxName string `json:"-"`
	// Host is the resolved upstream base the proxy dials, e.g.
	// "http://172.17.0.5:9090". Persisting it decouples the proxy read-path from
	// the co-located sandbox manager so the preview proxy can later be split into
	// a standalone service that only reads the DB (or a control-plane API).
	Host    string `json:"-"`
	Healthy bool   `json:"healthy"`
	// KeepalivePID is the setsid-detached listener pid recorded by KeepalivePort
	// so Cancel/Abort session cleanup can whitelist it (sandbox Destroy still
	// reclaims the whole container with the Run/gate lifecycle).
	KeepalivePID int       `json:"keepalivePid,omitempty"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// PreviewIssue is a problem a human reported against an app_preview node from
// the UI feedback chat. It is one-way feedback: the human submits it via REST
// while reviewing the preview, and the engine snapshots the issues into the
// preview_issues run variable at gate resume so a downstream node consumes them
// via {{vars.preview_issues}}. Selector/Port are optional context captured when
// the human picked a DOM element; Images are optional attached screenshots.
type PreviewIssue struct {
	ID           string        `gorm:"primaryKey" json:"id"`
	RunID        string        `gorm:"index" json:"runId"`
	NodeID       string        `json:"nodeId"`
	WorkflowID   string        `gorm:"index" json:"workflowId"`
	WorkflowName string        `json:"workflowName"`
	Body         string        `json:"body"`
	Selector     string        `json:"selector,omitempty"`
	Port         int           `json:"port,omitempty"`
	Images       []PromptImage `gorm:"serializer:json" json:"images,omitempty"`
	Status       string        `json:"status"` // open | resolved
	CreatedAt    time.Time     `json:"createdAt"`
}

// AllModels lists every table for AutoMigrate.
func AllModels() []any {
	return []any{
		&Project{},
		&WorkflowDef{}, &WorkflowVersion{}, &Run{}, &StateRun{},
		&RunVariable{}, &Artifact{}, &ArtifactVersion{}, &Gate{}, &ReactConversation{},
		&Sandbox{}, &SandboxLog{}, &Setting{}, &Session{}, &WorkflowAPIKey{},
		&RunPreviewPort{}, &PreviewIssue{}, &FeedbackEvent{},
		&ProjectMemoryItem{}, &ChatThread{}, &ChatMessage{}, &ChatTurnDraft{},
		&AgentCronJob{}, &AgentCronRun{}, &ChannelConfig{},
		&ProjectAuditEvent{},
		&NotifyDeliveryReceipt{},
		&GateShareLink{},
		&GateShareNonce{},
		&GateSharePreviewTicket{},
		&EmbedTicket{},
		&EmbedAnchor{},
		&EmbedSession{},
		&RequirementDraft{},
		&NotificationRead{},
		&NotificationBaseline{},
		&ProjectExternalMcpSettings{},
		&ProjectMcpApiKey{},
	}
}

// DefaultProjectID is the stable id used when AutoMigrate creates the initial
// 「默认项目」 for backfilling legacy workflows with no project ownership.
const DefaultProjectID = "proj-default"

// DefaultProjectName is the display name of the auto-created default project.
const DefaultProjectName = "默认项目"
