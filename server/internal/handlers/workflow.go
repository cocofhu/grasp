package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *Handlers) ListWorkflows(c *gin.Context) {
	wfs := h.WF.List(c.Query("projectId"))
	snaps := h.WF.PublishedSnapshots(wfs)
	out := make([]gin.H, 0, len(wfs))
	for _, wf := range wfs {
		if snap, ok := snaps[wf.ID]; ok {
			cp := snap
			out = append(out, workflowDTO(wf, &cp))
			continue
		}
		out = append(out, workflowDTO(wf))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetWorkflow(c *gin.Context) {
	wf, ok := h.WF.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if snap, found := h.WF.PublishedSnapshot(wf); found {
		c.JSON(http.StatusOK, workflowDTO(wf, &snap))
		return
	}
	c.JSON(http.StatusOK, workflowDTO(wf))
}

type workflowBody struct {
	ID           string                       `json:"id"`
	ProjectID    string                       `json:"projectId"`
	Name         string                       `json:"name"`
	Description  string                       `json:"description"`
	NeedsRepo    bool                         `json:"needsRepo"`
	ShowOnHome   *bool                        `json:"showOnHome"`
	NotifyPolicy *models.WorkflowNotifyPolicy `json:"notifyPolicy"`
	Nodes        []models.Node                `json:"nodes"`
	Edges        []models.Edge                `json:"edges"`
	Variables    []models.Variable            `json:"variables"`
}

func (h *Handlers) SaveWorkflow(c *gin.Context) {
	var b workflowBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if id := c.Param("id"); id != "" {
		b.ID = id
	}
	isCreate := b.ID == "" || c.Request.Method == http.MethodPost
	if b.ID == "" {
		b.ID = "wf-" + uuid.NewString()[:8]
		isCreate = true
	}

	if isCreate && strings.TrimSpace(b.ProjectID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": services.ErrWorkflowProjectRequired.Error()})
		return
	}
	graph := models.Graph{Nodes: b.Nodes, Edges: b.Edges, Variables: b.Variables}
	services.LiftInputVariables(&graph)

	wf := models.WorkflowDef{
		ID: b.ID, ProjectID: b.ProjectID, Name: b.Name, Description: b.Description, NeedsRepo: b.NeedsRepo,
		Graph: graph,
	}
	if b.NotifyPolicy != nil {
		wf.NotifyPolicy = *b.NotifyPolicy
	} else if !isCreate {

		if existing, ok := h.WF.Get(b.ID); ok {
			wf.NotifyPolicy = existing.NotifyPolicy
		}
	}
	// Create always false (handler + service). On update, omit → keep stored
	// value so an editor save cannot silently turn Home visibility off (plan g1.3).
	if isCreate {
		wf.ShowOnHome = false
	} else if b.ShowOnHome != nil {
		wf.ShowOnHome = *b.ShowOnHome
	} else if existing, ok := h.WF.Get(b.ID); ok {
		wf.ShowOnHome = existing.ShowOnHome
	}
	if err := h.WF.Save(&wf); err != nil {
		switch {
		case errors.Is(err, services.ErrEmptyWorkflowName),
			errors.Is(err, services.ErrWorkflowProjectRequired),
			errors.Is(err, services.ErrWorkflowProjectNotFound):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrWorkflowNameExists),
			errors.Is(err, services.ErrWorkflowProjectImmutable):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	action := models.AuditActionWorkflowUpdate
	summary := "update workflow"
	if isCreate {
		action = models.AuditActionWorkflowCreate
		summary = "create workflow"
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    wf.ProjectID,
		Actor:        h.auditActorFromContext(c),
		Action:       action,
		ResourceType: "workflow",
		ResourceID:   wf.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      summary,
		Payload: map[string]any{
			"name":    wf.Name,
			"status":  wf.Status(),
			"version": wf.Version,
			"nodes":   len(wf.Graph.Nodes),
		},
	})
	c.JSON(http.StatusOK, workflowDTO(wf))
}

type workflowNotifyPolicyBody struct {
	NotifyPolicy models.WorkflowNotifyPolicy `json:"notifyPolicy"`
}

// PatchWorkflowNotifyPolicy handles PATCH /api/workflows/:id/notify-policy.
// Notify-only write path: does not accept or rewrite Graph (review v1).
func (h *Handlers) PatchWorkflowNotifyPolicy(c *gin.Context) {
	var b workflowNotifyPolicyBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	wf, err := h.WF.UpdateNotifyPolicy(id, b.NotifyPolicy)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrWorkflowNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    wf.ProjectID,
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionWorkflowUpdate,
		ResourceType: "workflow",
		ResourceID:   wf.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update workflow notify policy",
		Payload: map[string]any{
			"notifyPolicy": wf.NotifyPolicy,
			"status":       wf.Status(),
			"version":      wf.Version,
		},
	})
	c.JSON(http.StatusOK, workflowDTO(wf))
}

