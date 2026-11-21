package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/chatcmd"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func (c *acpProvider) skillPrompt(req NodeReq, sb *sandbox.Sandbox, home, human string) (string, error) {
	if req.UserSkill == "" {
		return human, nil
	}
	profile := models.AgentProfile(req.Config)
	if home != "" {
		if !chatcmd.InstalledSkill(req.UserSkill, home) {
			return "", chatcmd.UnknownSkill(req.UserSkill)
		}
	} else if !chatcmd.InstalledSkill(req.UserSkill, c.workDir(profile), c.sharedWorkDir(req)) {
		return "", chatcmd.UnknownSkill(req.UserSkill)
	}
	root := sb.ConfigRoot
	if root == "" {
		root = c.agentLayout(profile, c.effectiveAgent(req)).ConfigRoot
	}
	return chatcmd.WithSkill(root, req.UserSkill, human), nil
}

func workflowContextPrompt(base string, state models.ChatContext, human string) string {
	if state.Pending == chatcmd.Compact && len(state.Messages) > 0 {
		base += "\n\n## 对话上下文摘录\n" + renderHandoff(state.Messages)
	}
	return strings.TrimSpace(base) + "\n\n## 用户消息\n" + human
}
