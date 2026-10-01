package handler

import (
	"net"
	"net/http"

	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// RuntimeBusy answers services.sh restart backend --if-idle. It skips the
// password guard, so it only serves loopback callers.
func RuntimeBusy(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			c.Status(http.StatusForbidden)
			return
		}
		busy, reason := chats.RuntimeBusy()
		c.JSON(http.StatusOK, gin.H{"busy": busy, "reason": reason})
	}
}
