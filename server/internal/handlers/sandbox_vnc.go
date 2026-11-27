package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SandboxVNC is the one VNC address of a sandbox: /sandbox-vnc/:sandboxId/ws.
// Console, run and review pages all watch the sandbox desktop through it; app
// ports are pages on that desktop, not addresses of their own.
func (h *Handlers) SandboxVNC(c *gin.Context) {
	id, name, ip, ok := h.desktopViewerSandbox(c, true)
	if !ok {
		return
	}
	h.Sbx.TouchActivity(id)
	h.serveDesktopVNC(c, name, ip, desktopVNCOptions{
		setClipboard: func(ctx context.Context, text string) error {
			return h.Sbx.SetDesktopClipboard(ctx, id, text)
		},
	})
}

// SandboxAudio streams the sound of the same desktop:
// /sandbox-audio/:sandboxId/ws. A viewer opens it next to its VNC session, so
// it never wakes a stopped sandbox.
func (h *Handlers) SandboxAudio(c *gin.Context) {
	_, name, ip, ok := h.desktopViewerSandbox(c, false)
	if !ok {
		return
	}
	upstreamURL, err := h.Browser.AudioWebSocketURL(c.Request.Context(), name, ip)
	if err != nil {
		c.String(http.StatusBadGateway, "desktop audio unavailable")
		return
	}
	serveDesktopAudio(c, upstreamURL)
}

// desktopViewerSandbox authorizes the viewer and resolves the sandbox desktop host,
// recovering a stopped sandbox when wake is set. On failure it has already
// written the response.
func (h *Handlers) desktopViewerSandbox(c *gin.Context, wake bool) (uint, string, string, bool) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return 0, "", "", false
		}
	}
	if h.Browser == nil {
		c.String(http.StatusServiceUnavailable, "vnc preview disabled")
		return 0, "", "", false
	}
	if h.Sbx == nil {
		c.String(http.StatusServiceUnavailable, "sandbox service unavailable")
		return 0, "", "", false
	}
	id, ok := parseUintParam(c, "sandboxId")
	if !ok {
		c.String(http.StatusBadRequest, "bad sandbox id")
		return 0, "", "", false
	}

	row, err := h.Sbx.Get(id)
	if err != nil || row == nil || row.Name == "" {
		c.String(http.StatusNotFound, "sandbox not found")
		return 0, "", "", false
	}
	if row.Status != "running" && wake {
		if _, _, err := h.Sbx.EnsureRunning(c.Request.Context(), id); err != nil {
			c.String(http.StatusConflict, "sandbox recovery unavailable: %v", err)
			return 0, "", "", false
		}
	}
	sandboxIP, code, msg := h.resolveDesktopSandbox(c.Request.Context(), row.Name)
	if code != 0 {
		c.String(code, msg)
		return 0, "", "", false
	}
	return id, row.Name, sandboxIP, true
}
