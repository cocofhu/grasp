package handler

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"time"

	"backend/internal/desktop"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
	"golang.design/x/clipboard"
)

// Leave room for the ChangeProperty header in a standard X11 request.
const desktopClipboardMaxBytes = 128 << 10

// DesktopClipboard puts text on the sandbox's X11 CLIPBOARD selection.
func DesktopClipboard(writeText func(context.Context, string) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		// JSON escape sequences can take up to six bytes per byte of text.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 6*desktopClipboardMaxBytes+1024)
		var body struct {
			Text string `json:"text"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.Text == "" || len(body.Text) > desktopClipboardMaxBytes {
			c.Status(http.StatusBadRequest)
			return
		}
		if err := writeText(c.Request.Context(), body.Text); err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func WriteDesktopClipboard(ctx context.Context, text string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// x11vnc otherwise broadcasts changes to every connected viewer. Apply
	// this to the running process as well, so updating the bundle is enough.
	if err := exec.CommandContext(ctx, "x11vnc", "-sync", "-R", "seldir:recv").Run(); err != nil {
		return err
	}
	if err := clipboard.Init(); err != nil {
		return err
	}
	_, err := clipboard.Write(ctx, clipboard.FmtText, []byte(text))
	return err
}

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
