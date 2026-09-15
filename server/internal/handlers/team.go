package handlers

import (
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// BootstrapAgentTeam handles POST /api/agent-teams/bootstrap.
func (h *Handlers) BootstrapAgentTeam(c *gin.Context) {
	if h.Team == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "team service unavailable"})
		return
	}
	var req services.TeamBootstrapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sess, err := h.Team.Bootstrap(c.Request.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, services.ErrTeamAgentConflict), errors.Is(err, services.ErrProjectNameExists):
			status = http.StatusConflict
		case errors.Is(err, services.ErrTeamValidation), errors.Is(err, services.ErrInvalidAgentName),
			errors.Is(err, services.ErrEmptyProjectName):
			status = http.StatusBadRequest
		default:
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, sess)
}

// GetAgentTeamBootstrap handles GET /api/agent-teams/bootstrap/:id.
func (h *Handlers) GetAgentTeamBootstrap(c *gin.Context) {
	if h.Team == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "team service unavailable"})
		return
	}
	sess, err := h.Team.GetSession(c.Param("id"))
	if err != nil {
		if errors.Is(err, services.ErrTeamSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sess)
}

// RetryAgentTeamBootstrap handles POST /api/agent-teams/bootstrap/:id/retry.
func (h *Handlers) RetryAgentTeamBootstrap(c *gin.Context) {
	if h.Team == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "team service unavailable"})
		return
	}
	sess, err := h.Team.Retry(c.Request.Context(), c.Param("id"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, services.ErrTeamSessionNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, sess)
}

// ListAgentTeamTemplates handles GET /api/agent-teams/templates.
// Includes solo extras (preflight); bootstrap roster stays 1 PM + 9.
func (h *Handlers) ListAgentTeamTemplates(c *gin.Context) {
	if h.Team == nil {
		c.JSON(http.StatusOK, gin.H{"items": services.AllCreateTemplates()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": h.Team.ListTemplates()})
}
