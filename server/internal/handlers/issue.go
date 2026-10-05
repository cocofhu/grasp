package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

// createPreviewIssueBody is the request payload for CreatePreviewIssue.
type createPreviewIssueBody struct {
	Body     string               `json:"body"`
	Selector string               `json:"selector"`
	Port     int                  `json:"port"`
	Images   []models.PromptImage `json:"images"`
}

// CreatePreviewIssue records a human-reported problem against a preview-capable
// node from the UI feedback chat. It is one-way feedback: the engine snapshots
// the issues into the preview_issues run variable at gate resume so a
// downstream node consumes them via {{vars.preview_issues}}.
func (h *Handlers) CreatePreviewIssue(c *gin.Context) {
	runID := c.Param("id")
	nodeID := c.Param("nodeId")
	if h.Issues == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "preview feedback unavailable"})
		return
	}
	var body createPreviewIssueBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	body.Body = strings.TrimSpace(body.Body)
	// The UI lets a reviewer submit a screenshot-only report (no text), so accept
	// the issue as long as it carries either a body or at least one image.
	if body.Body == "" && len(body.Images) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body or images required"})
		return
	}
	issue, err := h.Issues.Create(runID, nodeID, body.Body, strings.TrimSpace(body.Selector), body.Port, body.Images)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, issue)
}

// ListPreviewIssues returns the issues reported against a run/node for the UI.
func (h *Handlers) ListPreviewIssues(c *gin.Context) {
	runID := c.Param("id")
	nodeID := c.Param("nodeId")
	if h.Issues == nil {
		c.JSON(http.StatusOK, gin.H{"issues": []any{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"issues": h.Issues.ListByRunNode(runID, nodeID)})
}

// DeletePreviewIssue hard-removes one reported issue (the user deletes their
// own feedback from the UI).
func (h *Handlers) DeletePreviewIssue(c *gin.Context) {
	if h.Issues == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "preview feedback unavailable"})
		return
	}
	if err := h.Issues.Delete(c.Param("id"), c.Param("nodeId"), c.Param("issueId")); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
