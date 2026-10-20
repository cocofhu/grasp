package services

import "strings"

// Relation of a workspace Agent to the calling PM in WorkspaceAgentsView.
const (
	WorkspaceAgentRelationSelf  = "self"
	WorkspaceAgentRelationOther = "other"
)

// WorkspaceAgentMember is one Agent in the workspace.
type WorkspaceAgentMember struct {
	Name       string `json:"name"`
	AcpBackend string `json:"acpBackend,omitempty"`
	Relation   string `json:"relation"` // self|other
}

// WorkspaceAgentsView is the pm_list_workspace_agents response.
type WorkspaceAgentsView struct {
	Self   string                 `json:"self"`
	Agents []WorkspaceAgentMember `json:"agents"`
}

// ListWorkspaceAgents returns every Agent in the order given (AgentService.List
// is name-sorted), marking self as the caller.
func ListWorkspaceAgents(all []Agent, self string) WorkspaceAgentsView {
	self = strings.TrimSpace(self)
	out := WorkspaceAgentsView{
		Self:   self,
		Agents: []WorkspaceAgentMember{},
	}
	for _, a := range all {
		rel := WorkspaceAgentRelationOther
		if a.Name == self {
			rel = WorkspaceAgentRelationSelf
		}
		out.Agents = append(out.Agents, WorkspaceAgentMember{
			Name:       a.Name,
			AcpBackend: a.AcpBackend,
			Relation:   rel,
		})
	}
	return out
}
