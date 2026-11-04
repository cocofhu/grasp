package handlers

import (
	"errors"
	"net"
	"net/http"

	"github.com/cocofhu/grasp/internal/codexoauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

var errCodexDeviceUnavailable = errors.New("Codex sign-in is not available")

func (h *Handlers) codexDeviceReady(c *gin.Context) bool {
	if h.CodexDevice == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errCodexDeviceUnavailable.Error(), "code": "device_login_disabled"})
		return false
	}
	return true
}

// CodexDeviceStatus reports whether "Sign in to ChatGPT with a device code" is available.
// GET /api/workspace/credentials/codex/device
func (h *Handlers) CodexDeviceStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.CodexDevice != nil})
}

// StartCodexDevice requests a user code. The device auth ID never leaves the server.
// POST /api/workspace/credentials/codex/device
func (h *Handlers) StartCodexDevice(c *gin.Context) {
	if !h.codexDeviceReady(c) {
		return
	}
	var b githubDeviceStartBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := h.CodexDevice.Start(c.Request.Context(), githubDeviceOwner(c), b.Name, b.CredentialID)
	if err != nil {
		writeCodexDeviceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// PollCodexDevice checks the sign-in once and saves the login on success.
// POST /api/workspace/credentials/codex/device/:sessionId/poll
func (h *Handlers) PollCodexDevice(c *gin.Context) {
	if !h.codexDeviceReady(c) {
		return
	}
	out, err := h.CodexDevice.Poll(c.Request.Context(), githubDeviceOwner(c), c.Param("sessionId"))
	if err != nil {
		writeCodexDeviceErr(c, err)
		return
	}
	if out.Status == services.CodexDeviceSuccess && out.Credential != nil {
		v := out.Credential
		summary, verb := "create credential (codex device code)", "create"
		if out.Updated {
			summary, verb = "update credential (codex device code)", "update"
		}
		h.recordAudit(services.AuditRecord{
			Actor: h.auditActorFromContext(c), Action: models.AuditActionWorkspaceConfig,
			ResourceType: "credential", ResourceID: v.ID, Outcome: models.AuditOutcomeOK, Summary: summary,
			Payload: map[string]any{"type": v.Type, "provider": v.Provider, "name": v.Name, "toolId": v.ToolID, "authMethod": "device_code", "operation": verb},
		})
	}
	c.JSON(http.StatusOK, out)
}

// CancelCodexDevice drops a session; it is idempotent.
// DELETE /api/workspace/credentials/codex/device/:sessionId
func (h *Handlers) CancelCodexDevice(c *gin.Context) {
	if !h.codexDeviceReady(c) {
		return
	}
	h.CodexDevice.Cancel(githubDeviceOwner(c), c.Param("sessionId"))
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

func writeCodexDeviceErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrCodexDeviceNotEnabled):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "not_enabled"})
	case errors.Is(err, services.ErrCodexDeviceSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "session_not_found"})
	case errors.Is(err, services.ErrCodexDeviceTarget):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "invalid_target"})
	case isCodexAuthErr(err):
		c.JSON(http.StatusBadGateway, gin.H{"error": "OpenAI sign-in service failed: " + err.Error(), "code": "upstream_error"})
	default:
		writeCredentialErr(c, err)
	}
}

func isCodexAuthErr(err error) bool {
	var oe *codexoauth.Error
	return errors.As(err, &oe) || isNetErr(err)
}

func isNetErr(err error) bool {
	var ne net.Error
	return errors.As(err, &ne)
}
