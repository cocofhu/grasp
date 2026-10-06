package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type projectCreateBody struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Variables   []models.ProjectVariable `json:"variables"`
}

type projectUpdateBody struct {
	Name                    *string                     `json:"name"`
	Description             *string                     `json:"description"`
	Variables               *[]models.ProjectVariable   `json:"variables"`
	NotifyPolicy            *models.ProjectNotifyPolicy `json:"notifyPolicy"`
	UnknownModelDisplayName *string                     `json:"unknownModelDisplayName"`
}

func (h *Handlers) ListProjects(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusOK, []gin.H{})
		return
	}
	ps := h.Projects.List()
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	tokens := h.Projects.TokenBreakdownByProjectIDs(ids)
	out := make([]gin.H, 0, len(ps))
	for _, p := range ps {
		out = append(out, projectDTO(p, h.Projects.WorkflowCount(p.ID), tokens[p.ID]))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetProject(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	p, ok := h.Projects.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, projectDTO(p, h.Projects.WorkflowCount(p.ID), h.Projects.TokenBreakdown(p.ID)))
}

func (h *Handlers) ListProjectRunTags(c *gin.Context) {
	if h.Projects == nil || h.Runs == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if _, ok := h.Projects.Get(c.Param("id")); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": h.Runs.ProjectRunTags(c.Param("id"))})
}

func (h *Handlers) CreateProject(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	var b projectCreateBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.Projects.Create(b.Name, b.Description, b.Variables)
	if err != nil {
		writeProjectErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    p.ID,
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionProjectConfig,
		ResourceType: "project",
		ResourceID:   p.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "create project",
		Payload: map[string]any{
			"name":        p.Name,
			"description": p.Description,
			"variables":   services.MaskProjectVarsForAudit(p.Variables),
		},
	})
	c.JSON(http.StatusOK, projectDTO(p, 0, services.ProjectTokenBreakdown{}))
}

func (h *Handlers) UpdateProject(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	var b projectUpdateBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	p, err := h.Projects.Update(id, b.Name, b.Description, b.Variables, b.NotifyPolicy, b.UnknownModelDisplayName)
	if err != nil {
		writeProjectErr(c, err)
		return
	}
	changed := []string{}
	if b.Name != nil {
		changed = append(changed, "name")
	}
	if b.Description != nil {
		changed = append(changed, "description")
	}
	if b.Variables != nil {
		changed = append(changed, "variables")
	}
	if b.NotifyPolicy != nil {
		changed = append(changed, "notifyPolicy")
	}
	if b.UnknownModelDisplayName != nil {
		changed = append(changed, "unknownModelDisplayName")
	}
	payload := map[string]any{"changed": changed, "name": p.Name}
	if b.Variables != nil {
		payload["variables"] = services.MaskProjectVarsForAudit(p.Variables)
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    p.ID,
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionProjectConfig,
		ResourceType: "project",
		ResourceID:   p.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update project config",
		Payload:      payload,
	})
	c.JSON(http.StatusOK, projectDTO(p, h.Projects.WorkflowCount(p.ID), h.Projects.TokenBreakdown(p.ID)))
}

