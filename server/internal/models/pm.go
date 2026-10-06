package models

import (
	"strings"
	"time"
)

// ProjectMemoryItem is one long-term memory entry scoped to a project + agent.
// Human edits/clears go through Agent Studio (any authenticated user);
// agents write via memory-store MCP (source=agent).
type ProjectMemoryItem struct {
	ID        string `gorm:"primaryKey" json:"id"`
	ProjectID string `gorm:"index;uniqueIndex:idx_pm_mem_proj_agent_title" json:"projectId"`
	// AgentName isolates memories per Agent within a project.
	AgentName string    `gorm:"index;uniqueIndex:idx_pm_mem_proj_agent_title" json:"agentName"`
	Title     string    `gorm:"uniqueIndex:idx_pm_mem_proj_agent_title" json:"title"`
	Content   string    `json:"content"`
	Source    string    `json:"source"` // agent | user | admin | system
	UpdatedBy string    `json:"updatedBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ChatThread is a consult thread under a project.
// Human UI lists by UserID. MCP context-store visibility is the caller's
// user threads plus the agent's cron threads (not other users' consults).
type ChatThread struct {
	ID        string `gorm:"primaryKey" json:"id"`
	ProjectID string `gorm:"index" json:"projectId"`
	UserID    string `gorm:"index" json:"userId"`
	// AgentName is the PM Agent that owns this conversation for MCP isolation.
	// Preserved when the project rebinds to another Agent.
	AgentName string `gorm:"index" json:"agentName,omitempty"`
	// Kind: user (interactive) | cron (scheduler-owned exclusive thread).
	Kind  string `gorm:"index" json:"kind,omitempty"`
	Title string `json:"title"`
	// Unspoken is a computed flag: channel thread with no source=channel inbound.
	Unspoken bool `json:"unspoken,omitempty" gorm:"-"`
	// SandboxRef stores the bound models.Sandbox.ID as a decimal string when a
	// PM consult sandbox is live for this thread. Empty means unbound.
	SandboxRef string    `json:"sandboxRef,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Chat thread kinds.
const (
	ChatThreadKindUser = "user"
	ChatThreadKindCron = "cron"
)

// ChatMessage is one persisted turn in a PM Leader thread (context-store SoT).
type ChatMessage struct {
	ID       string `gorm:"primaryKey" json:"id"`
	ThreadID string `gorm:"index" json:"threadId"`
	Role     string `json:"role"` // user | assistant | system
	Content  string `json:"content"`
	// Status is the turn outcome for this message. Empty is treated as "ok".
	// user messages may be "failed" when the assistant side never produced a valid reply.
	Status string `json:"status,omitempty"` // ok | failed
	// FailKind classifies why a turn failed (only when Status=failed).
	// Values: connection | sandbox | empty | unknown | stopped.
	FailKind string `json:"failKind,omitempty"`
	// Source tags how the turn was produced (user chat vs cron vs channel).
	Source string `json:"source,omitempty"` // user | cron | channel | ""
	// Images are optional base64 attachments the user sent with this turn.
	Images          []PromptImage      `gorm:"serializer:json" json:"images,omitempty"`
	Citations       []ProgressCitation `gorm:"serializer:json" json:"citations,omitempty"`
	AttachedContext *AttachedContext   `gorm:"serializer:json" json:"attachedContext,omitempty"`
	// Usage is the assistant turn's token accounting from prompt_done (components
	// summed across model buckets). nil / absent = not reported; non-nil
	// (incl. all zeros) = explicitly reported.
	// Only assistant messages from successful PM turns carry this; Stdio never
	// writes here.
	Usage *TokenUsage `gorm:"serializer:json" json:"usage,omitempty"`
	// UsageByModel is the per-model breakdown after ingest weak-key merge /
	// ACP_BRIDGE_MODEL fill. Usage not covered by a bucket is ledgered as
	// 「未知/未分桶」. Stdio never writes here.
	UsageByModel TokenUsageByModel `gorm:"serializer:json" json:"usageByModel,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// ProgressCitation references a progress object embedded in an assistant reply.
type ProgressCitation struct {
	Type           string `json:"type"` // run | gate | artifact | workflow | plan
	TargetID       string `json:"targetId"`
	SummarySnippet string `json:"summarySnippet,omitempty"`
	// Detail holds optional expanded fields for in-chat expand (not a second truth store).
	Detail map[string]any `json:"detail,omitempty"`
}

// AttachedContext is a user-selected Run or workflow focus for one question.
type AttachedContext struct {
	Kind  string `json:"kind"` // run | workflow
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// ChatTurnDraft is the in-progress assistant partial for one PM consult turn.
// One draft per thread (unique ThreadID). Used for hydrate + catch-up resume.
type ChatTurnDraft struct {
	ID          string `gorm:"primaryKey" json:"id"`
	ThreadID    string `gorm:"uniqueIndex" json:"threadId"`
	UserMsgID   string `gorm:"index" json:"userMsgId"`
	PartialText string `json:"partialText"`
	// ChunkIndex is the number of agent_message_chunk texts accumulated so far.
	ChunkIndex int `json:"chunkIndex"`
	// EventSeq is the last buffered turn-event sequence (for WS catch-up).
	EventSeq int `json:"eventSeq"`
	// Status: streaming | done | failed
	Status    string    `json:"status"`
	FailKind  string    `json:"failKind,omitempty"`
	SandboxID uint      `json:"sandboxId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AgentCronJob is an Agent-owned scheduled task (task-scheduler MCP).
type AgentCronJob struct {
	ID           string `gorm:"primaryKey" json:"id"`
	AgentName    string `gorm:"index" json:"agentName"`
	ProjectID    string `gorm:"index" json:"projectId"`
	ThreadID     string `gorm:"index" json:"threadId"`
	Name         string `json:"name"`
	Prompt       string `json:"prompt"`
	ScheduleKind string `json:"scheduleKind"` // at | every | cron
	ScheduleExpr string `json:"scheduleExpr"`
	Timezone     string `json:"timezone,omitempty"`
	Enabled      bool   `gorm:"index" json:"enabled"`
	// DeliverToChannel pushes the turn result to the channel bound to this
	// job's AgentName (per-job switch). No-op when that Agent has no channel
	// with CronDeliver enabled.
	DeliverToChannel  bool       `json:"deliverToChannel"`
	NextRunAt         *time.Time `gorm:"index" json:"nextRunAt,omitempty"`
	LastRunAt         *time.Time `json:"lastRunAt,omitempty"`
	LastStatus        string     `json:"lastStatus,omitempty"`
	LastError         string     `json:"lastError,omitempty"`
	ConsecutiveErrors int        `json:"consecutiveErrors"`
	ClaimedAt         *time.Time `json:"claimedAt,omitempty"`
	ClaimOwner        string     `json:"claimOwner,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// ChannelConfig is an external IM channel binding managed via the admin WebUI.
// A project may have multiple QQ channels (one primary + zero or more secondary).
// Each channel binds exactly one Agent for inbound turns; AppID stays globally
// unique. The design is channel-agnostic: Type selects the adapter, and Config
// carries adapter-specific extras.
//
// The app secret is stored AES-GCM encrypted (crypto.Encrypt) in AppSecretEnc
// and never serialized to the client.
type ChannelConfig struct {
	ID   string `gorm:"primaryKey" json:"id"`
	Type string `gorm:"index" json:"type"` // qq | wecom | feishu | dingtalk
	Name string `json:"name"`
	// Enabled toggles whether the Manager starts an adapter for this row.
	Enabled bool `gorm:"index" json:"enabled"`
	// ProjectID is the project this bot serves.
	ProjectID string `gorm:"index" json:"projectId"`
	// AgentName is the PM Agent that answers inbound turns on this channel.
	// At most one channel per Agent (enforced in service + partial unique index).
	AgentName string `gorm:"index" json:"agentName"`
	// IsPrimary marks the project's primary channel (at most one per project).
	// Its agent mirrors Project.PmLeaderAgent for Web/gate paths; secondary
	// channels have equal channel capabilities.
	IsPrimary bool `gorm:"index" json:"isPrimary"`
	// EnabledMcps lists platform PM role MCP ids for channel turns only
	// (pm-progress / pm-workflow-read / pm-workflow-write / pm-agent-fs / pm-prd-manager).
	// nil → defaults; explicit empty → none. Web/gate still use Project.PmEnabledMcps.
	EnabledMcps []string `gorm:"serializer:json" json:"enabledMcps,omitempty"`
	// AppID is the bot's public app id (unique across configs).
	AppID string `gorm:"index" json:"appId"`
	// AppSecretEnc is the AES-GCM encrypted app secret (never returned).
	AppSecretEnc string `json:"-"`
	// TurnTimeoutSeconds overrides the PM turn deadline for this channel's turns
	// (0 → platform default). Applies only to channel/cron turns, not workflow nodes.
	TurnTimeoutSeconds int `json:"turnTimeoutSeconds"`
	// CronDeliver marks this channel as the cron result push target for its
	// bound Agent's jobs (not a project-wide singleton).
	CronDeliver bool `json:"cronDeliver"`
	// CronDeliverTarget is the push destination as "scene:conversationId"
	// (e.g. "guild:123", "group:openid", "c2c:openid").
	CronDeliverTarget string `json:"cronDeliverTarget,omitempty"`
	// Config holds adapter-specific extra settings (sandbox base URL, intents…).
	Config    map[string]any `gorm:"serializer:json" json:"config,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Channel types.
const (
	ChannelTypeQQ       = "qq"
	ChannelTypeWeCom    = "wecom"
	ChannelTypeFeishu   = "feishu"
	ChannelTypeDingTalk = "dingtalk"
)

// RegisteredChannelTypes returns channel types the platform can persist/start.
func RegisteredChannelTypes() []string {
	return []string{ChannelTypeQQ, ChannelTypeWeCom, ChannelTypeFeishu, ChannelTypeDingTalk}
}

// IsRegisteredChannelType reports whether typ is a known adapter type.
func IsRegisteredChannelType(typ string) bool {
	switch strings.TrimSpace(typ) {
	case ChannelTypeQQ, ChannelTypeWeCom, ChannelTypeFeishu, ChannelTypeDingTalk:
		return true
	default:
		return false
	}
}

// AgentCronRun is one execution record for an AgentCronJob.
type AgentCronRun struct {
	ID         string     `gorm:"primaryKey" json:"id"`
	JobID      string     `gorm:"index" json:"jobId"`
	Status     string     `json:"status"` // ok | skipped | error
	Error      string     `json:"error,omitempty"`
	SandboxID  uint       `json:"sandboxId,omitempty"`
	MessageID  string     `json:"messageId,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}
