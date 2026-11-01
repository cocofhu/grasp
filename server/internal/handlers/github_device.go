package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type githubDeviceStartBody struct {
	Name         string `json:"name"`
	CredentialID string `json:"credentialId"`
}

// githubDeviceOwner binds a device session to the signed-in user. Without
// auth (local tests) every caller shares the empty owner.
func githubDeviceOwner(c *gin.Context) string {
	if sess, ok := auth.GetSession(c); ok && sess != nil {
		return strings.TrimSpace(sess.Username)
	}
	return ""
}

func (h *Handlers) githubDeviceEnabled() bool {
	return h.GitHubDevice != nil && h.GitHubDevice.Enabled()
}

// GitHubDeviceStatus reports whether "Sign in with GitHub" is available.
// GET /api/workspace/credentials/github/device
func (h *Handlers) GitHubDeviceStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.githubDeviceEnabled(), "host": services.GitHubDeviceHost, "scopes": strings.Fields(services.GitHubDeviceScopes)})
}

// StartGitHubDevice requests a device code. The device_code never leaves the server.
// POST /api/workspace/credentials/github/device
func (h *Handlers) StartGitHubDevice(c *gin.Context) {
	if !h.githubDeviceEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": services.ErrGitHubDeviceFlowDisabled.Error(), "code": "device_login_disabled"})
		return
	}
	var b githubDeviceStartBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := h.GitHubDevice.Start(c.Request.Context(), githubDeviceOwner(c), b.Name, b.CredentialID)
	if err != nil {
		writeGitHubDeviceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// PollGitHubDevice checks the authorization once and saves the credential on success.
// POST /api/workspace/credentials/github/device/:sessionId/poll
func (h *Handlers) PollGitHubDevice(c *gin.Context) {
	if !h.githubDeviceEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": services.ErrGitHubDeviceFlowDisabled.Error(), "code": "device_login_disabled"})
		return
	}
	out, err := h.GitHubDevice.Poll(c.Request.Context(), githubDeviceOwner(c), c.Param("sessionId"))
	if err != nil {
		writeGitHubDeviceErr(c, err)
		return
	}
	if out.Status == services.GitHubDeviceAuthorized && out.Credential != nil {
		v := out.Credential
		summary, verb := "create credential (github device flow)", "create"
		if out.Updated {
			summary, verb = "update credential (github device flow)", "update"
		}
		h.recordAudit(services.AuditRecord{
			Actor: h.auditActorFromContext(c), Action: models.AuditActionWorkspaceConfig,
			ResourceType: "credential", ResourceID: v.ID, Outcome: models.AuditOutcomeOK, Summary: summary,
			Payload: map[string]any{"type": v.Type, "provider": v.Provider, "name": v.Name, "host": v.Host, "githubLogin": out.GitHubLogin, "scopes": out.Scopes, "authMethod": "device_flow", "operation": verb},
		})
	}
	c.JSON(http.StatusOK, out)
}

// CancelGitHubDevice drops a session; it is idempotent.
// DELETE /api/workspace/credentials/github/device/:sessionId
func (h *Handlers) CancelGitHubDevice(c *gin.Context) {
	if !h.githubDeviceEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": services.ErrGitHubDeviceFlowDisabled.Error(), "code": "device_login_disabled"})
		return
	}
	h.GitHubDevice.Cancel(githubDeviceOwner(c), c.Param("sessionId"))
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

func writeGitHubDeviceErr(c *gin.Context, err error) {
	var apiErr *services.GitHubAPIError
	switch {
	case errors.Is(err, services.ErrGitHubDeviceFlowDisabled):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "device_login_disabled"})
	case errors.Is(err, services.ErrGitHubDeviceSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "session_not_found"})
	case errors.Is(err, services.ErrGitHubDeviceTarget):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "invalid_target"})
	case errors.As(err, &apiErr):
		c.JSON(http.StatusBadGateway, gin.H{"error": apiErr.Error(), "code": apiErr.Code})
	default:
		writeCredentialErr(c, err)
	}
}
