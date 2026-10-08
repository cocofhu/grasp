package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/browser"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// vncClientMsg is the JSON control envelope over the VNC preview socket.
// Binary frames carry RFB (proxied to websockify); text JSON handles Pick/navigate.
// vncTouchEvery throttles viewer activity marks; well under browser.TabIdleTTL.
const vncTouchEvery = 15 * time.Second

// vncToucher marks a viewer active on any client traffic so a watched but
// untouched preview is not swept as idle. Used from one reader goroutine.
type vncToucher struct {
	touch func()
	now   func() time.Time
	last  time.Time
}

func newVncToucher(touch func()) *vncToucher {
	return &vncToucher{touch: touch, now: time.Now}
}

func (t *vncToucher) mark() {
	now := t.now()
	if !t.last.IsZero() && now.Sub(t.last) < vncTouchEvery {
		return
	}
	t.last = now
	t.touch()
}

type vncClientMsg struct {
	Type   string `json:"type"`   // "inspect" | "navigate" | "ping"
	On     bool   `json:"on"`     // inspect
	Action string `json:"action"` // navigate: "reload"|"back"|"forward"|"goto"
	URL    string `json:"url"`    // navigate goto target (about:blank / http…)
}

// desktopVNCOptions adapts the one sandbox desktop proxy to a caller.
type desktopVNCOptions struct {
	// onConnected runs right after the WebSocket upgrade, before the desktop
	// opens; the returned func runs when the connection ends.
	onConnected func(conn *websocket.Conn, writeJSON func(any) error) func()
	// allowMsg drops control messages the viewer may not send.
	allowMsg func(vncClientMsg) bool
}

// resolveDesktopSandbox returns the container IP of a running sandbox, or an
// HTTP status and message for a recycled or unreachable one.
func (h *Handlers) resolveDesktopSandbox(ctx context.Context, sandboxName string) (string, int, string) {
	if h.Sbx == nil {
		return "", http.StatusServiceUnavailable, "sandbox service unavailable"
	}
	mgr := h.Sbx.Manager()
	if mgr == nil {
		return "", http.StatusServiceUnavailable, "sandbox manager unavailable"
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if status := mgr.Status(rctx, sandboxName); status != "running" {
		return "", http.StatusGone, "sandbox recycled"
	}
	ip, err := mgr.ContainerIP(rctx, sandboxName)
	if err != nil || ip == "" {
		return "", http.StatusBadGateway, "sandbox host unavailable"
	}
	return ip, 0, ""
}

// serveDesktopVNC upgrades c and proxies noVNC (RFB over WebSocket) to the
// sandbox's in-container websockify, handling JSON control messages
// (Pick/navigate) over CDP on the same Chromium. Every viewer of a sandbox,
// signed-in or share-ticket, watches this one desktop: the Agent's screen.
// Attaching never navigates it.
func (h *Handlers) serveDesktopVNC(c *gin.Context, sandboxName, sandboxIP string, opts desktopVNCOptions) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Str("sandbox", sandboxName).Err(err).Msg("desktop vnc websocket upgrade failed")
		return
	}
	defer func() { _ = conn.Close() }()

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
	if opts.onConnected != nil {
		if done := opts.onConnected(conn, writeJSON); done != nil {
			defer done()
		}
	}

	const navigateURL = "about:blank"
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
	defer func() { _ = upstream.Close() }()

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

	// Client → upstream: binary RFB passthrough; text JSON → CDP control.
	go func() {
		activity := newVncToucher(sess.Touch)
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				_ = upstream.Close()
				return
			}
			activity.mark()
			if msgType == websocket.TextMessage {
				var m vncClientMsg
				if json.Unmarshal(data, &m) == nil {
					if opts.allowMsg == nil || opts.allowMsg(m) {
						h.applyVncMsg(sess.Page(), m, pushJSON)
					}
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
			log.Warn().Err(err).Bool("on", m.On).Msg("desktop vnc SetInspect failed")
			if pushJSON != nil {
				switch {
				case m.On && errors.Is(err, browser.ErrDesktopNotReady):
					pushJSON(gin.H{"type": "not-ready"})
				case !m.On:
					// The client already shows inspect off; the page may still be armed.
					pushJSON(gin.H{"type": "inspect-off-failed"})
				}
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

// vncReadyURL is the URL the attached desktop page is showing, which differs
// from navigateURL when a viewer re-attaches to a page left elsewhere.
func vncReadyURL(ctx context.Context, page browser.Page, navigateURL string) string {
	uctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if u, err := page.URL(uctx); err == nil && strings.TrimSpace(u) != "" {
		return u
	}
	return navigateURL
}
