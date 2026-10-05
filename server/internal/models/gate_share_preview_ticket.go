package models

import "time"

// GateSharePreviewTicket is a short-lived credential for public preview
// VNC / API proxy. Plaintext share tokens are never stored; tickets are keyed
// by token hash and bind run/node/port for server-side Lookup.
type GateSharePreviewTicket struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	TokenHash string    `gorm:"index:idx_gspt_token_hash;size:64;not null" json:"-"`
	Ticket    string    `gorm:"uniqueIndex;size:64;not null" json:"-"`
	RunID     string    `gorm:"size:64;not null" json:"-"`
	NodeID    string    `gorm:"size:128;not null" json:"-"`
	Port      int       `gorm:"not null" json:"-"`
	Purpose   string    `gorm:"size:16;not null" json:"-"` // vnc | api
	ExpiresAt time.Time `gorm:"index" json:"-"`
	CreatedAt time.Time `json:"-"`
}
