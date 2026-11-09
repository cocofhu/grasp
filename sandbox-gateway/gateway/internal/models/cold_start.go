package models

import "time"

// ColdStart is the once-only dispatch fence for a legacy environment's full
// authorization replacement. It survives gateway restarts and never stores
// credentials in a public receipt. A resource accepts one migration operation.
type ColdStart struct {
	Resource       string    `gorm:"primaryKey;size:128" json:"-"`
	RequestID      string    `gorm:"size:128" json:"-"`
	InputSHA256    string    `gorm:"size:64" json:"-"`
	PayloadSHA256  string    `gorm:"size:64" json:"-"`
	NativeIdentity string    `gorm:"size:256" json:"-"`
	State          string    `gorm:"size:32" json:"-"`
	CreatedAt      time.Time `json:"-"`
	UpdatedAt      time.Time `json:"-"`
}
