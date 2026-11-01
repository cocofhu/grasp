package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NodeDialogue returns a node's current full dialogue state, the same value
// the run WebSocket sends as type:"dialogue". Clients call it when a tail
// frame does not fit the state they hold.
func (h *Handlers) NodeDialogue(c *gin.Context) {
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	if _, ok := h.Runs.Get(runID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	st, ok := h.Eng.DialogueSnapshot(runID, nodeID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, st)
}
