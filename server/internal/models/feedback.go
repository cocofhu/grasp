package models

import (
	"strings"
	"time"
)

// Feedback event kinds. Only human-originated events are recorded: rollback /
// auto-retry / resume-from are machine routing decisions and stay out of this
// table entirely (the run trace already carries them).
const (
	FeedbackKindClarify = "clarify"
	FeedbackKindReview  = "review"
	FeedbackKindGate    = "gate"
	FeedbackKindPreview = "preview"
)

// FeedbackEvent is one round of human feedback on a run, persisted append-only.
//
// It is the source of truth from which feedback artifacts and
// feedback_index.json are rendered, mirroring how run_error.json is derived
// from StateRun. Rows are only ever INSERTed: a node visited N times with M
// revise turns each keeps every one of them, because losing a round loses the
// reviewer's reasoning for that round.
//
// Two ordinals are carried on purpose. Seq is the run-global order. Round is
// the ordinal within (NodeID, Iteration) — one execution can hold many revise
// turns, so Iteration alone cannot address "the 3rd push-back of the 2nd run".
// Round remains the audit ordinal. ReAct artifacts intentionally do not include
// it: they are upserted cumulative conclusions keyed by node and iteration.
type FeedbackEvent struct {
	ID    uint   `gorm:"primaryKey" json:"-"`
	RunID string `gorm:"index" json:"runId"`
	Seq   int    `json:"seq"`
	Round int    `json:"round"`
	Kind  string `json:"kind"`

	NodeID    string `json:"nodeId"`
	Iteration int    `json:"iteration"`

	Actor          string `json:"actor,omitempty"`
	Unattributable bool   `json:"unattributable,omitempty"`
	CallerKind     string `json:"callerKind,omitempty"` // pm | apikey | system | external

	// Action is the kind-specific verb: revise | answer | auto_answer |
	// snapshot, or the gate action id for a gate decision.
	Action string `json:"action,omitempty"`
	// Text is the human's verbatim opinion.
	Text string `json:"text,omitempty"`
	// AgentSummary is an optional Agent-authored induction of this round's
	// feedback, distinct from FeedbackSummary (index gist) and from any
	// transcript bubble. Empty means the round has no card-level summary.
	AgentSummary string `json:"agentSummary,omitempty"`
	// Interrupted marks a revise turn whose agent reply was cancelled or
	// failed. The opinion was still given, so the round is kept — only its
	// Targets are absent because nothing landed.
	Interrupted bool `json:"interrupted,omitempty"`

	// ArtifactName is the product this event rendered to. ReAct rounds in one
	// execution share it; gate/preview rounds retain distinct names. Empty when
	// IndexOnly is set.
	ArtifactName string `json:"artifactName,omitempty"`
	// IndexOnly marks a round that is listed in the index but gets no
	// standalone product. Used for platform auto-answered clarify rounds: they
	// shape the requirement and later rounds need that context, but there is no
	// human prose worth a file of its own.
	IndexOnly bool `json:"indexOnly,omitempty"`

	Annotations []ReactAnnotation `gorm:"serializer:json" json:"annotations,omitempty"`
	// Attachments must only carry blob refs; PromptImage.Data is cleared before
	// persistence so base64 never reaches the DB or the rendered artifact.
	Attachments []PromptImage `gorm:"serializer:json" json:"attachments,omitempty"`
	// Turns is this round's incremental dialogue, not the whole conversation.
	Turns   []ReactMessage   `gorm:"serializer:json" json:"turns,omitempty"`
	Targets []FeedbackTarget `gorm:"serializer:json" json:"targets,omitempty"`
	// Detail carries kind-specific fields: gate stores form / goto / external;
	// preview stores each issue's selector / port.
	Detail map[string]any `gorm:"serializer:json" json:"detail,omitempty"`

	OccurredAt time.Time `json:"at"`
}

// FeedbackTarget records the before/after digest of a product a round of
// feedback changed, so "what did this push-back actually alter" is answerable
// without diffing the artifact store by hand.
type FeedbackTarget struct {
	Name    string `json:"name"`
	Before  string `json:"before,omitempty"` // sha256[:16]
	After   string `json:"after,omitempty"`
	Changed bool   `json:"changed"`
}

// WantsArtifact reports whether this event participates in a product file.
func (e FeedbackEvent) WantsArtifact() bool { return !e.IndexOnly }

// HasSubstance reports whether the event carries content worth persisting: an
// opinion, an annotation, an attachment, a ReAct turn, or an AgentSummary.
//
// AgentSummary alone qualifies because「确认并流转」may produce an induction of
// the dialogue with no new human prose and an empty reconcile narration — that
// summary still has nowhere else to live in the ledger.
//
// This is the single gate on the whole feature. A gate approval clicked through
// with an empty form has none of the above and must not produce a row or a
// product — otherwise bulk sign-off floods the run with empty shells.
func (e FeedbackEvent) HasSubstance() bool {
	if strings.TrimSpace(e.Text) != "" {
		return true
	}
	if strings.TrimSpace(e.AgentSummary) != "" {
		return true
	}
	if len(e.Annotations) > 0 || len(e.Attachments) > 0 {
		return true
	}
	for _, t := range e.Turns {
		if strings.TrimSpace(t.Text) != "" || len(t.Questions) > 0 || len(t.Forms) > 0 ||
			len(t.Annotations) > 0 || len(t.Images) > 0 {
			return true
		}
	}
	return false
}
