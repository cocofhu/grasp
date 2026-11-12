package handlers

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// SearchWorkflowReferences handles GET /api/workflows/:id/references/search
// for the home composer in workflow mode. GitHub searches the workflow's bound
// repositories with their own credentials.
func (h *Handlers) SearchWorkflowReferences(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	scope, ok := h.workflowReferenceScope(user, c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	res, err := h.Solo.SearchScopedReferences(c.Request.Context(), scope, referenceQuery(c))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// SearchRunNodeReferences handles GET /api/runs/:id/nodes/:nodeId/references/search
// for a run chat. GitHub searches the run's repositories with their own
// credentials.
func (h *Handlers) SearchRunNodeReferences(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	scope, ok := h.runReferenceScope(user, c.Param("id"), c.Param("nodeId"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	res, err := h.Solo.SearchScopedReferences(c.Request.Context(), scope, referenceQuery(c))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// workflowReferenceScope is the launch scope of a workflow: its bound
// repositories and its first clarify Agent.
func (h *Handlers) workflowReferenceScope(user, workflowID string) (*models.SoloSession, bool) {
	if h.WF == nil {
		return nil, false
	}
	wf, ok := h.WF.Get(workflowID)
	if !ok {
		return nil, false
	}
	var repos []models.SessionRepo
	if wf.Defaults != nil && h.Solo != nil {
		for _, choice := range wf.Defaults.Repos {
			if snap, err := h.Solo.Projects().Snapshot([]models.RepoChoice{choice}); err == nil {
				repos = append(repos, snap...)
			}
		}
	}
	return services.ReferenceScope(user, h.firstClarifyAgent(wf.Graph), repos), true
}

// firstClarifyAgent walks the graph from its start and returns the Agent of
// the first clarify node, which receives the launcher's opening message.
func (h *Handlers) firstClarifyAgent(g models.Graph) string {
	start := g.StartNode()
	if start == nil {
		return ""
	}
	seen := map[string]bool{start.ID: true}
	queue := []string{start.ID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if n := g.FindNode(id); n != nil && n.Type == "agent" {
			profile := models.AgentProfile(n.Config)
			if h.Agents != nil {
				if agent, ok := h.Agents.Get(profile); ok && agent.Capabilities.Clarify() {
					return profile
				}
			}
		}
		for _, e := range g.OutEdges(id) {
			if !seen[e.Target] {
				seen[e.Target] = true
				queue = append(queue, e.Target)
			}
		}
	}
	return ""
}

// runReferenceScope is the scope of one run chat: the run's repositories and
// the Agent of the node that answers (a gate's upstream producer).
func (h *Handlers) runReferenceScope(user, runID, nodeID string) (*models.SoloSession, bool) {
	if h.Runs == nil {
		return nil, false
	}
	run, ok := h.Runs.Get(runID)
	if !ok {
		return nil, false
	}
	node := run.Graph.FindNode(nodeID)
	if node == nil {
		return nil, false
	}
	if node.Type == "human_gate" && h.Eng != nil {
		if producerID, _ := h.Eng.GateReactInfo(runID, nodeID); producerID != "" {
			if p := run.Graph.FindNode(producerID); p != nil {
				node = p
			}
		}
	}
	return services.ReferenceScope(user, models.AgentProfile(node.Config), run.Repos), true
}

// prepareRunReferences validates the @ picks of a run chat message for the
// signed-in sender. It writes the error response and reports false on failure.
func (h *Handlers) prepareRunReferences(c *gin.Context, runID, nodeID string, inputs []services.ReferenceInput) ([]models.MessageReference, bool) {
	if len(inputs) == 0 {
		return nil, true
	}
	user, ok := h.soloReady(c)
	if !ok {
		return nil, false
	}
	scope, ok := h.runReferenceScope(user, runID, nodeID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return nil, false
	}
	refs, err := h.Solo.PrepareScopedReferences(c.Request.Context(), scope, inputs)
	if err != nil {
		writeSoloErr(c, err)
		return nil, false
	}
	return refs, true
}
