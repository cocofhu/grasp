package models

import "time"

// Embed credential kinds. A session embed acts as the logged-in user who
// issued the ticket; a share embed acts within one gate/review share link.
const (
	EmbedKindSession = "session"
	EmbedKindShare   = "share"
)

// EmbedTicket is a one-shot credential placed in the preview page URL
// fragment. Only its hash is stored; redemption sets ConsumedAt.
type EmbedTicket struct {
	ID             uint       `gorm:"primaryKey" json:"-"`
	TicketHash     string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	Kind           string     `gorm:"size:16;not null" json:"-"`
	RunID          string     `gorm:"index;size:64;not null" json:"-"`
	NodeID         string     `gorm:"size:128;not null" json:"-"`
	Username       string     `gorm:"size:128" json:"-"`
	ShareTokenHash string     `gorm:"size:64" json:"-"`
	GraspOrigin    string     `gorm:"size:512;not null" json:"-"`
	ExpiresAt      time.Time  `gorm:"index" json:"-"`
	ConsumedAt     *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"-"`
}

// EmbedSession is the bearer credential the chat drawer holds after redeeming
// an EmbedTicket. Only its hash is stored.
type EmbedSession struct {
	ID             uint      `gorm:"primaryKey" json:"-"`
	TokenHash      string    `gorm:"uniqueIndex;size:64;not null" json:"-"`
	Kind           string    `gorm:"size:16;not null" json:"-"`
	RunID          string    `gorm:"index;size:64;not null" json:"-"`
	NodeID         string    `gorm:"size:128;not null" json:"-"`
	Username       string    `gorm:"size:128" json:"-"`
	ShareTokenHash string    `gorm:"index;size:64" json:"-"`
	ExpiresAt      time.Time `gorm:"index" json:"-"`
	CreatedAt      time.Time `json:"-"`
}
