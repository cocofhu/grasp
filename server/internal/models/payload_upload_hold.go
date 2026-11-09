package models

import "time"

// PayloadUploadHold reserves immutable snapshot objects before the first PUT.
// Planned hashes can name not-yet-existing objects and are not payload roots.
// The typed refs become roots only after full read-back verification. Unknown,
// expired and partially verified holds retain their reservation and roots;
// nothing here authorizes deletion or a native sandbox job replay.
type PayloadUploadHold struct {
	ID                 string     `gorm:"primaryKey;size:64" json:"-"`
	ScopeKind          string     `gorm:"index:idx_upload_scope;size:32" json:"-"`
	ScopeID            string     `gorm:"index:idx_upload_scope;size:191" json:"-"`
	OperationID        string     `gorm:"index;size:128" json:"-"`
	SnapshotID         string     `gorm:"index;size:64" json:"-"`
	ActorID            string     `gorm:"size:191" json:"-"`
	Lineage            string     `gorm:"index;size:128" json:"-"`
	StorageFingerprint string     `gorm:"index;size:64" json:"-"`
	PlanSHA256         string     `gorm:"size:64" json:"-"`
	Limits             string     `gorm:"type:text" json:"-"`
	ArchiveSHA256      string     `gorm:"size:64" json:"-"`
	ArchiveBytes       int64      `json:"-"`
	ManifestSHA256     string     `gorm:"size:64" json:"-"`
	ManifestBytes      int64      `json:"-"`
	ReservedBytes      int64      `json:"-"`
	IntentDeadline     time.Time  `json:"-"`
	Deadline           time.Time  `json:"-"`
	State              string     `gorm:"index;size:32" json:"-"`
	Owner              string     `gorm:"size:128" json:"-"`
	Generation         uint64     `json:"-"`
	ExpiresAt          time.Time  `gorm:"index" json:"-"`
	ArchiveRef         string     `gorm:"payload:binary" json:"-"`
	ManifestRef        string     `gorm:"payload:json" json:"-"`
	ArchiveVerifiedAt  *time.Time `json:"-"`
	ManifestVerifiedAt *time.Time `json:"-"`
	PinnedAt           *time.Time `json:"-"`
	Error              string     `gorm:"size:191" json:"-"`
	CreatedAt          time.Time  `json:"-"`
	UpdatedAt          time.Time  `json:"-"`
}

// PayloadUploadBudget serializes short admission/reservation updates. Bytes and
// HoldCount are monotonic while destructive GC is disabled. HoldCount also
// prevents an immutable backup silently accepting loss of the hold-root table.
type PayloadUploadBudget struct {
	ID            string    `gorm:"primaryKey;size:64" json:"-"`
	ReservedBytes int64     `json:"-"`
	HoldCount     int64     `json:"-"`
	CreatedAt     time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}
