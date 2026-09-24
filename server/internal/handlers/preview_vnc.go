package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/browser"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// vncClientMsg is the JSON control envelope over the VNC preview socket.
// Binary frames carry RFB (proxied to websockify); text JSON handles Pick/navigate.
type vncClientMsg struct {
	Type   string `json:"type"`   // "inspect" | "navigate"
	On     bool   `json:"on"`     // inspect
	Action string `json:"action"` // navigate: "reload"|"back"|"forward"|"goto"
	URL    string `json:"url"`    // navigate goto target (about:blank / http…)
}

// PreviewVNC proxies noVNC (RFB over WebSocket) to the app_preview sandbox's
// in-container websockify while handling JSON control messages (Pick/navigate)
// over CDP on the same Chromium instance inside that sandbox.
func (h *Handlers) PreviewVNC(c *gin.Context) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return
		}
	}
	if h.Browser == nil {
		c.String(http.StatusServiceUnavailable, "vnc preview disabled")
		return
	}
	runID := c.Param("runId")
	nodeID := c.Param("nodeId")
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil || port <= 0 {
		c.String(http.StatusBadRequest, "bad port")
		return
	}

	rctx, rcancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	bridgeURL, sandboxName, ok := h.resolvePreviewTarget(rctx, runID, nodeID, port)
	rcancel()
	if !ok {
		if sandboxName != "" {
			c.String(http.StatusGone, "sandbox recycled")
			return
		}
		c.String(http.StatusNotFound, "preview not registered")
		return
	}

	sandboxIP, err := previewHostIP(bridgeURL)
	if err != nil {
		c.String(http.StatusBadGateway, "preview host invalid")
		return
	}
	// Stay on the sandbox loopback so iptables sends this port through preview-inject.
	// The browser origin remains http://127.0.0.1:<port>/, so app paths are unchanged.
	navigateURL := fmt.Sprintf("http://127.0.0.1:%d/", port)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Str("run_id", runID).Str("node_id", nodeID).Err(err).Msg("preview-vnc websocket upgrade failed")
		return
	}
	defer conn.Close()

	// gorilla/websocket does not support concurrent writes; the RFB passthrough
	// loop, pushJSON, and OnPick may all write to conn.
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
		_ = writeJSON(gin.H{"type": "error", "message": err.Error()})
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
		// Remote Esc left CDP inspect; frontend must clear button pressed state.
		pushJSON(gin.H{"type": "inspect-canceled"})
	})
	sess.Page().OnDescribeFailed(func() {
		// Node describe failed after Overlay pick — distinct from Esc cancel.
		pushJSON(gin.H{"type": "describe-failed"})
	})

	pushJSON(gin.H{"type": "ready", "url": navigateURL})

	go func() {
		select {
		case <-sess.Done():
			pushJSON(gin.H{"type": "closed", "reason": sess.Reason()})
			time.Sleep(50 * time.Millisecond)
			_ = conn.Close()
		case <-done:
		}
	}()

	// Client → upstream: binary RFB passthrough; text JSON → CDP control.
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

	// Upstream → client: RFB binary passthrough.
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

func (h *Handlers) applyVncMsg(page browser.Page, m vncClientMsg, pushJSON func(any)) {
	switch m.Type {
	case "inspect":
		if err := page.SetInspect(m.On); err != nil {
			log.Warn().Err(err).Bool("on", m.On).Msg("preview-vnc SetInspect failed")
			if m.On && pushJSON != nil && errors.Is(err, browser.ErrDesktopNotReady) {
				pushJSON(gin.H{"type": "not-ready"})
			}
		}
	case "navigate":
		if m.Action == "goto" || m.URL != "" {
			url := m.URL
			if url == "" {
				url = "about:blank"
			}
			_ = page.Goto(url)
			return
		}
		_ = page.Navigate(m.Action)
	}
}

// previewHostIP extracts the host/IP from a bridge URL like http://172.17.0.2:3000/.
func previewHostIP(bridgeURL string) (string, error) {
	u, err := url.Parse(bridgeURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("empty host in %q", bridgeURL)
	}
	return host, nil
}
