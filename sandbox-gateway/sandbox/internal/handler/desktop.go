package handler

import (
	"errors"
	"net/http"
	"os/exec"

	"backend/internal/desktop"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// DesktopStatus answers GET /api/desktop/status: mode, shell readiness,
// whether an agent turn is running and who holds the keyboard and mouse.
func DesktopStatus(chats *service.ChatManager, paths desktop.Paths, store *desktop.ControlStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		busy := false
		if chats != nil {
			busy, _ = chats.RuntimeActivity()
		}
		c.JSON(http.StatusOK, desktop.BuildStatus(paths, store.Read(), busy, exec.LookPath))
	}
}

// DesktopControl answers PUT /api/desktop/control {holder, client}. A user
// hold lasts desktop.ControlTTL and must be renewed.
func DesktopControl(chats *service.ChatManager, paths desktop.Paths, store *desktop.ControlStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Holder string `json:"holder"`
			Client string `json:"client"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
			return
		}
		ctl, err := store.Set(body.Holder, body.Client)
		if errors.Is(err, desktop.ErrBadHolder) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		busy := false
		if chats != nil {
			busy, _ = chats.RuntimeActivity()
		}
		c.JSON(http.StatusOK, desktop.BuildStatus(paths, ctl, busy, exec.LookPath))
	}
}
