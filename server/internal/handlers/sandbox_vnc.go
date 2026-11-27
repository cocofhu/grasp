package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/gin-gonic/gin"
)

// SandboxVNC is the one VNC address of a sandbox: /sandbox-vnc/:sandboxId/ws.
// Console, run and review pages all watch the sandbox desktop through it; app
// ports are pages on that desktop, not addresses of their own.
func (h *Handlers) SandboxVNC(c *gin.Context) {
	row, ip := h.desktopViewerSandbox(c, true)
	if row == nil {
		return
	}
	h.Sbx.TouchActivity(row.ID)
	h.serveDesktopVNC(c, row.Name, ip, desktopVNCOptions{
		setClipboard: h.desktopClipboardWriter(row, sessionUsername(c)),
	})
}

func (h *Handlers) desktopClipboardWriter(row *models.Sandbox, username string) func(context.Context, string) error {
	if !h.canWriteSandboxClipboard(row, username) {
		return nil
	}
	return func(ctx context.Context, text string) error {
		if !h.canWriteSandboxClipboard(row, username) {
			return errors.New("sandbox clipboard access denied")
		}
		return h.Sbx.SetDesktopClipboard(ctx, row.ID, text)
	}
}

// SandboxAudio streams the sound of the same desktop:
// /sandbox-audio/:sandboxId/ws. A viewer opens it next to its VNC session, so
// it never wakes a stopped sandbox.
func (h *Handlers) SandboxAudio(c *gin.Context) {
	row, ip := h.desktopViewerSandbox(c, false)
	if row == nil {
		return
	}
	upstreamURL, err := h.Browser.AudioWebSocketURL(c.Request.Context(), row.Name, ip)
	if err != nil {
		c.String(http.StatusBadGateway, "desktop audio unavailable")
		return
	}
	serveDesktopAudio(c, upstreamURL)
}

// desktopViewerSandbox authorizes the viewer and resolves the sandbox desktop host,
// recovering a stopped sandbox when wake is set. On failure it has already
// written the response.
func (h *Handlers) desktopViewerSandbox(c *gin.Context, wake bool) (*models.Sandbox, string) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return nil, ""
		}
	}
	if h.Browser == nil {
		c.String(http.StatusServiceUnavailable, "vnc preview disabled")
		return nil, ""
	}
	if h.Sbx == nil {
		c.String(http.StatusServiceUnavailable, "sandbox service unavailable")
		return nil, ""
	}
	id, ok := parseUintParam(c, "sandboxId")
	if !ok {
		c.String(http.StatusBadRequest, "bad sandbox id")
		return nil, ""
	}

	row, err := h.Sbx.Get(id)
	if err != nil || row == nil || row.Name == "" {
		c.String(http.StatusNotFound, "sandbox not found")
		return nil, ""
	}
	if row.Status != "running" && wake {
		if _, _, err := h.Sbx.EnsureRunning(c.Request.Context(), id); err != nil {
			c.String(http.StatusConflict, "sandbox recovery unavailable: %v", err)
			return nil, ""
		}
	}
	sandboxIP, code, msg := h.resolveDesktopSandbox(c.Request.Context(), row.Name)
	if code != 0 {
		c.String(code, msg)
		return nil, ""
	}
	return row, sandboxIP
}
