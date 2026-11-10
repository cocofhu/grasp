package models

import "time"

// Parent is the private originating chat. Reference is a separate authorized
// chat; it never grants access to the parent's messages.
type SoloWorkflowLink struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	SoloID    string    `gorm:"uniqueIndex:idx_solo_run;index" json:"soloId"`
	RunID     string    `gorm:"uniqueIndex:idx_solo_run;index" json:"runId"`
	UserID    string    `gorm:"index" json:"-"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
}

// WorkflowConversationEvent is the durable outbox. It is committed with the
// status transition and delivered idempotently to a persisted Solo message.
type WorkflowConversationEvent struct {
	ID      string `gorm:"primaryKey"`
	RunID   string `gorm:"uniqueIndex:idx_workflow_event;index"`
	Version uint64 `gorm:"uniqueIndex:idx_workflow_event"`
	SoloID  string `gorm:"index"`
	UserID  string
	Kind    string
	Payload string `gorm:"serializer:filetext"`
	// Version zero is an already-frozen legacy Payload (inline or fileref).
	// Version one has SQL-owned immutable source rows; workers materialize the
	// same Payload column outside the Run transition transaction.
	SourceVersion     int
	SourceCount       int
	SourceFingerprint string
	Disposition       string `gorm:"index;index:idx_workflow_event_due,priority:1"`
	// Retry metadata is independent of the immutable captured result. Failed
	// items stay pending and retryable without blocking another user's event.
	Attempts      int        `gorm:"not null;default:0"`
	NextAttemptAt *time.Time `gorm:"index:idx_workflow_event_due,priority:2"`
	LastError     string     `gorm:"size:191"`
	DeliveredAt   *time.Time
	CreatedAt     time.Time
}
