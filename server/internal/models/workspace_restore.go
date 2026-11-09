package models

import "time"

// WorkspaceRestore is an explicit operation receipt for a new destination.
// Completed control jobs alone never set this row to restored or change Solo's
// current environment. Actual extraction/integrity/binding are later phases.
type WorkspaceRestore struct {
	ID              string    `gorm:"primaryKey;size:64" json:"-"`
	ActorID         string    `gorm:"index;size:191" json:"-"`
	SoloID          string    `gorm:"index;size:191" json:"-"`
	SnapshotID      string    `gorm:"index;size:64" json:"-"`
	JobID           string    `gorm:"index;size:64" json:"-"`
	TargetSandboxID uint      `gorm:"index" json:"-"`
	ExpectedBinding string    `gorm:"size:128" json:"-"`
	Destination     string    `json:"-"`
	Fingerprint     string    `gorm:"size:64" json:"-"`
	ConfirmNonce    string    `gorm:"size:128" json:"-"`
	State           string    `gorm:"index;size:32" json:"-"`
	Error           string    `json:"-"`
	CreatedAt       time.Time `json:"-"`
	UpdatedAt       time.Time `json:"-"`
}
