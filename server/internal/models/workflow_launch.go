package models

import "time"

// WorkflowLaunch is committed in the same transaction as the new Run. A
// repeated request can only return that Run; reusing a key with different
// inputs is rejected. The scope is authenticated source + actor + origin Solo.
type WorkflowLaunch struct {
	Key         string `gorm:"primaryKey;size:64"`
	Fingerprint string
	RunID       string `gorm:"size:191;uniqueIndex"`
	Source      string
	ActorID     string
	SoloID      string `gorm:"index"`
	CreatedAt   time.Time
}