func (h *Handlers) DeleteProject(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	id := c.Param("id")
	actor := h.auditActorFromContext(c)
	if err := h.Projects.Delete(id); err != nil {
		if errors.Is(err, services.ErrProjectHasWorkflows) {
			n := h.Projects.WorkflowCount(id)
			c.JSON(http.StatusConflict, gin.H{"error": services.FormatProjectHasWorkflowsError(n)})
			return
		}
		writeProjectErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    id,
		Actor:        actor,
		Action:       models.AuditActionProjectConfig,
		ResourceType: "project",
		ResourceID:   id,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "delete project",
		Payload:      map[string]any{"deleted": true},
	})
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ListProjectCredentials returns metadata and masked values only.
func (h *Handlers) ListProjectCredentials(c *gin.Context) {
	if h.ProjectCredentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	rows, err := h.ProjectCredentials.List(c.Param("id"))
	if err != nil {
		writeCredentialErr(c, err)
		return
	}
	rows = append(rows, h.projectCredentialAdapters(c.Param("id"))...)
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

// projectCredentialAdapters exposes credentials that already have a dedicated
// encrypted/hash-backed service. The adapter only returns metadata and a
// masked prefix; it never copies plaintext into ProjectCredential storage.
func (h *Handlers) projectCredentialAdapters(projectID string) []services.ProjectCredentialView {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	out := make([]services.ProjectCredentialView, 0)
	if h.Channels != nil {
		if channels, err := h.Channels.ListByProject(projectID); err == nil {
			for _, ch := range channels {
				masked := ""
				if ch.AppSecretSet {
					masked = "••••••••"
				}
				out = append(out, services.ProjectCredentialView{
					ID: "channel:" + ch.ID, ProjectID: projectID, Type: "channel",
					Provider: ch.Type, Name: ch.Name, Target: ch.ID, TargetType: "channel", TargetID: ch.ID,
					Masked: masked, Source: "channel", Configured: ch.AppSecretSet, Enabled: ch.Enabled,
					CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt,
				})
			}
		}
	}
	if h.ProjectMcpKeys != nil {
		for _, key := range h.ProjectMcpKeys.List(projectID) {
			out = append(out, services.ProjectCredentialView{
				ID: "external_mcp:" + key.ID, ProjectID: projectID, Type: "external_mcp",
				Name: key.Name, Target: key.ID, TargetType: "external_mcp", TargetID: key.ID,
				Masked: key.KeyPrefix, Source: "external_mcp", Configured: true, Enabled: key.RevokedAt == nil,
				RevokedAt: key.RevokedAt, CreatedAt: key.CreatedAt,
			})
		}
	}
	if h.APIKeys != nil && h.WF != nil {
		for _, wf := range h.WF.List(projectID) {
			for _, key := range h.APIKeys.List(wf.ID) {
				out = append(out, services.ProjectCredentialView{
					ID: "workflow:" + key.ID, ProjectID: projectID, Type: "workflow",
					Name: key.Name, Target: wf.ID, TargetType: "workflow", TargetID: wf.ID,
					Masked: key.KeyPrefix, Source: "workflow", Configured: true, Enabled: key.RevokedAt == nil,
					RevokedAt: key.RevokedAt, CreatedAt: key.CreatedAt,
				})
			}
		}
	}
	return out
}

type projectCredentialBody struct {
	services.ProjectCredentialInput
}

func (h *Handlers) CreateProjectCredential(c *gin.Context) {
	if h.ProjectCredentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	var b projectCredentialBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.ProjectCredentials.Create(c.Param("id"), b.ProjectCredentialInput)
	if err != nil {
		writeCredentialErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{ProjectID: c.Param("id"), Actor: h.auditActorFromContext(c), Action: models.AuditActionProjectConfig, ResourceType: "project_credential", ResourceID: v.ID, Outcome: models.AuditOutcomeOK, Summary: "create project credential", Payload: map[string]any{"type": v.Type, "provider": v.Provider, "name": v.Name}})
	c.JSON(http.StatusOK, v)
}

func (h *Handlers) UpdateProjectCredential(c *gin.Context) {
	if h.ProjectCredentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	var b projectCredentialBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.ProjectCredentials.Update(c.Param("id"), c.Param("credentialId"), b.ProjectCredentialInput)
	if err != nil {
		writeCredentialErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{ProjectID: c.Param("id"), Actor: h.auditActorFromContext(c), Action: models.AuditActionProjectConfig, ResourceType: "project_credential", ResourceID: v.ID, Outcome: models.AuditOutcomeOK, Summary: "update project credential", Payload: map[string]any{"type": v.Type, "provider": v.Provider, "name": v.Name, "cleared": b.Clear}})
	c.JSON(http.StatusOK, v)
}

func (h *Handlers) RevokeProjectCredential(c *gin.Context) {
	if h.ProjectCredentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	if err := h.ProjectCredentials.Revoke(c.Param("id"), c.Param("credentialId")); err != nil {
		writeCredentialErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{ProjectID: c.Param("id"), Actor: h.auditActorFromContext(c), Action: models.AuditActionProjectConfig, ResourceType: "project_credential", ResourceID: c.Param("credentialId"), Outcome: models.AuditOutcomeOK, Summary: "revoke project credential"})
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func (h *Handlers) ClearProjectCredential(c *gin.Context) {
	if h.ProjectCredentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	projectID := c.Param("id")
	credentialID := c.Param("credentialId")
	view, err := h.ProjectCredentials.Get(projectID, credentialID)
	if err != nil {
		writeCredentialErr(c, err)
		return
	}
	if err := h.ProjectCredentials.Clear(projectID, credentialID); err != nil {
		writeCredentialErr(c, err)
		return
	}
	if h.Agents != nil && strings.EqualFold(view.Type, "ai") && strings.EqualFold(view.Provider, "opencode") {
		h.Agents.ClearOpenCodeCredentialSelection(projectID, credentialID)
	}
	h.recordAudit(services.AuditRecord{ProjectID: projectID, Actor: h.auditActorFromContext(c), Action: models.AuditActionProjectConfig, ResourceType: "project_credential", ResourceID: credentialID, Outcome: models.AuditOutcomeOK, Summary: "clear project credential"})
	c.JSON(http.StatusOK, gin.H{"status": "cleared"})
}

// SecretsKeyMissingCode tells the UI to show a plain "server has no encryption
// key" message instead of the operator-facing error text.
const SecretsKeyMissingCode = "secrets_key_missing"

func isSecretsKeyErr(err error) bool {
	return errors.Is(err, crypto.ErrNoSecretsKey) || errors.Is(err, crypto.ErrInvalidSecretsKey)
}

func writeSecretsKeyErr(c *gin.Context, err error) {
	c.JSON(http.StatusPreconditionFailed, gin.H{"error": err.Error(), "code": SecretsKeyMissingCode})
}

func writeCredentialErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrCredentialProject), errors.Is(err, services.ErrCredentialType), errors.Is(err, services.ErrCredentialName), errors.Is(err, services.ErrCredentialTarget), errors.Is(err, services.ErrCredentialEnvKey), errors.Is(err, services.ErrCredentialModel), errors.Is(err, services.ErrCredentialBaseURL):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrProjectNotFound), errors.Is(err, services.ErrCredentialNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case isSecretsKeyErr(err):
		writeSecretsKeyErr(c, err)
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// GetProjectTokenStats returns the same aggregation as GET /api/stats/token,
// locked to this project. A query projectId cannot switch the scope.
// Window defaults to 30d when omitted (usage stats still default to all).
// Other filters match usage stats: from/to, granularity, source, status, phase,
// modelKey, workflowId, nodeType, timezone, utcOffsetMinutes.
func (h *Handlers) GetProjectTokenStats(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	id := c.Param("id")
	if _, ok := h.Projects.Get(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	q, ok := parseTokenStatsQuery(c, services.TokenStatsWindow30d)
	if !ok {
		return
	}
	// Lock the project. Ignore any client-supplied projectId.
	q.ProjectID = id
	if strings.TrimSpace(c.Query("window")) == "" && q.From == "" && q.To == "" {
		q.Window = services.TokenStatsWindow30d
	}

	result, err := h.Projects.GlobalTokenStats(c.Request.Context(), q)
	if err != nil {
		writeTokenStatsError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func writeProjectErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrEmptyProjectName),
		errors.Is(err, services.ErrSecretPlaceholderOnNewKey),
		errors.Is(err, services.ErrUnknownModelDisplayNameTooLong):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrProjectNameExists):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrProjectNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrProjectHasWorkflows):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
