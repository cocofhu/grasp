package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/version"
	"github.com/gin-gonic/gin"
)

// SandboxInject serves a short-lived ConfigHome .tgz for gateway
// config.bundleUrl / SANDBOX_INJECT. Auth is the one-shot Bearer from Create
// (not session cookie). Must stay outside /api auth middleware.
func (h *Handlers) SandboxInject(c *gin.Context) {
	if h.InjectBundles == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "inject store unavailable"})
		return
	}
	h.InjectBundles.ServeHTTP(c.Writer, c.Request, c.Param("id"))
}

// MCPRPC is the run-scoped artifact-store MCP endpoint. The in-container
// cursor-agent connects here (URL + Bearer token injected at ACP
// session/new) and calls write_artifact / read_artifact / list_artifacts.
// Streamable-HTTP: POST carries a JSON-RPC message; GET/DELETE are no-ops.
func (h *Handlers) MCPRPC(c *gin.Context) {
	runID := c.Param("runId")
	token := bearer(c.GetHeader("Authorization"))
	if h.MCP == nil || !h.MCP.AuthorizeRun(runID, token) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if c.Request.Method != http.MethodPost {

		c.Status(http.StatusOK)
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	status, resp := h.MCP.ServeRPC(runID, token, body)
	if resp == nil {
		c.Status(status)
		return
	}
	c.Data(status, "application/json", resp)
}

func bearer(h string) string {
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return strings.TrimSpace(h)
}

// Health is the readiness probe. During shutdown it returns 503 with grace info;
// an unreadable database also returns 503 so the pod is taken out of service.
func (h *Handlers) Health(c *gin.Context) {
	if h.Shutdown != nil && h.Shutdown.IsDraining() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":                  "shutting_down",
			"ready":                   false,
			"message":                 "服务正在关闭，不接受新请求",
			"grace_remaining_seconds": h.Shutdown.GraceRemainingSeconds(),
		})
		return
	}
	if h.Runs != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		err := h.Runs.Ping(ctx)
		cancel()
		if err != nil {
			_ = c.Error(err)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "db_error",
				"ready":   false,
				"message": "数据库不可用: " + err.Error(),
			})
			return
		}
	}
	body := gin.H{"status": "ok", "ready": true, "vnc_preview": h.Browser != nil}
	if sha := version.ShortSHA(); sha != "" {
		body["commit"] = sha
	}
	c.JSON(http.StatusOK, body)
}

// Live is the liveness probe: always 200 while the process is up.
func (h *Handlers) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}

// NodeRegistry returns the structured-product contract manifest (single source
// of truth for backend + frontend artifact mappings).
func (h *Handlers) NodeRegistry(c *gin.Context) {
	c.JSON(http.StatusOK, nodereg.BuildManifest())
}

// DashboardStats returns summary counters.
func (h *Handlers) DashboardStats(c *gin.Context) {
	c.JSON(http.StatusOK, h.Dash.Compute())
}

// PlatformStatus returns AppTopbar StatusMetrics (Auth-protected via /api group).
// Query: timezone=IANA (preferred), utcOffsetMinutes=int (fallback, east of UTC positive).
func (h *Handlers) PlatformStatus(c *gin.Context) {
	if h.Dash == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "dashboard unavailable"})
		return
	}
	q := services.PlatformStatusQuery{
		Timezone: c.Query("timezone"),
	}
	if raw := strings.TrimSpace(c.Query("utcOffsetMinutes")); raw != "" {
		mins, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid utcOffsetMinutes"})
			return
		}
		q.UTCOffsetMinutes = &mins
	}
	st, err := h.Dash.PlatformStatus(c.Request.Context(), q)
	if err != nil {
		if errors.Is(err, services.ErrInvalidTokenStatsTimezone) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}
