package models

import "time"

// WorkspaceSnapshot holds file-capture metadata. Ready requires semantic archive
// and manifest validation, full object read-back and a generation-fenced atomic
// storage pin, snapshot publication and job-final transaction.
type WorkspaceSnapshot struct {
	ID               string     `gorm:"primaryKey;size:64" json:"-"`
	ScopeKind        string     `gorm:"index;size:32" json:"-"`
	ScopeID          string     `gorm:"index;size:191" json:"-"`
	ActorID          string     `gorm:"index;size:191" json:"-"`
	RunID            string     `gorm:"index;size:191" json:"-"`
	StateRunID       uint       `gorm:"index" json:"-"`
	NodeID           string     `gorm:"size:191" json:"-"`
	Iteration        int        `json:"-"`
	RunAttempt       int        `json:"-"`
	ProviderAttempt  int        `json:"-"`
	SourceSandboxID  uint       `gorm:"index" json:"-"`
	SourceGeneration uint64     `json:"-"`
	JobID            string     `gorm:"index;size:64" json:"-"`
	PublicationJobID string     `gorm:"index;size:64" json:"-"`
	Lineage          string     `gorm:"index;size:128" json:"-"`
	State            string     `gorm:"index;size:32" json:"-"`
	PolicyVersion    string     `gorm:"size:64" json:"-"`
	ArchiveRef       string     `gorm:"payload:binary" json:"-"`
	ManifestRef      string     `gorm:"payload:json" json:"-"`
	ArchiveSHA256    string     `gorm:"size:64" json:"-"`
	ManifestSHA256   string     `gorm:"size:64" json:"-"`
	Bytes            int64      `json:"-"`
	Entries          int64      `json:"-"`
	VerifiedAt       *time.Time `json:"-"`
	RetainUntil      *time.Time `gorm:"index" json:"-"`
	CreatedAt        time.Time  `json:"-"`
	UpdatedAt        time.Time  `json:"-"`
}

// WorkspaceSnapshotJob contains a small immutable spec and durable dispatch
// marker. Bodies, native process state and authorization secrets do not enter
// the spec. RuntimeOwner is a private operation capability, never an API field.
type WorkspaceSnapshotJob struct {
	ID                string     `gorm:"primaryKey;size:64" json:"-"`
	ActorID           string     `gorm:"index;size:191" json:"-"`
	ScopeKind         string     `gorm:"index;size:32" json:"-"`
	ScopeID           string     `gorm:"index;size:191" json:"-"`
	SandboxID         uint       `gorm:"index" json:"-"`
	Kind              string     `gorm:"size:32" json:"-"`
	Fingerprint       string     `gorm:"size:64" json:"-"`
	Body              string     `json:"-"`
	Nonce             string     `gorm:"size:128" json:"-"`
	Lineage           string     `gorm:"index;size:128" json:"-"`
	State             string     `gorm:"index;size:32" json:"-"`
	Deadline          time.Time  `gorm:"index" json:"-"`
	Owner             string     `gorm:"size:128" json:"-"`
	Generation        uint64     `json:"-"`
	ExpiresAt         *time.Time `gorm:"index" json:"-"`
	DispatchedAt      *time.Time `json:"-"`
	RuntimeOwner      string     `gorm:"size:128" json:"-"`
	RuntimeResource   string     `gorm:"size:128" json:"-"`
	RuntimeGeneration uint64     `json:"-"`
	RuntimeSHA256     string     `gorm:"size:64" json:"-"`
	RemoteExpiresAt   *time.Time `json:"-"`
	Result            string     `json:"-"`
	Error             string     `json:"-"`
	CreatedAt         time.Time  `json:"-"`
	UpdatedAt         time.Time  `json:"-"`
}
