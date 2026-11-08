package models

import "time"

// WorkflowInstructionMailbox belongs to one concrete StateRun. Generation is
// renewed when an executor reconnects/restarts; stale instructions never migrate
// to another node visit. It has its own row so StateRun snapshots cannot revert
// mailbox counters or the final admission seal.
type WorkflowInstructionMailbox struct {
	ID           uint      `gorm:"primaryKey;autoIncrement:false" json:"-"`
	RunID        string    `gorm:"index" json:"runId"`
	NodeID       string    `json:"nodeId"`
	Iteration    int       `json:"iteration"`
	Attempt      int       `json:"attempt"`
	Generation   string    `gorm:"size:36" json:"generation"`
	State        string    `json:"state"`
	NextSequence uint64    `json:"-"`
	Revision     uint64    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// WorkflowInstruction is immutable user intent plus its explicit delivery
// state. A dispatch whose acknowledgement is lost becomes unknown, never
// silently replayed against a later visit or an external side effect.
type WorkflowInstruction struct {
	ID              string    `gorm:"primaryKey;size:36" json:"id"`
	MailboxID       uint      `gorm:"uniqueIndex:idx_workflow_instruction_client,priority:1;index" json:"-"`
	Generation      string    `gorm:"size:36;uniqueIndex:idx_workflow_instruction_client,priority:2" json:"generation"`
	ClientMessageID string    `gorm:"size:128;uniqueIndex:idx_workflow_instruction_client,priority:3" json:"clientMessageId"`
	RunID           string    `gorm:"index" json:"runId"`
	NodeID          string    `json:"nodeId"`
	Iteration       int       `json:"iteration"`
	Attempt         int       `json:"attempt"`
	Sequence        uint64    `json:"sequence"`
	ActorID         string    `json:"actorId"`
	Text            string    `gorm:"serializer:filetext" json:"text"`
	State           string    `json:"state"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
