package models

import "time"

// Token ledger sources.
const (
	TokenLedgerSourceWorkflow = "workflow"
	TokenLedgerSourcePM       = "pm"
	TokenLedgerSourceStudio   = "studio"
)

// Token ledger statuses.
const (
	TokenLedgerStatusOK        = "ok"
	TokenLedgerStatusFailed    = "failed"
	TokenLedgerStatusCancelled = "cancelled"
)

// Token ledger phases (what the LLM call was doing).
const (
	TokenLedgerPhaseProduction  = "production"
	TokenLedgerPhaseInteractive = "interactive"
	TokenLedgerPhaseChat        = "chat"
)

// TokenUsageEvent is one append-only token-consumption ledger row. Every LLM
// usage report (workflow node save/flush, PM turn, Studio chat) writes one row
// per model bucket, so analytics can GROUP BY any dimension without joining
// through mutable workflow/project rows. Name fields are snapshots taken at
// write time and survive workflow/project deletion.
type TokenUsageEvent struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time `gorm:"index" json:"createdAt"`
	Source       string    `gorm:"index;size:16" json:"source"`
	Phase        string    `gorm:"size:32" json:"phase,omitempty"`
	Status       string    `gorm:"index;size:16" json:"status"`
	ProjectID    string    `gorm:"index" json:"projectId,omitempty"`
	ProjectName  string    `json:"projectName,omitempty"`
	WorkflowID   string    `gorm:"index" json:"workflowId,omitempty"`
	WorkflowName string    `json:"workflowName,omitempty"`
	RunID        string    `gorm:"index" json:"runId,omitempty"`
	RunTitle     string    `json:"runTitle,omitempty"`
	NodeID       string    `json:"nodeId,omitempty"`
	NodeType     string    `gorm:"size:64" json:"nodeType,omitempty"`
	ThreadID     string    `gorm:"index" json:"threadId,omitempty"`
	SandboxID    uint      `json:"sandboxId,omitempty"`
	ModelKey     string    `gorm:"index" json:"modelKey"`
	ModelSource  string    `gorm:"size:32" json:"modelSource,omitempty"`
	ModelFilled  bool      `json:"modelFilled,omitempty"`

	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

// Total returns the four-component sum.
func (e TokenUsageEvent) Total() int64 {
	return e.InputTokens + e.OutputTokens + e.CacheReadTokens + e.CacheWriteTokens
}
