package models

import "time"

// SoloCreation retains the authenticated request identity after acknowledgement
// loss and even after explicit chat deletion. It never starts another session
// when the original one has been closed or deleted.
type SoloCreation struct {
	ID            string `gorm:"primaryKey;size:64"`
	UserID        string `gorm:"size:191;index"`
	Fingerprint   string `gorm:"size:64;not null"`
	SessionID     string `gorm:"size:191;index"`
	InitialTurnID string `gorm:"size:191"`
	CreatedAt     time.Time
}
