package models

import "time"

// WorkspaceRestore is an explicit operation receipt for a new destination.
// Only a validated native materialization receipt can mark this operation
// restored. A restored copy never changes Solo's active environment.
type WorkspaceRestore struct {
	ID               string `gorm:"primaryKey;size:64" json:"-"`
	ActorID          string `gorm:"index;size:191" json:"-"`
	SoloID           string `gorm:"index;size:191" json:"-"`
	SnapshotID       string `gorm:"index;size:64" json:"-"`
	JobID            string `gorm:"index;size:64" json:"-"`
	TargetSandboxID  uint   `gorm:"index" json:"-"`
	ExpectedBinding  string `gorm:"size:128" json:"-"`
	LifecycleEpoch   uint64 `json:"-"`
	LifecycleAttempt string `gorm:"size:64" json:"-"`
	Lineage          string `gorm:"index;size:128" json:"-"`
	// These bounded metadata strings freeze the source identity and native wire
	// receipt. Object references remain owned by the immutable ready snapshot.
	SourceIdentity string    `gorm:"size:64" json:"-"`
	SourceSpec     string    `json:"-"`
	SourceReceipt  string    `json:"-"`
	Destination    string    `json:"-"`
	Fingerprint    string    `gorm:"size:64" json:"-"`
	ConfirmNonce   string    `gorm:"size:128" json:"-"`
	State          string    `gorm:"index;size:32" json:"-"`
	Error          string    `json:"-"`
	CreatedAt      time.Time `json:"-"`
	UpdatedAt      time.Time `json:"-"`
}
