package handlers

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// PublicAppPreviewMainCancel stops the conversation explicitly shared by its owner.
// Isolated links cannot control the original conversation.
func (h *Handlers) PublicAppPreviewMainCancel(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	link, sb := h.publicAppShare(c)
	if link == nil {
		return
	}
	if link.PreviewChatMode != models.PreviewChatMain {
		c.JSON(http.StatusForbidden, gin.H{"error": "main conversation is private"})
		return
	}
	var err error
	var body struct {
		TurnID string `json:"turnId"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if c.ShouldBindJSON(&body) != nil || body.TurnID == "" || len(body.TurnID) > 256 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_turn"})
		return
	}
	if sb.Purpose == services.SandboxPurposeSolo {
		if h.Solo == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
			return
		}
		err = h.Solo.CancelPreviewTurn(sb.ThreadID, link.CreatedBy, body.TurnID)
	} else {
		state, active := h.workflowPreviewDialogue(link.RunID, link.NodeID)
		if !active || state.Iteration != link.Iteration {
			c.JSON(http.StatusConflict, gin.H{"error": "main conversation unavailable"})
			return
		}
		if !h.Eng.CancelPreviewShareTurn(link.RunID, link.NodeID, body.TurnID) {
			c.JSON(http.StatusConflict, gin.H{"error": "turn_changed", "code": "turn_changed"})
			return
		}
	}
	if err != nil {
		writePreviewMainError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
