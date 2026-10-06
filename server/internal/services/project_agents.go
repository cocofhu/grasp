package services

import "strings"

// Relation of a project member to the calling PM in ProjectAgentsView.
const (
	ProjectAgentRelationSelf  = "self"
	ProjectAgentRelationOther = "other"
)

// ProjectAgentMember is one Agent bound to the project.
type ProjectAgentMember struct {
	Name       string `json:"name"`
	AcpBackend string `json:"acpBackend,omitempty"`
	Relation   string `json:"relation"` // self|other
}

// ProjectAgentsView is the pm_list_project_agents response.
type ProjectAgentsView struct {
	ProjectID string               `json:"projectId"`
	Self      string               `json:"self"`
	Agents    []ProjectAgentMember `json:"agents"`
}

// ListProjectAgents returns the Agents whose projectId equals projectID, sorted
// as given (AgentService.List is name-sorted), marking self as the caller.
func ListProjectAgents(all []Agent, projectID, self string) ProjectAgentsView {
	self = strings.TrimSpace(self)
	out := ProjectAgentsView{
		ProjectID: strings.TrimSpace(projectID),
		Self:      self,
		Agents:    []ProjectAgentMember{},
	}
	for _, a := range all {
		if !AgentProjectMatches(a, projectID) {
			continue
		}
		rel := ProjectAgentRelationOther
		if a.Name == self {
			rel = ProjectAgentRelationSelf
		}
		out.Agents = append(out.Agents, ProjectAgentMember{
			Name:       a.Name,
			AcpBackend: a.AcpBackend,
			Relation:   rel,
		})
	}
	return out
}
