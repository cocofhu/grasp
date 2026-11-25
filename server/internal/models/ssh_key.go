package models

import "time"

// UserSSHKey is a public key a user registered for the SSH gateway. The key
// never reaches a sandbox; the gateway authenticates it and logs into the
// sandbox with platform credentials. Deleting the row revokes access at once.
type UserSSHKey struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Username string `gorm:"size:191;not null;index" json:"-"`
	Name     string `gorm:"size:191;not null" json:"name"`
	// PublicKey is the normalized authorized_keys line without a comment.
	PublicKey string `gorm:"type:text;not null" json:"publicKey"`
	// Fingerprint is the SHA256 fingerprint ("SHA256:...") and is unique across users.
	Fingerprint string     `gorm:"size:191;not null;uniqueIndex" json:"fingerprint"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

func (UserSSHKey) TableName() string {
	return "user_ssh_keys"
}
