package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// BootstrapProjectOnboarding handles POST /api/projects/:id/bootstrap-onboarding.
// Any project: shared auth, the chosen built-in template Agents, 默认工作流.
// It never starts a Run. Missing apiKey or an invalid team → 400 with no partial
// resources created.
func (h *Handlers) BootstrapProjectOnboarding(c *gin.Context) {
	if h.Onboarding == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "onboarding unavailable"})
		return
	}
	projectID := strings.TrimSpace(c.Param("id"))
	var req services.OnboardingBootstrapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.Onboarding.Bootstrap(projectID, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrOnboardingAPIKeyRequired),
			errors.Is(err, services.ErrInvalidAgentName),
			errors.Is(err, services.ErrOnboardingInvalidTeam):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrOnboardingProjectNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrOnboardingAgentConflict):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case isSecretsKeyErr(err):
			writeSecretsKeyErr(c, err)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateWorkflowFromBaseline handles POST /api/workflows/from-baseline.
func (h *Handlers) CreateWorkflowFromBaseline(c *gin.Context) {
	if h.Onboarding == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "baseline workflow service unavailable"})
		return
	}
	var req services.CreateBaselineWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	wf, err := h.Onboarding.CreateFromBaseline(req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrEmptyWorkflowName),
			errors.Is(err, services.ErrWorkflowProjectRequired),
			errors.Is(err, services.ErrBaselineReposRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrWorkflowProjectNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrWorkflowNameExists):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    wf.ProjectID,
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionWorkflowCreate,
		ResourceType: "workflow",
		ResourceID:   wf.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "create workflow from embedded baseline",
		Payload: map[string]any{
			"name": wf.Name, "status": wf.Status, "version": wf.Version,
		},
	})
	c.JSON(http.StatusCreated, workflowDTO(wf))
}
