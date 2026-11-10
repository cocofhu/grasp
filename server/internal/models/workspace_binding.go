package models

import "time"

// WorkspaceBindingHead is the durable admission barrier and canonical logical
// root for one physical environment. Paths and physical operation generations
// are not user-selected binding identity.
type WorkspaceBindingHead struct {
	SandboxID                uint       `gorm:"primaryKey" json:"-"`
	ScopeKind                string     `gorm:"size:32" json:"-"`
	ScopeID                  string     `gorm:"index;size:191" json:"-"`
	Resource                 string     `gorm:"size:128" json:"-"`
	Lineage                  string     `gorm:"index;size:128" json:"-"`
	BindingID                string     `gorm:"size:64" json:"-"`
	NativeID                 string     `gorm:"size:128" json:"-"`
	Revision                 uint64     `json:"-"`
	NativeFingerprint        string     `gorm:"size:64" json:"-"`
	Fingerprint              string     `gorm:"size:64" json:"-"`
	NativeHead               string     `json:"-"`
	AuthorizationFingerprint string     `gorm:"size:64" json:"-"`
	LifecycleEpoch           uint64     `json:"-"`
	LifecycleAttempt         string     `gorm:"size:64" json:"-"`
	PendingOperationID       string     `gorm:"index;size:64" json:"-"`
	LastOperationID          string     `gorm:"size:64" json:"-"`
	State                    string     `gorm:"index;size:32" json:"-"`
	ReleasedAt               *time.Time `json:"-"`
	CreatedAt                time.Time  `json:"-"`
	UpdatedAt                time.Time  `json:"-"`
}

// WorkspaceBinding retains roots that an explicit Return may select. Release
// marks their disk unavailable but preserves snapshot and historical receipts.
type WorkspaceBinding struct {
	ID                string     `gorm:"primaryKey;size:64" json:"-"`
	ActorID           string     `gorm:"index;size:191" json:"-"`
	SoloID            string     `gorm:"index;size:191" json:"-"`
	SandboxID         uint       `gorm:"index" json:"-"`
	Resource          string     `gorm:"size:128" json:"-"`
	Lineage           string     `gorm:"index;size:128" json:"-"`
	NativeID          string     `gorm:"size:128" json:"-"`
	Revision          uint64     `json:"-"`
	NativeFingerprint string     `gorm:"size:64" json:"-"`
	NativeHead        string     `json:"-"`
	SelectorKind      string     `gorm:"size:32" json:"-"`
	SelectorID        string     `gorm:"size:128" json:"-"`
	ParentBindingID   string     `gorm:"size:64" json:"-"`
	RestoreID         string     `gorm:"index;size:64" json:"-"`
	SnapshotID        string     `gorm:"index;size:64" json:"-"`
	State             string     `gorm:"index;size:32" json:"-"`
	ReleasedAt        *time.Time `json:"-"`
	CreatedAt         time.Time  `json:"-"`
	UpdatedAt         time.Time  `json:"-"`
}

type WorkspaceReturnPoint struct {
	ID                string    `gorm:"primaryKey;size:64" json:"-"`
	ActorID           string    `gorm:"index;size:191" json:"-"`
	SoloID            string    `gorm:"index;size:191" json:"-"`
	SandboxID         uint      `gorm:"index" json:"-"`
	OperationID       string    `gorm:"index;size:64" json:"-"`
	PreviousBindingID string    `gorm:"index;size:64" json:"-"`
	LeavingBindingID  string    `gorm:"size:64" json:"-"`
	SnapshotID        string    `gorm:"index;size:64" json:"-"`
	State             string    `gorm:"index;size:32" json:"-"`
	CreatedAt         time.Time `json:"-"`
	UpdatedAt         time.Time `json:"-"`
}

type WorkspaceBindingOperation struct {
	ID                       string    `gorm:"primaryKey;size:64" json:"-"`
	ActorID                  string    `gorm:"index;size:191" json:"-"`
	SoloID                   string    `gorm:"index;size:191" json:"-"`
	SandboxID                uint      `gorm:"index" json:"-"`
	ClientNonce              string    `gorm:"size:128" json:"-"`
	Fingerprint              string    `gorm:"size:64" json:"-"`
	Action                   string    `gorm:"size:32" json:"-"`
	ExpectedBinding          string    `gorm:"size:64" json:"-"`
	PreviousBindingID        string    `gorm:"size:64" json:"-"`
	TargetBindingID          string    `gorm:"size:64" json:"-"`
	BindingID                string    `gorm:"size:64" json:"-"`
	RestoreID                string    `gorm:"index;size:64" json:"-"`
	ReturnSnapshotID         string    `gorm:"index;size:64" json:"-"`
	SourceIdentity           string    `gorm:"size:64" json:"-"`
	NativeSpec               string    `json:"-"`
	Resource                 string    `gorm:"size:128" json:"-"`
	Lineage                  string    `gorm:"index;size:128" json:"-"`
	AuthorizationFingerprint string    `gorm:"size:64" json:"-"`
	LifecycleEpoch           uint64    `json:"-"`
	LifecycleAttempt         string    `gorm:"size:64" json:"-"`
	JobID                    string    `gorm:"index;size:64" json:"-"`
	State                    string    `gorm:"index;size:32" json:"-"`
	Result                   string    `json:"-"`
	Error                    string    `json:"-"`
	CreatedAt                time.Time `json:"-"`
	UpdatedAt                time.Time `json:"-"`
}

// WorkspaceBindingUse records long-lived IDE/terminal/preview access across
// server replicas. An unconfirmed disconnect remains a blocker until the
// original connection closes or a confirmed provider stop proves it ended.
type WorkspaceBindingUse struct {
	ID        string    `gorm:"primaryKey;size:64" json:"-"`
	SandboxID uint      `gorm:"index" json:"-"`
	BindingID string    `gorm:"size:64" json:"-"`
	Kind      string    `gorm:"size:32" json:"-"`
	Owner     string    `gorm:"size:64" json:"-"`
	State     string    `gorm:"index;size:32" json:"-"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}
