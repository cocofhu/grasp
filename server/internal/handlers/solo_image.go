package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handlers) SoloImage(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	data, media, err := h.Solo.Image(ctx, c.Param("id"), user, c.Query("path"))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.Data(http.StatusOK, media, data)
}
