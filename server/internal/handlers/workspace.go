package handlers

import (
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type workspaceUpdateBody struct {
	Name                    *string                       `json:"name"`
	Description             *string                       `json:"description"`
	Variables               *[]models.WorkspaceVariable   `json:"variables"`
	NotifyPolicy            *models.WorkspaceNotifyPolicy `json:"notifyPolicy"`
	UnknownModelDisplayName *string                       `json:"unknownModelDisplayName"`
}

func (h *Handlers) GetWorkspace(c *gin.Context) {
	if h.Workspace == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "workspace unavailable"})
		return
	}
	p, err := h.Workspace.Workspace()
	if err != nil {
		writeWorkspaceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, workspaceDTO(p, h.Workspace.WorkflowCount(), h.Workspace.TokenBreakdown()))
}

func (h *Handlers) ListRunTags(c *gin.Context) {
	if h.Runs == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": h.Runs.RunTags()})
}

func (h *Handlers) UpdateWorkspace(c *gin.Context) {
	if h.Workspace == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "workspace unavailable"})
		return
	}
	var b workspaceUpdateBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.Workspace.Update(b.Name, b.Description, b.Variables, b.NotifyPolicy, b.UnknownModelDisplayName)
	if err != nil {
		writeWorkspaceErr(c, err)
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
		payload["variables"] = services.MaskWorkspaceVarsForAudit(p.Variables)
	}
	h.recordAudit(services.AuditRecord{
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionWorkspaceConfig,
		ResourceType: "workspace",
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update workspace config",
		Payload:      payload,
	})
	c.JSON(http.StatusOK, workspaceDTO(p, h.Workspace.WorkflowCount(), h.Workspace.TokenBreakdown()))
}

// ListCredentials returns metadata and masked values only.
func (h *Handlers) ListCredentials(c *gin.Context) {
	if h.Credentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	rows, err := h.Credentials.List()
	if err != nil {
		writeCredentialErr(c, err)
		return
	}
	rows = append(rows, h.credentialAdapters()...)
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

// credentialAdapters exposes credentials that already have a dedicated
// encrypted/hash-backed service. The adapter only returns metadata and a
// masked prefix; it never copies plaintext into Credential storage.
func (h *Handlers) credentialAdapters() []services.CredentialView {
	out := make([]services.CredentialView, 0)
	if h.Channels != nil {
		if channels, err := h.Channels.List(); err == nil {
			for _, ch := range channels {
				masked := ""
				if ch.AppSecretSet {
					masked = "••••••••"
				}
				out = append(out, services.CredentialView{
					ID: "channel:" + ch.ID, Type: "channel",
					Provider: ch.Type, Name: ch.Name, Target: ch.ID, TargetType: "channel", TargetID: ch.ID,
					Masked: masked, Source: "channel", Configured: ch.AppSecretSet, Enabled: ch.Enabled,
					CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt,
				})
			}
		}
	}
	if h.McpKeys != nil {
		for _, key := range h.McpKeys.List() {
			out = append(out, services.CredentialView{
				ID: "external_mcp:" + key.ID, Type: "external_mcp",
				Name: key.Name, Target: key.ID, TargetType: "external_mcp", TargetID: key.ID,
				Masked: key.KeyPrefix, Source: "external_mcp", Configured: true, Enabled: key.RevokedAt == nil,
				RevokedAt: key.RevokedAt, CreatedAt: key.CreatedAt,
			})
		}
	}
	if h.APIKeys != nil && h.WF != nil {
		for _, wf := range h.WF.List() {
			for _, key := range h.APIKeys.List(wf.ID) {
				out = append(out, services.CredentialView{
					ID: "workflow:" + key.ID, Type: "workflow",
					Name: key.Name, Target: wf.ID, TargetType: "workflow", TargetID: wf.ID,
					Masked: key.KeyPrefix, Source: "workflow", Configured: true, Enabled: key.RevokedAt == nil,
					RevokedAt: key.RevokedAt, CreatedAt: key.CreatedAt,
				})
			}
		}
	}
	return out
}

func (h *Handlers) CreateCredential(c *gin.Context) {
	if h.Credentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	var b services.CredentialInput
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.Credentials.Create(b)
	if err != nil {
		writeCredentialErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{Actor: h.auditActorFromContext(c), Action: models.AuditActionWorkspaceConfig, ResourceType: "credential", ResourceID: v.ID, Outcome: models.AuditOutcomeOK, Summary: "create credential", Payload: map[string]any{"type": v.Type, "provider": v.Provider, "name": v.Name}})
	c.JSON(http.StatusOK, v)
}

func (h *Handlers) UpdateCredential(c *gin.Context) {
	if h.Credentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	var b services.CredentialInput
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.Credentials.Update(c.Param("credentialId"), b)
	if err != nil {
		writeCredentialErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{Actor: h.auditActorFromContext(c), Action: models.AuditActionWorkspaceConfig, ResourceType: "credential", ResourceID: v.ID, Outcome: models.AuditOutcomeOK, Summary: "update credential", Payload: map[string]any{"type": v.Type, "provider": v.Provider, "name": v.Name, "cleared": b.Clear}})
	c.JSON(http.StatusOK, v)
}

func (h *Handlers) RevokeCredential(c *gin.Context) {
	if h.Credentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	credentialID := c.Param("credentialId")
	if err := h.Credentials.Revoke(credentialID); err != nil {
		writeCredentialErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{Actor: h.auditActorFromContext(c), Action: models.AuditActionWorkspaceConfig, ResourceType: "credential", ResourceID: credentialID, Outcome: models.AuditOutcomeOK, Summary: "revoke credential"})
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func (h *Handlers) ClearCredential(c *gin.Context) {
	if h.Credentials == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credentials unavailable"})
		return
	}
	credentialID := c.Param("credentialId")
	if _, err := h.Credentials.Get(credentialID); err != nil {
		writeCredentialErr(c, err)
		return
	}
	if err := h.Credentials.Clear(credentialID); err != nil {
		writeCredentialErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{Actor: h.auditActorFromContext(c), Action: models.AuditActionWorkspaceConfig, ResourceType: "credential", ResourceID: credentialID, Outcome: models.AuditOutcomeOK, Summary: "clear credential"})
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
	case errors.Is(err, services.ErrCredentialAliasTaken):
		alias := ""
		var taken *services.CredentialAliasTakenError
		if errors.As(err, &taken) {
			alias = taken.Alias
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "alias_taken", "alias": alias})
	case errors.Is(err, services.ErrCredentialPurpose), errors.Is(err, services.ErrCredentialType), errors.Is(err, services.ErrCredentialName), errors.Is(err, services.ErrCredentialTarget), errors.Is(err, services.ErrCredentialEnvKey), errors.Is(err, runtime.ErrCodexLoginFileEmpty), errors.Is(err, runtime.ErrCodexLoginFileAPIKey):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrCredentialNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case isSecretsKeyErr(err):
		writeSecretsKeyErr(c, err)
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func writeWorkspaceErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrEmptyWorkspaceName),
		errors.Is(err, services.ErrSecretPlaceholderOnNewKey),
		errors.Is(err, services.ErrSecretPlaceholderOnPublicValue),
		errors.Is(err, services.ErrUnknownModelDisplayNameTooLong):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrWorkspaceMissing):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
