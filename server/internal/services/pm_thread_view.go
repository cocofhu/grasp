package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// PmChatThread adds current Agent availability to the persisted conversation.
// Availability describes this thread's Agent, independently of the default
// Agent, the workspace send switch and the caller's write permissions.
type PmChatThread struct {
	models.ChatThread
	AgentAvailable bool   `json:"agentAvailable"`
	AgentError     string `json:"agentError,omitempty"`
}

type threadAgentAvailability struct {
	available bool
	errorText string
}

func (s *PmService) threadAgentAvailability(thread models.ChatThread) threadAgentAvailability {
	name := strings.TrimSpace(thread.AgentName)
	if name == "" {
		return threadAgentAvailability{errorText: "会话未绑定 Agent"}
	}
	if s == nil || s.skills == nil {
		return threadAgentAvailability{errorText: "会话的 Agent 已删除或不可用"}
	}
	if !s.skills.Exists(name) {
		return threadAgentAvailability{errorText: "会话的 Agent 已删除或不可用"}
	}
	// The thread list needs identity metadata, not the Agent's workspace files.
	config := s.skills.readConfig(name)
	if !AgentProjectMatches(Agent{ProjectID: config.ProjectID}, thread.ProjectID) {
		return threadAgentAvailability{errorText: "会话 Agent 不属于当前工作区"}
	}
	return threadAgentAvailability{available: true}
}

func (s *PmService) ThreadView(thread models.ChatThread) PmChatThread {
	availability := s.threadAgentAvailability(thread)
	return PmChatThread{ChatThread: thread, AgentAvailable: availability.available, AgentError: availability.errorText}
}

// ThreadViews resolves each distinct Agent once per response. It does not cache
// availability across requests, so deletions and repairs are reflected promptly.
func (s *PmService) ThreadViews(threads []models.ChatThread) []PmChatThread {
	out := make([]PmChatThread, 0, len(threads))
	type identity struct{ workspaceID, agentName string }
	resolved := make(map[identity]threadAgentAvailability)
	for _, thread := range threads {
		key := identity{thread.ProjectID, strings.TrimSpace(thread.AgentName)}
		availability, ok := resolved[key]
		if !ok {
			availability = s.threadAgentAvailability(thread)
			resolved[key] = availability
		}
		out = append(out, PmChatThread{ChatThread: thread, AgentAvailable: availability.available, AgentError: availability.errorText})
	}
	return out
}
