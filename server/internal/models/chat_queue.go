package models

import "time"

// ChatQueue is the durable authority for one conversation's ordered commands.
// A renewable owner may execute a single turn; viewers do not own execution.
type ChatQueue struct {
	ID              string `gorm:"primaryKey;size:64"`
	Surface         string `gorm:"size:64;uniqueIndex:idx_chat_queue_scope,priority:1"`
	SessionKey      string `gorm:"size:191;uniqueIndex:idx_chat_queue_scope,priority:2"`
	Revision        uint64
	Generation      uint64
	NextSequence    uint64
	Owner           string `gorm:"size:36"`
	OwnerExpiresAt  *time.Time
	ActiveTurnID    string `gorm:"size:191"`
	CancelRequested bool
	Retired         bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ChatQueueEpoch allocates monotonic, globally comparable generations. It has
// one row and never retains per-message allocation history.
type ChatQueueEpoch struct {
	ID         string `gorm:"primaryKey;size:32"`
	Generation uint64
}

// ChatQueueTurn is one immutable accepted intent plus its explicit receipt.
// Terminal rows preserve client-id deduplication after an HTTP acknowledgement
// is lost. Bodies use the common payload backend; no queue mutation rewrites
// another accepted request's bytes.
type ChatQueueTurn struct {
	ID              string `gorm:"primaryKey;size:191"`
	QueueID         string `gorm:"size:64;uniqueIndex:idx_chat_turn_client,priority:1;index:idx_chat_turn_order,priority:1"`
	ActorID         string `gorm:"size:191"`
	ClientMessageID string `gorm:"size:128"`
	ClientKey       string `gorm:"size:64;uniqueIndex:idx_chat_turn_client,priority:2"`
	Fingerprint     string `gorm:"size:64"`
	Sequence        uint64
	Position        uint64
	State           string `gorm:"size:32;index:idx_chat_turn_order,priority:2"`
	Payload         []byte `gorm:"serializer:filebytes;type:text"`
	ExecutionEpoch  uint64
	DispatchedAt    *time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	Reason          string `gorm:"size:64"`
	ResultID        string `gorm:"size:191"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
