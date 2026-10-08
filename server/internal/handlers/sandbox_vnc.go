package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SandboxVNC is the one VNC address of a sandbox: /sandbox-vnc/:sandboxId/ws.
// Console, run and review pages all watch the sandbox desktop through it; app
// ports are pages on that desktop, not addresses of their own.
func (h *Handlers) SandboxVNC(c *gin.Context) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return
		}
	}
	if h.Browser == nil {
		c.String(http.StatusServiceUnavailable, "vnc preview disabled")
		return
	}
	if h.Sbx == nil {
		c.String(http.StatusServiceUnavailable, "sandbox service unavailable")
		return
	}
	id, ok := parseUintParam(c, "sandboxId")
	if !ok {
		c.String(http.StatusBadRequest, "bad sandbox id")
		return
	}

	row, err := h.Sbx.Get(id)
	if err != nil || row == nil || row.Name == "" {
		c.String(http.StatusNotFound, "sandbox not found")
		return
	}
	sandboxIP, code, msg := h.resolveDesktopSandbox(c.Request.Context(), row.Name)
	if code != 0 {
		c.String(code, msg)
		return
	}
	h.serveDesktopVNC(c, row.Name, sandboxIP, desktopVNCOptions{})
}
