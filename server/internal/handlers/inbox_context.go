package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// RunInboxContext returns gate or clarify approval context for a pending inbox
// item (GET /api/runs/:id/inbox-context?nodeId=&iteration=).
func (h *Handlers) RunInboxContext(c *gin.Context) {
	runID := c.Param("id")
	nodeID := c.Query("nodeId")
	iterStr := c.Query("iteration")
	if nodeID == "" || iterStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nodeId and iteration are required"})
		return
	}
	iteration, err := strconv.Atoi(iterStr)
	if err != nil || iteration < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "iteration must be a positive integer"})
		return
	}
	run, ok := h.Runs.Get(runID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	kind, ok := h.Runs.InboxContextKind(runID, nodeID, iteration)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "no pending inbox item"})
		return
	}

	switch kind {
	case services.InboxKindGate:
		h.inboxContextGate(c, runID, nodeID, iteration)
	case services.InboxKindClarify:
		h.inboxContextClarify(c, runID, nodeID, iteration)
	case services.InboxKindClarifyStarting:
		h.inboxContextStarting(c, run, nodeID, iteration)
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "no pending inbox item"})
	}
}

func (h *Handlers) inboxContextGate(c *gin.Context, runID, gateNodeID string, iteration int) {
	run, ok := h.Runs.Get(runID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	arts := h.Arts.ByRun(runID)
	artsDTO := make([]gin.H, 0, len(arts))
	for _, a := range arts {
		artsDTO = append(artsDTO, artifactMetaDTO(a))
	}

	gateNode := run.Graph.FindNode(gateNodeID)
	upstreamIDs := services.GateUpstreamNodeIDs(gateNode)
	slimExecs := h.Runs.SlimNodeExecutions(runID, upstreamIDs)

	out := gin.H{
		"type":           "gate",
		"nodes":          graphNodesDTO(run.Graph),
		"artifacts":      artsDTO,
		"nodeExecutions": slimExecs,
	}
	// Gate DTO subset + reactSessions for Inbox hard-refresh seed-then-live
	// (parity with runDetailDTO / clarify inbox-context).
	if g, ok := h.Runs.PendingGate(runID); ok && g.NodeID == gateNodeID {
		gateDTO := gin.H{
			"runId": g.RunID, "nodeId": g.NodeID, "iteration": g.Iteration,
			"workflowId": g.WorkflowID, "workflowName": g.WorkflowName,
			"title": g.Title, "bodyMd": g.BodyMd, "actions": g.Actions, "form": g.Form,
			"upstreamNodeId": g.UpstreamNodeID, "upstreamIteration": g.UpstreamIteration,
			"requestedAt": g.RequestedAt,
		}
		if h.Eng != nil {
			if pid, alive := h.Eng.GateReactInfo(runID, g.NodeID); pid != "" {
				gateDTO["reactUpstreamNodeId"] = pid
				gateDTO["reactSessionAlive"] = alive
			}
		}
		out["gate"] = gateDTO
	}
	if h.Eng != nil {
		if byNode := reactSessionsDTO(h.Eng.ReviewSessionsForRun(runID)); byNode != nil {
			out["reactSessions"] = byNode
		}
	}
	c.JSON(http.StatusOK, out)
}

// inboxContextStarting serves the clarify context of a booting clarify node:
// same shape as a normal clarify context but with an empty transcript and
// starting=true, so the inbox detail pane can render the boot loader.
func (h *Handlers) inboxContextStarting(c *gin.Context, run models.Run, nodeID string, iteration int) {
	c.JSON(http.StatusOK, gin.H{
		"type":           "clarify",
		"status":         run.Status,
		"nodes":          graphNodesDTO(run.Graph),
		"artifacts":      []gin.H{},
		"nodeExecutions": map[string][]map[string]any{},
		"clarify": gin.H{
			"nodeId":    nodeID,
			"iteration": iteration,
			"turns":     []models.ReactMessage{},
			"done":      false,
			"label":     services.ClarifyLabel(run.Graph, nodeID),
			"starting":  true,
		},
	})
}

func (h *Handlers) inboxContextClarify(c *gin.Context, runID, nodeID string, iteration int) {
	conv, run, ok := h.Runs.ClarifyContext(runID, nodeID, iteration)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	arts := h.Arts.ByRun(runID)
	artsDTO := make([]gin.H, 0, len(arts))
	for _, a := range arts {
		artsDTO = append(artsDTO, artifactMetaDTO(a))
	}

	clarifyNode := run.Graph.FindNode(nodeID)
	slimIDs := services.ClarifySlimNodeIDs(clarifyNode, nodeID, arts)
	slimExecs := h.Runs.SlimNodeExecutions(runID, slimIDs)

	clarify := gin.H{
		"nodeId":    conv.NodeID,
		"iteration": conv.Iteration,
		"turns":     conv.Turns(),
		"done":      conv.Done,
		"label":     services.ClarifyLabel(run.Graph, conv.NodeID),
	}
	if name := strings.TrimSpace(conv.PreviewArtifact); name != "" {
		clarify["previewArtifact"] = name
	}
	out := gin.H{
		"type":           "clarify",
		"status":         run.Status,
		"nodes":          graphNodesDTO(run.Graph),
		"artifacts":      artsDTO,
		"nodeExecutions": slimExecs,
		"clarify":        clarify,
	}
	// Authoritative busy/queue for GatesInbox refresh-resume (parity with runDetailDTO).
	if h.Eng != nil {
		if byNode := reactSessionsDTO(h.Eng.ReviewSessionsForRun(runID)); byNode != nil {
			out["reactSessions"] = byNode
		}
	}
	c.JSON(http.StatusOK, out)
}
