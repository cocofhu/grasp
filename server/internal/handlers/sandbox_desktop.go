package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// ListMCPPresets handles GET /api/mcp-presets: the built-in MCP templates the
// MCP tab offers as quick-add entries.
func (h *Handlers) ListMCPPresets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"presets": services.MCPPresets()})
}

// desktopSandbox resolves the sandbox for the desktop routes. Solo sandboxes
// are visible to their owner only.
func (h *Handlers) desktopSandbox(c *gin.Context) (*models.Sandbox, bool) {
	if h.Sbx == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sandbox service unavailable"})
		return nil, false
	}
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return nil, false
	}
	row, err := h.Sbx.Get(id)
	if err != nil || row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sandbox not found"})
		return nil, false
	}
	if row.Purpose == services.SandboxPurposeSolo && row.ThreadID != "" && h.Solo != nil {
		if _, err := h.Solo.Require(row.ThreadID, sessionUsername(c)); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "sandbox not found"})
			return nil, false
		}
	}
	if row.Status != "running" {
		c.JSON(http.StatusConflict, gin.H{"error": "sandbox not running"})
		return nil, false
	}
	return row, true
}

// SandboxDesktopStatus handles GET /api/sandboxes/:id/desktop.
func (h *Handlers) SandboxDesktopStatus(c *gin.Context) {
	row, ok := h.desktopSandbox(c)
	if !ok {
		return
	}
	st, err := h.Sbx.DesktopStatus(c.Request.Context(), row.ID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "desktop status unavailable"})
		return
	}
	c.JSON(http.StatusOK, st)
}

// SandboxDesktopControl handles PUT /api/sandboxes/:id/desktop/control
// {holder: user|agent, client}. Only a signed-in session may change control;
// public share tickets never reach /api.
func (h *Handlers) SandboxDesktopControl(c *gin.Context) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return
		}
	}
	var body struct {
		Holder string `json:"holder"`
		Client string `json:"client"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}
	body.Holder = strings.TrimSpace(body.Holder)
	if body.Holder != "user" && body.Holder != "agent" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "holder must be user or agent"})
		return
	}
	row, ok := h.desktopSandbox(c)
	if !ok {
		return
	}
	client := sessionUsername(c) + ":" + strings.TrimSpace(body.Client)
	st, err := h.Sbx.SetDesktopControl(c.Request.Context(), row.ID, body.Holder, client)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "desktop control unavailable"})
		return
	}
	h.Sbx.TouchActivity(row.ID)
	c.JSON(http.StatusOK, st)
}
