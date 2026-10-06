package pmmcp

import (
	"strings"

	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"
)

func (h *Host) callTeamTools(sess *Session, skill *services.AgentService, name string, args map[string]any) (any, bool) {
	h.mu.RLock()
	team := h.team
	h.mu.RUnlock()
	if team == nil {
		return map[string]any{"error": "team service unavailable"}, true
	}
	if strings.TrimSpace(sess.ProjectID) == "" {
		return map[string]any{"error": "session missing project"}, true
	}
	if strings.TrimSpace(sess.AgentName) == "" {
		return map[string]any{"error": "pm leader not bound"}, true
	}

	switch name {
	case "pm_list_agent_templates":
		return map[string]any{"items": team.ListTemplates()}, false

	case "pm_create_agent_from_template":
		templateID := strings.TrimSpace(platformmcp.StrArg(args, "templateId"))
		agentName := strings.TrimSpace(platformmcp.StrArg(args, "name"))
		if templateID == "" || agentName == "" {
			return map[string]any{"error": "templateId and name are required"}, true
		}
		leader, ok := skill.Get(sess.AgentName)
		if !ok {
			return map[string]any{"error": "leader agent not found"}, true
		}
		if !services.AgentProjectMatches(leader, sess.ProjectID) {
			return map[string]any{"error": "leader not bound to session project"}, true
		}
		created, err := team.CreateAgentFromTemplate(services.CreateFromTemplateArgs{
			TemplateID:  templateID,
			Name:        agentName,
			ProjectID:   sess.ProjectID,
			AcpBackend:  leader.AcpBackend,
			GitCredType: leader.GitCredentialType,
			MCP:         leader.MCP,
			Env:         leader.Env,
		})
		if err != nil {
			return map[string]any{"error": err.Error()}, true
		}
		return map[string]any{"ok": true, "agentName": created.Name, "templateId": templateID}, false
	}
	return map[string]any{"error": "unknown tool: " + name}, true
}
