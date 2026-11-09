package api

import (
	"net/http"
	"time"

	"sandbox-gateway/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// NewRouter keeps the v1 control plane compatible and offers an authenticated,
// sandbox-scoped TCP transport for clients outside the provider network.
func NewRouter(h *Handler, cfg *config.Config) *gin.Engine {
	h.profiles = cfg.Profiles
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(requestLogger(), recoverLogger())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "driver": cfg.Driver})
	})

	v1 := r.Group("/api/v1")
	v1.Use(APIKeyAuth(cfg.Auth.APIKeys))
	{
		v1.POST("/sandboxes", h.Create)
		v1.GET("/sandboxes", h.List)
		v1.GET("/sandboxes/:id", h.Get)
		v1.POST("/sandboxes/:id/start", h.Start)
		v1.POST("/sandboxes/:id/stop", h.Stop)
		v1.POST("/sandboxes/:id/reinstall", h.Reinstall)
		v1.DELETE("/sandboxes/:id", h.Destroy)
		v1.GET("/sandboxes/:id/status", h.Status)
		v1.GET("/sandboxes/:id/hosts/:port", h.Host)
		v1.POST("/sandboxes/:id/ports", h.PublishPort)
		v1.GET("/sandboxes/:id/logs", h.Logs)
		v1.GET("/sandboxes/:id/exit", h.LastExit)
	}
	release := v1.Group("/sandboxes/:id/releases")
	release.Use(requireTransportAuth(cfg.Auth.APIKeys))
	release.GET("/capabilities", h.ReleaseCapabilities)
	release.POST("", h.PreviewEnvironmentRelease)
	release.POST("/:releaseId/confirm", h.ConfirmEnvironmentRelease)
	release.PATCH("/:releaseId/fence", h.RenewEnvironmentRelease)
	release.GET("/:releaseId", h.GetEnvironmentRelease)
	v2 := r.Group("/api/v2")
	v2.Use(requireTransportAuth(cfg.Auth.APIKeys), APIKeyAuth(cfg.Auth.APIKeys))
	v2.GET("/info", gatewayInfo(cfg))
	v2.GET("/sandboxes/:id/connect/:port", h.Connect)
	return r
}

// requestLogger emits one JSON access log per request (logging-spec: cost_ms + status).
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		ev := log.Info()
		switch {
		case status >= 500:
			ev = log.Error()
		case status >= 400:
			ev = log.Warn()
		}
		ev.
			Str("method", c.Request.Method).
			Str("path", c.FullPath()).
			Str("raw_path", c.Request.URL.Path).
			Int("status", status).
			Int64("cost_ms", time.Since(start).Milliseconds()).
			Str("client_ip", c.ClientIP()).
			Int("body_bytes", c.Writer.Size()).
			Msg("http request")
	}
}

func recoverLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().
					Interface("panic", rec).
					Str("method", c.Request.Method).
					Str("raw_path", c.Request.URL.Path).
					Msg("http panic recovered")
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}
