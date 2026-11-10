package models

import "time"

// WorkspaceImport is a frozen, explicitly confirmed Git transfer. The archive
// is uploaded and verified before this metadata row is committed. Sandbox and
// object-store coordinates remain private; clients confirm only its opaque ID.
type WorkspaceImport struct {
	ID               string    `gorm:"primaryKey;size:64" json:"-"`
	ActorID          string    `gorm:"index;size:191" json:"-"`
	SoloID           string    `gorm:"index;size:191" json:"-"`
	RunID            string    `gorm:"index;size:191" json:"-"`
	NodeID           string    `gorm:"size:191" json:"-"`
	Iteration        int       `json:"-"`
	Attempt          int       `json:"-"`
	SourceSandboxID  uint      `json:"-"`
	TargetSandboxID  uint      `json:"-"`
	SourceGeneration uint64    `json:"-"`
	Fingerprint      string    `gorm:"size:64" json:"-"`
	ArchiveRef       string    `gorm:"payload:binary" json:"-"`
	ArchiveSHA256    string    `gorm:"size:64" json:"-"`
	Repositories     string    `json:"-"`
	State            string    `gorm:"index;size:32" json:"-"`
	ConfirmNonce     string    `gorm:"size:128" json:"-"`
	Directory        string    `json:"-"`
	Error            string    `json:"-"`
	CreatedAt        time.Time `json:"-"`
	UpdatedAt        time.Time `json:"-"`

	// Legacy previews have no expiry; their age is not inferred retroactively.
	ExpiresAt *time.Time `gorm:"index" json:"-"`
}
