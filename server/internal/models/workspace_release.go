package models

import "time"

// WorkspaceRelease is an explicit, loss-aware whole-environment operation.
// Releasing compute is independent of deleting chat, snapshots or Return points.
// DispatchedAt forbids automatically repeating native deletion after a crash.
type WorkspaceRelease struct {
	ID                  string     `gorm:"primaryKey;size:64" json:"-"`
	ActorID             string     `gorm:"index;size:191" json:"-"`
	SoloID              string     `gorm:"index;size:191" json:"-"`
	SandboxID           uint       `gorm:"index" json:"-"`
	Resource            string     `gorm:"size:191" json:"-"`
	Lineage             string     `gorm:"index;size:128" json:"-"`
	PreviewNonce        string     `gorm:"size:128" json:"-"`
	Fingerprint         string     `gorm:"size:64" json:"-"`
	BindingsFingerprint string     `gorm:"size:64" json:"-"`
	ScopeJSON           string     `json:"-"`
	ProviderPlan        string     `json:"-"`
	ProviderPlanSHA256  string     `gorm:"size:64" json:"-"`
	LifecycleEpoch      uint64     `json:"-"`
	LifecycleAttempt    string     `gorm:"size:64" json:"-"`
	ConfirmNonce        string     `gorm:"size:128" json:"-"`
	State               string     `gorm:"index;size:32" json:"-"`
	Deadline            time.Time  `gorm:"index" json:"-"`
	DispatchedAt        *time.Time `json:"-"`
	ProviderReceipt     string     `json:"-"`
	Error               string     `gorm:"size:128" json:"-"`
	CreatedAt           time.Time  `json:"-"`
	UpdatedAt           time.Time  `json:"-"`
}