type workflowHomeVisibilityBody struct {
	ShowOnHome *bool `json:"showOnHome"`
}

// PatchWorkflowHomeVisibility handles PATCH /api/workflows/:id/home-visibility.
// Visibility-only write path: does not accept or rewrite Graph (plan g1.2).
func (h *Handlers) PatchWorkflowHomeVisibility(c *gin.Context) {
	var b workflowHomeVisibilityBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if b.ShowOnHome == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "showOnHome is required"})
		return
	}
	id := c.Param("id")
	wf, err := h.WF.UpdateShowOnHome(id, *b.ShowOnHome)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrWorkflowNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    wf.ProjectID,
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionWorkflowUpdate,
		ResourceType: "workflow",
		ResourceID:   wf.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update workflow home visibility",
		Payload: map[string]any{
			"showOnHome": wf.ShowOnHome,
			"status":     wf.Status(),
			"version":    wf.Version,
		},
	})
	c.JSON(http.StatusOK, workflowDTO(wf))
}

func (h *Handlers) PublishWorkflow(c *gin.Context) {
	wf, err := h.WF.Publish(c.Param("id"))
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, services.ErrWorkflowNotFound) {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    wf.ProjectID,
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionWorkflowPublish,
		ResourceType: "workflow",
		ResourceID:   wf.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      fmt.Sprintf("publish workflow v%d", wf.Version),
		Payload:      map[string]any{"name": wf.Name, "version": wf.Version},
	})
	c.JSON(http.StatusOK, workflowDTO(wf))
}

func (h *Handlers) WorkflowVersions(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.WF.Get(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, h.WF.Versions(id))
}

func (h *Handlers) WorkflowVersionGraph(c *gin.Context) {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version"})
		return
	}
	graph, err := h.WF.VersionGraph(c.Param("id"), version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, graphDTO(graph))
}

func (h *Handlers) ImportWorkflow(c *gin.Context) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取请求体"})
		return
	}
	wf, err := h.WF.Import(raw, c.Query("projectId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, workflowDTO(wf))
}

func (h *Handlers) RestoreWorkflowVersion(c *gin.Context) {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version"})
		return
	}
	wf, err := h.WF.Restore(c.Param("id"), version)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrWorkflowNotFound), errors.Is(err, services.ErrWorkflowVersionNotFound):
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
		Action:       models.AuditActionWorkflowUpdate,
		ResourceType: "workflow",
		ResourceID:   wf.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      fmt.Sprintf("restore workflow v%d as v%d", version, wf.Version),
		Payload:      map[string]any{"restoredFrom": version, "version": wf.Version},
	})
	c.JSON(http.StatusOK, workflowDTO(wf))
}

func (h *Handlers) DeleteWorkflow(c *gin.Context) {
	id := c.Param("id")
	projectID := ""
	if wf, ok := h.WF.Get(id); ok {
		projectID = wf.ProjectID
	}
	actor := h.auditActorFromContext(c)
	if err := h.WF.Delete(id); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if projectID != "" {
		h.recordAudit(services.AuditRecord{
			ProjectID:    projectID,
			Actor:        actor,
			Action:       models.AuditActionWorkflowDelete,
			ResourceType: "workflow",
			ResourceID:   id,
			Outcome:      models.AuditOutcomeOK,
			Summary:      "delete workflow",
			Payload:      map[string]any{"deleted": true},
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *Handlers) CopyWorkflowPreview(c *gin.Context) {
	suggested, sourceName, sourceID, err := h.WF.CopyPreview(c.Param("id"))
	if errors.Is(err, services.ErrWorkflowNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"suggestedName": suggested,
		"sourceName":    sourceName,
		"sourceId":      sourceID,
	})
}

type copyWorkflowBody struct {
	Name string `json:"name"`
}

func (h *Handlers) CopyWorkflow(c *gin.Context) {
	var b copyWorkflowBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	wf, err := h.WF.Copy(c.Param("id"), b.Name)
	if errors.Is(err, services.ErrWorkflowNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if errors.Is(err, services.ErrEmptyWorkflowName) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, services.ErrWorkflowNameExists) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, workflowDTO(wf))
}
