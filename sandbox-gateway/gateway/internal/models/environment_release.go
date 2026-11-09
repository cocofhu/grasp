package models

import "time"

// EnvironmentRelease is a durable, explicitly authorized physical loss receipt.
// No native mutation may be replayed after DispatchedAt was committed.
type EnvironmentRelease struct {
	ID           string     `gorm:"primaryKey;size:128" json:"-"`
	Resource     string     `gorm:"index;size:128" json:"-"`
	Lineage      string     `gorm:"size:128" json:"-"`
	Plan         string     `json:"-"`
	PlanSHA256   string     `gorm:"size:64" json:"-"`
	State        string     `gorm:"index;size:32" json:"-"`
	ConfirmNonce string     `gorm:"size:128" json:"-"`
	Owner        string     `gorm:"size:128" json:"-"`
	Generation   uint64     `json:"-"`
	ExpiresAt    *time.Time `gorm:"index" json:"-"`
	Deadline     time.Time  `gorm:"index" json:"-"`
	DispatchedAt *time.Time `json:"-"`
	Result       string     `json:"-"`
	Error        string     `gorm:"size:128" json:"-"`
	CreatedAt    time.Time  `json:"-"`
	UpdatedAt    time.Time  `json:"-"`
}

// EnvironmentReleaseFence outlives both provider workloads and released rows.
// Old SQL generations or new lifecycle calls cannot resurrect a released ID.
type EnvironmentReleaseFence struct {
	Resource        string    `gorm:"primaryKey;size:128" json:"-"`
	Lineage         string    `gorm:"size:128" json:"-"`
	HighWater       uint64    `json:"-"`
	ActiveReleaseID string    `gorm:"size:128;index" json:"-"`
	CreatedAt       time.Time `json:"-"`
	UpdatedAt       time.Time `json:"-"`
}
