package handlers

import (
	"net/http"
	"strconv"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type visitModelBody struct {
	Iteration       int     `json:"iteration"`
	ConfigurationID *string `json:"configurationId"`
	ModelID         *string `json:"modelId"`
	ReasoningEffort *string `json:"reasoningEffort"`
}

// GetVisitModel handles GET /api/runs/:id/nodes/:nodeId/model?iteration=.
func (h *Handlers) GetVisitModel(c *gin.Context) {
	iteration, _ := strconv.Atoi(c.Query("iteration"))
	choice, err := services.GetVisitModel(h.Runs.DB(), c.Param("id"), c.Param("nodeId"), iteration)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, choice)
}

// SetVisitModel handles POST /api/runs/:id/nodes/:nodeId/model.
// It records the choice for this visit only. The sandbox is aligned when the
// next chat turn starts, so a turn that is already generating keeps its model.
func (h *Handlers) SetVisitModel(c *gin.Context) {
	if h.Agents == nil || h.Runs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model choice unavailable"})
		return
	}
	var body visitModelBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	run, ok := h.Runs.Get(runID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	node := run.Graph.FindNode(nodeID)
	if node == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	profile := models.AgentProfile(node.Config)
	agent, found := h.Agents.Get(profile)
	if profile == "" || !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Agent 不存在", "code": "agent_not_found"})
		return
	}
	choice, err := services.SetVisitModel(h.Runs.DB(), h.Agents.Effective(agent), runID, nodeID, body.Iteration, body.ConfigurationID, body.ModelID, body.ReasoningEffort)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, choice)
}
