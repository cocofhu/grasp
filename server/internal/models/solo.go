package models

import (
	"encoding/json"
	"time"
)

// Project is one Git repository registered for Solo sessions, with the access
// credentials and environment it needs. Secret values never live here: Env only
// holds non-secret keys, and secrets are referenced through CredentialBindings.
type Project struct {
	ID          string `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:191;uniqueIndex" json:"name"`
	Description string `json:"description,omitempty"`
	RepoURL     string `json:"repoUrl"`
	// DefaultBranch is checked out when a session does not pick a branch.
	DefaultBranch string `json:"defaultBranch,omitempty"`
	// RepoDirName is the clone directory under the sandbox workspace.
	RepoDirName string `json:"repoDirName,omitempty"`
	// GitCredentialType is this repository's own access method:
	// github_https | gitlab_https | ssh. Empty means no repository credential
	// (public clone or none). It never inherits or replaces the Agent's Git.
	GitCredentialType    string             `json:"gitCredentialType,omitempty"`
	GitCredentialID      string             `json:"gitCredentialId,omitempty"`
	SshHostsCredentialID string             `json:"sshHostsCredentialId,omitempty"`
	Env                  map[string]string  `gorm:"serializer:json" json:"env,omitempty"`
	CredentialBindings   map[string]*string `gorm:"serializer:json" json:"credentialBindings,omitempty"`
	CreatedBy            string             `json:"createdBy,omitempty"`
	LastUsedAt           *time.Time         `json:"lastUsedAt,omitempty"`
	CreatedAt            time.Time          `json:"createdAt"`
	UpdatedAt            time.Time          `json:"updatedAt"`
}

// Solo session statuses.
const (
	SoloStatusStarting = "starting"
	SoloStatusRunning  = "running"
	SoloStatusClosed   = "closed"
	SoloStatusError    = "error"
)

// SoloSession is a direct CLI chat with one Agent in a resident sandbox. The
// sandbox has no idle TTL; it lives until the user closes the session.
type SoloSession struct {
	ID        string `gorm:"primaryKey" json:"id"`
	UserID    string `gorm:"index" json:"userId"`
	Title     string `json:"title"`
	AgentName string `gorm:"index" json:"agentName"`
	// ProjectID is empty when the session runs in an empty workspace.
	ProjectID string `gorm:"index" json:"projectId,omitempty"`
	// ProjectSnapshot freezes the project config at start so later edits or
	// deletion do not affect a live session.
	ProjectSnapshot *Project `gorm:"serializer:json" json:"project,omitempty"`
	Branch          string   `json:"branch,omitempty"`
	// InitialModelID is the model chosen for this session's first chat.
	// Empty follows the Agent configuration's primary model and does not change it.
	InitialModelID string `json:"initialModelId,omitempty"`
	SandboxID      uint   `gorm:"index" json:"sandboxId,omitempty"`
	Status         string `gorm:"index" json:"status"`
	Error          string `json:"error,omitempty"`
	// EnvSources maps each injected env key to where it came from
	// (platform | agent | project | credential | system). Values are never stored.
	EnvSources map[string]string `gorm:"serializer:json" json:"-"`
	// Restarts counts sandbox re-creations.
	Restarts int `json:"restarts"`
	// NeedsPreamble is set on restart: the CLI session was lost, so the next
	// prompt carries a summary of the persisted history.
	NeedsPreamble bool       `json:"-"`
	LastActiveAt  *time.Time `json:"lastActiveAt,omitempty"`
	ClosedAt      *time.Time `json:"closedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// SoloMessage is one persisted turn of a Solo session. Agent turns keep the raw
// ACP frames so the UI can replay thoughts and tool calls.
type SoloMessage struct {
	ID        string `gorm:"primaryKey" json:"id"`
	SessionID string `gorm:"index" json:"sessionId"`
	// Role: user | assistant | system (system marks restarts).
	Role    string            `json:"role"`
	Content string            `json:"content"`
	Images  []PromptImage     `gorm:"serializer:json" json:"images,omitempty"`
	Events  []json.RawMessage `gorm:"serializer:json" json:"events,omitempty"`
	// Status: ok | failed | stopped. Empty means ok.
	Status       string            `json:"status,omitempty"`
	Error        string            `json:"error,omitempty"`
	Usage        *TokenUsage       `gorm:"serializer:json" json:"usage,omitempty"`
	UsageByModel TokenUsageByModel `gorm:"serializer:json" json:"usageByModel,omitempty"`
	DurationMs   int64             `json:"durationMs,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}
