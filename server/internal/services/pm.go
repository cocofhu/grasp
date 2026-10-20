package services

import (
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

var (
	// ErrPmLeaderDisabled is returned when consult APIs are called while PM Leader is off.
	ErrPmLeaderDisabled = errors.New("PM Leader 未启用")
	// ErrPmLeaderNoAgent is returned when enabling without a bound agent, or the agent is missing.
	ErrPmLeaderNoAgent = errors.New("未绑定可用 Agent，请先到 Agent 配置中心准备后再绑定")
	// ErrPmLeaderAgentMissing is returned when the bound agent was deleted.
	ErrPmLeaderAgentMissing = errors.New("绑定的 Agent 已删除或不可用，请管理员重新绑定")
	// ErrPmMemoryNotFound is returned when a memory item is missing.
	ErrPmMemoryNotFound = errors.New("记忆条目不存在")
	// ErrPmThreadNotFound is returned when a chat thread is missing or not owned.
	ErrPmThreadNotFound = errors.New("会话不存在")
	// ErrPmMessageNotFound is returned when a chat message is missing.
	ErrPmMessageNotFound = errors.New("消息不存在")
	// ErrPmMessageInvalidStatus is returned for unsupported status/failKind updates.
	ErrPmMessageInvalidStatus = errors.New("无效的消息失败状态")
	// ErrPmMessageInvalidRole is returned when failure metadata is applied to a non-user message.
	ErrPmMessageInvalidRole = errors.New("仅支持标记用户消息的失败状态")
	// ErrPmAdminRequired is returned for admin-only memory / admin paths (not PM Leader bind).
	ErrPmAdminRequired = errors.New("需要平台管理员权限")
	// ErrPmCronJobNotFound is returned when a cron job is missing.
	ErrPmCronJobNotFound = errors.New("定时任务不存在")
	// ErrPmChannelReadOnly is returned when a Web client tries to write/delete a channel thread.
	ErrPmChannelReadOnly = errors.New("渠道会话为只读，不可当普通 Web 线程编辑或删除")
)

// IsChannelUserID reports whether userID is a registered channel synthetic identity
// (qq:… / wecom:… / feishu:…).
func IsChannelUserID(userID string) bool {
	for _, typ := range models.RegisteredChannelTypes() {
		if strings.HasPrefix(userID, typ+":") {
			return true
		}
	}
	return false
}

// ChannelTypeFromUserID returns the registered channel type prefix, or "".
func ChannelTypeFromUserID(userID string) string {
	for _, typ := range models.RegisteredChannelTypes() {
		if strings.HasPrefix(userID, typ+":") {
			return typ
		}
	}
	return ""
}

// ChannelPeerID extracts the conversation/user id after type:scene:.
func ChannelPeerID(userID string) string {
	parts := strings.SplitN(userID, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// IsSyntheticThreadUserID reports cron/channel synthetic owners that must not trigger UI merge.
func IsSyntheticThreadUserID(userID string) bool {
	return IsChannelUserID(userID) || strings.HasPrefix(userID, "cron:")
}

// Valid PM chat fail kinds (product-level categories).
const (
	PmFailConnection = "connection"
	PmFailSandbox    = "sandbox"
	PmFailEmpty      = "empty"
	PmFailUnknown    = "unknown"
	PmFailStopped    = "stopped"
	// PmFailInterrupted marks a turn cut off by a server restart.
	PmFailInterrupted = "interrupted"
)

func validPmFailKind(kind string) bool {
	switch kind {
	case PmFailConnection, PmFailSandbox, PmFailEmpty, PmFailUnknown, PmFailStopped, PmFailInterrupted:
		return true
	default:
		return false
	}
}

// PmService manages PM Leader binding, memory, and chat persistence.
type PmService struct {
	blobs  blob.Store
	db     *gorm.DB
	skills *AgentService
}

// NewPmService builds the service.
func NewPmService(db *gorm.DB, skills *AgentService) *PmService {
	return &PmService{db: db, skills: skills}
}

// SetBlobStore wires attachment externalization for chat messages.
func (s *PmService) SetBlobStore(store blob.Store) { s.blobs = store }

// --- binding ----------------------------------------------------------------

// PmLeaderBinding is the API shape for enable/bind state.
type PmLeaderBinding struct {
	Enabled        bool   `json:"enabled"`
	AgentConfigRef string `json:"agentConfigRef"`
	AgentAvailable bool   `json:"agentAvailable"`
	AgentError     string `json:"agentError,omitempty"`
	// EnabledMcps lists PM-only MCP ids (pm-progress, pm-workflow-read,
	// pm-workflow-write, pm-agent-fs, pm-prd-manager). nil/omitted on disk means defaults; explicit empty means none.
	EnabledMcps []string `json:"enabledMcps"`
	// GateAutoVar is the run variable name that enables auto-invoking PM on
	// gate pauses when present and truthy. Empty = capability off.
	GateAutoVar string `json:"gateAutoVar"`
	// GateAutoPrompt is optional text appended after the system default
	// gate-auto guidance (may be empty).
	GateAutoPrompt string `json:"gateAutoPrompt"`
	// AclNote points users to Agent Studio for memory management.
	AclNote string `json:"aclNote"`
}

// DefaultPmEnabledMcps is the default PM-only MCP set.
var DefaultPmEnabledMcps = []string{"pm-progress", "pm-workflow-read", "pm-workflow-write", "pm-agent-fs", "pm-prd-manager"}

// FilterPmEnabledMcps returns validated unique PM MCP ids (may be empty).
func FilterPmEnabledMcps(in []string) []string {
	allowed := map[string]bool{
		"pm-progress": true, "pm-workflow-read": true, "pm-workflow-write": true,
		"pm-agent-fs": true, "pm-prd-manager": true,
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range in {
		id = strings.TrimSpace(id)
		if !allowed[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if out == nil {
		return []string{}
	}
	return out
}

// EffectivePmEnabledMcps resolves stored values: nil → default both; non-nil → filtered (may be empty).
func EffectivePmEnabledMcps(stored []string) []string {
	if stored == nil {
		return append([]string{}, DefaultPmEnabledMcps...)
	}
	return FilterPmEnabledMcps(stored)
}

const pmLeaderAclNote = "记忆请在 Agent Studio「数据 → 记忆」中管理；任意已登录用户可编辑。"

// GetBinding returns the PM Leader binding.
func (s *PmService) GetBinding() (PmLeaderBinding, error) {
	p, err := loadWorkspace(s.db)
	if err != nil {
		return PmLeaderBinding{}, err
	}
	b := PmLeaderBinding{
		Enabled:        p.PmLeaderEnabled,
		AgentConfigRef: p.PmLeaderAgent,
		EnabledMcps:    EffectivePmEnabledMcps(p.PmEnabledMcps),
		GateAutoVar:    p.PmGateAutoVar,
		GateAutoPrompt: p.PmGateAutoPrompt,
		AclNote:        pmLeaderAclNote,
	}
	if p.PmLeaderAgent == "" {
		b.AgentAvailable = false
		if p.PmLeaderEnabled {
			b.AgentError = ErrPmLeaderNoAgent.Error()
		}
		return b, nil
	}
	if s.skills != nil {
		if _, ok := s.skills.Get(p.PmLeaderAgent); !ok {
			b.AgentAvailable = false
			b.AgentError = ErrPmLeaderAgentMissing.Error()
		} else {
			b.AgentAvailable = true
		}
	} else {
		b.AgentAvailable = true
	}
	return b, nil
}

// UpdateBinding patches enable/agent/enabledMcps/gate-auto fields.
// Enabling requires a resolvable agent. gateAutoVar/gateAutoPrompt are optional
// patches (nil = leave unchanged); empty string clears. Variable existence/type
// is not validated at save time.
func (s *PmService) UpdateBinding(enabled *bool, agent *string, enabledMcps []string, gateAutoVar, gateAutoPrompt *string) (PmLeaderBinding, error) {
	p, err := loadWorkspace(s.db)
	if err != nil {
		return PmLeaderBinding{}, err
	}
	if agent != nil {
		name := strings.TrimSpace(*agent)
		if name != "" && s.skills != nil {
			if _, ok := s.skills.Get(name); !ok {
				return PmLeaderBinding{}, ErrPmLeaderNoAgent
			}
		}
		p.PmLeaderAgent = name
	}
	if enabledMcps != nil {
		// Explicit empty list disables all PM-only MCPs (do not expand to defaults).
		p.PmEnabledMcps = FilterPmEnabledMcps(enabledMcps)
	}
	if gateAutoVar != nil {
		p.PmGateAutoVar = strings.TrimSpace(*gateAutoVar)
	}
	if gateAutoPrompt != nil {
		p.PmGateAutoPrompt = *gateAutoPrompt
	}
	if enabled != nil {
		if *enabled {
			if strings.TrimSpace(p.PmLeaderAgent) == "" {
				return PmLeaderBinding{}, ErrPmLeaderNoAgent
			}
			if s.skills != nil {
				if _, ok := s.skills.Get(p.PmLeaderAgent); !ok {
					return PmLeaderBinding{}, ErrPmLeaderNoAgent
				}
			}
		}
		p.PmLeaderEnabled = *enabled
	}
	p.UpdatedAt = time.Now()
	if err := s.db.Save(&p).Error; err != nil {
		return PmLeaderBinding{}, err
	}
	return s.GetBinding()
}

// RequireEnabled checks the workspace has PM Leader on with a usable agent.
func (s *PmService) RequireEnabled() (models.Workspace, error) {
	p, err := loadWorkspace(s.db)
	if err != nil {
		return models.Workspace{}, err
	}
	if !p.PmLeaderEnabled {
		return models.Workspace{}, ErrPmLeaderDisabled
	}
	if strings.TrimSpace(p.PmLeaderAgent) == "" {
		return models.Workspace{}, ErrPmLeaderNoAgent
	}
	if s.skills != nil {
		if _, ok := s.skills.Get(p.PmLeaderAgent); !ok {
			return models.Workspace{}, ErrPmLeaderAgentMissing
		}
	}
	return p, nil
}

// RequireSendEnabled checks the workspace switch independently of the default
// Agent. Existing threads resolve their own AgentName.
func (s *PmService) RequireSendEnabled() (models.Workspace, error) {
	p, err := loadWorkspace(s.db)
	if err != nil {
		return p, err
	}
	if !p.PmLeaderEnabled {
		return p, ErrPmLeaderDisabled
	}
	return p, nil
}
