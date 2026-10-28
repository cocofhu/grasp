package models

import (
	"encoding/json"
	"time"
)

// Project is one Git repository registered for Solo sessions. It stores only
// identity, display text and the clone location. Access and environment come
// from the Agent that starts the session, never from the repository row.
type Project struct {
	ID          string `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:191;uniqueIndex" json:"name"`
	Description string `json:"description,omitempty"`
	RepoURL     string `json:"repoUrl"`
	// DefaultBranch is checked out when a session does not pick a branch.
	DefaultBranch string `json:"defaultBranch,omitempty"`
	// RepoDirName is the clone directory under the sandbox workspace.
	RepoDirName string     `json:"repoDirName,omitempty"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Solo session statuses.
const (
	SoloStatusStarting = "starting"
	SoloStatusRunning  = "running"
	SoloStatusClosed   = "closed"
	SoloStatusError    = "error"
)

// SessionRepo is one repository frozen into a Solo session at start. It is
// stored inside the session, not as its own table, and only records what to
// clone. Agent permissions and environment are resolved again on each start.
type SessionRepo struct {
	Position    int    `json:"position"`
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	RepoURL     string `json:"repoUrl"`
	RepoDirName string `json:"repoDirName"`
	Branch      string `json:"branch,omitempty"`
}

// SoloSession is a direct CLI chat with one Agent in a resident sandbox. The
// sandbox has no idle TTL; it lives until the user closes the session.
type SoloSession struct {
	ID        string `gorm:"primaryKey" json:"id"`
	UserID    string `gorm:"index" json:"userId"`
	Title     string `json:"title"`
	AgentName string `gorm:"index" json:"agentName"`
	// ProjectID is the single repository of a legacy session. New multi-repo
	// sessions leave it empty; usage is the Repos list. A new single-repo
	// session still fills it so older readers keep working.
	ProjectID string `gorm:"index" json:"projectId,omitempty"`
	// ProjectSnapshot freezes the single project of a legacy session.
	ProjectSnapshot *Project `gorm:"serializer:json" json:"project,omitempty"`
	Branch          string   `json:"branch,omitempty"`
	// InitialModelID is the model chosen for this session's first chat.
	// Empty follows the Agent configuration's primary model and does not change it.
	// Switching models later does not rewrite this value.
	InitialModelID string `json:"initialModelId,omitempty"`
	// CurrentModelID is the model later turns use. Empty means the primary model.
	// It is independent of InitialModelID. CurrentModelSet distinguishes that
	// explicit primary from an old row that never stored a current model.
	CurrentModelID  string `json:"currentModelId,omitempty"`
	CurrentModelSet bool   `json:"currentModelSet,omitempty"`
	// CurrentReasoningEffort is a session choice, independent of the Agent's
	// saved configuration. Empty selects the tool's default reasoning behavior.
	CurrentReasoningEffort string `json:"reasoningEffort"`
	// AppliedModelID is the model last given to the sandbox chat. It is not
	// part of the API. AppliedModelSet false means the sandbox still has the
	// opening model (or has not been aligned yet).
	AppliedModelID         string `json:"-"`
	AppliedModelSet        bool   `json:"-"`
	AppliedReasoningEffort string `json:"-"`
	// ModelNotice is a stable error code shown after a switch is rolled back.
	ModelNotice string `json:"modelNotice,omitempty"`
	// Repos is the ordered repository snapshot. Empty means an empty workspace
	// when ProjectSnapshot is also empty. Legacy rows leave this empty and are
	// read as a one-element list built from ProjectSnapshot.
	Repos     []SessionRepo `gorm:"serializer:json" json:"repos,omitempty"`
	SandboxID uint          `gorm:"index" json:"sandboxId,omitempty"`
	Status    string        `gorm:"index" json:"status"`
	Error     string        `json:"error,omitempty"`
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
