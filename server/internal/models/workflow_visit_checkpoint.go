package models

import "time"

// WorkflowVisitCheckpoint seals one judged visit before its physical workspace
// can be rebound. An outcome is logical immutable JSON, not a serializer hook;
// ready file evidence and application of the route are separate state machines.
// A restored/incomplete checkpoint never authorizes replay of its Agent.
type WorkflowVisitCheckpoint struct {
	ID                 string     `gorm:"primaryKey;size:64" json:"id"`
	RunID              string     `gorm:"index;size:191" json:"runId"`
	StateRunID         uint       `gorm:"index" json:"stateRunId"`
	NodeID             string     `gorm:"size:191" json:"nodeId"`
	Iteration          int        `json:"iteration"`
	RunAttempt         int        `json:"runAttempt"`
	ProviderAttempt    int        `json:"providerAttempt"`
	SandboxID          uint       `gorm:"index" json:"-"`
	Resource           string     `gorm:"size:191" json:"-"`
	Lineage            string     `gorm:"index;size:128" json:"-"`
	ActorID            string     `gorm:"size:191" json:"-"`
	ControlVersion     uint64     `json:"-"`
	OriginalStatus     string     `gorm:"size:32" json:"-"`
	GraphSHA256        string     `gorm:"size:64" json:"-"`
	OutcomeSHA256      string     `gorm:"size:64" json:"-"`
	OutcomeRef         string     `gorm:"payload:json" json:"-"`
	BindingFingerprint string     `gorm:"size:64" json:"-"`
	SnapshotID         string     `gorm:"size:64" json:"snapshotId,omitempty"`
	CaptureNonce       string     `gorm:"size:128" json:"-"`
	CaptureDurationMS  int64      `json:"-"`
	CaptureStartedAt   *time.Time `json:"-"`
	NextProbeAt        *time.Time `gorm:"index" json:"-"`
	// FileState: pending/ready/blocked/unknown/skipped/not_created/unavailable.
	FileState string `gorm:"index;size:32" json:"fileState"`
	// RouteState: sealing/sealed/applied/blocked/unknown/cancelled.
	RouteState string    `gorm:"index;size:32" json:"routeState"`
	Reason     string    `gorm:"size:191" json:"reason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
