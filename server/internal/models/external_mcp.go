package models

import "time"

// ExternalMcpSettings stores the workspace's external MCP access control as a
// singleton row keyed by WorkspaceID. Default enabled=false; packs are
// independent from Workspace.PmEnabledMcps.
type ExternalMcpSettings struct {
	ID           string    `gorm:"primaryKey" json:"-"`
	Enabled      bool      `json:"enabled"`
	EnabledPacks []string  `gorm:"serializer:json" json:"enabledPacks"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// WorkspaceMcpApiKey is an external MCP credential. Plaintext is
// shown only once at creation; KeyHash is bcrypt. Revoked keys fail immediately.
type WorkspaceMcpApiKey struct {
	ID        string     `gorm:"primaryKey" json:"id"`
	Name      string     `json:"name"`
	KeyPrefix string     `json:"keyPrefix"`
	KeyHash   string     `json:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}
