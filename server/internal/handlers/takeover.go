package handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

type takeoverBody struct {
	// Iteration optionally pins the visit the user saw; a stale value is
	// refused with node_not_current. 0 / absent = the current visit.
	Iteration int `json:"iteration"`
}

// TakeoverNode interrupts an auto Agent node and switches this visit to a
// manual session for good. 200 is idempotent (taken_over | already_manual);
// 409 carries {error: code, message, current}; 404 for an unknown run/node.
func (h *Handlers) TakeoverNode(c *gin.Context) {
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	var b takeoverBody
	if err := c.ShouldBindJSON(&b); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if b.Iteration < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "iteration must be >= 0"})
		return
	}
	actor := h.auditActorFromContext(c)
	res, err := h.Eng.TakeoverNode(runID, nodeID, b.Iteration)
	if err != nil {
		var te *engine.TakeoverError
		if errors.As(err, &te) {
			if te.NotFound() {
				c.JSON(http.StatusNotFound, gin.H{"error": te.Code, "message": te.Message})
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": te.Code, "message": te.Message, "current": te.Current})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if res.Status == engine.TakeoverStatusTakenOver {
		h.recordAudit(services.AuditRecord{
			Actor:        actor,
			Action:       models.AuditActionNodeTakeover,
			ResourceType: "run",
			ResourceID:   runID,
			RunID:        runID,
			Outcome:      models.AuditOutcomeOK,
			Summary:      "take over auto node " + nodeID,
			Payload:      map[string]any{"runId": runID, "nodeId": nodeID, "iteration": res.Iteration},
		})
	}
	c.JSON(http.StatusOK, res)
}
