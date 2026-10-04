package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/browser"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// SandboxVNC proxies noVNC (RFB over WebSocket) for a console sandbox by ID.
// Unlike PreviewVNC it does not require a registered preview port triple; it
// resolves the sandbox container name/IP, EnsureSandboxVNC on demand, and
// attaches to the sandbox desktop page as it is (about:blank only for the
// first viewer; address-bar navigation uses CDP goto).
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
	sandboxName := row.Name

	rctx, rcancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	mgr := h.Sbx.Manager()
	if mgr == nil {
		rcancel()
		c.String(http.StatusServiceUnavailable, "sandbox manager unavailable")
		return
	}
	if status := mgr.Status(rctx, sandboxName); status != "running" {
		rcancel()
		c.String(http.StatusGone, "sandbox recycled")
		return
	}
	sandboxIP, err := mgr.ContainerIP(rctx, sandboxName)
	rcancel()
	if err != nil || sandboxIP == "" {
		c.String(http.StatusBadGateway, "sandbox host unavailable")
		return
	}

	navigateURL := "about:blank"

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Uint("sandbox_id", id).Err(err).Msg("sandbox-vnc websocket upgrade failed")
		return
	}
	defer conn.Close()

	var wmu sync.Mutex
	writeJSON := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteJSON(v)
	}
	writeMsg := func(msgType int, data []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteMessage(msgType, data)
	}
	pushJSON := func(v any) {
		_ = writeJSON(v)
	}

	openCtx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	sess, err := h.Browser.OpenInSandbox(openCtx, sandboxName, sandboxIP, navigateURL)
	if err != nil {
		msg := err.Error()
		if msg == "" {
			msg = "未启动浏览器组件"
		}
		_ = writeJSON(gin.H{"type": "error", "message": msg})
		return
	}
	defer sess.Close()

	vncURL, err := sess.VNCWebSocketURL()
	if err != nil {
		_ = writeJSON(gin.H{"type": "error", "message": err.Error()})
		return
	}

	upstream, _, err := websocket.DefaultDialer.Dial(vncURL, nil)
	if err != nil {
		_ = writeJSON(gin.H{"type": "error", "message": "vnc upstream failed"})
		return
	}
	defer upstream.Close()

	done := make(chan struct{})
	defer close(done)

	sess.Page().OnPick(func(p browser.Pick) {
		pushJSON(gin.H{"type": "picked", "pick": p})
	})
	sess.Page().OnInspectCanceled(func() {
		pushJSON(gin.H{"type": "inspect-canceled"})
	})
	sess.Page().OnDescribeFailed(func() {
		pushJSON(gin.H{"type": "describe-failed"})
	})

	pushJSON(gin.H{"type": "ready", "url": vncReadyURL(c.Request.Context(), sess.Page(), navigateURL)})

	go func() {
		select {
		case <-sess.Done():
			pushJSON(gin.H{"type": "closed", "reason": sess.Reason()})
			time.Sleep(50 * time.Millisecond)
			_ = conn.Close()
		case <-done:
		}
	}()

	go func() {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				_ = upstream.Close()
				return
			}
			if msgType == websocket.TextMessage {
				var m vncClientMsg
				if json.Unmarshal(data, &m) == nil {
					sess.Touch()
					h.applyVncMsg(sess.Page(), m, pushJSON)
					continue
				}
			}
			if err := upstream.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	}()

	for {
		msgType, data, err := upstream.ReadMessage()
		if err != nil {
			return
		}
		if err := writeMsg(msgType, data); err != nil {
			return
		}
	}
}
