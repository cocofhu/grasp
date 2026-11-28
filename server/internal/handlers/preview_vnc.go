package handlers

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// vncClientMsg is the JSON control envelope over the VNC preview socket.
// Binary frames carry RFB (proxied to websockify); text JSON handles Pick/navigate.
// vncTouchEvery throttles viewer activity marks; well under browser.TabIdleTTL.
const vncTouchEvery = 15 * time.Second
const vncClipboardMaxBytes = 128 << 10

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
	Type   string `json:"type"`   // "inspect" | "navigate" | "ping" | clipboard controls
	On     bool   `json:"on"`     // inspect
	Action string `json:"action"` // navigate: "reload"|"back"|"forward"|"goto"|"open" (new tab)
	URL    string `json:"url"`    // navigate goto/open target (about:blank / http…)
	ID     int    `json:"id"`     // clipboard write request or copy observation ID
	Text   string `json:"text"`   // clipboard text
	Paste  bool   `json:"paste"`  // apply Ctrl+V after replacing the clipboard
	Shift  bool   `json:"shift"`  // apply Ctrl+Shift+V
}

type vncClipboardStateReader func(context.Context, sandbox.DesktopClipboardState) (sandbox.DesktopClipboardState, error)

// desktopVNCOptions adapts the one sandbox desktop proxy to a caller.
type desktopVNCOptions struct {
	// onConnected runs right after the WebSocket upgrade, before the desktop
	// opens; the returned func runs when the connection ends.
	onConnected func(conn *websocket.Conn, writeJSON func(any) error) func()
	// allowMsg drops control messages the viewer may not send.
	allowMsg func(vncClientMsg) bool
	// setClipboard is available to signed-in viewers with clipboard access.
	setClipboard   func(context.Context, string) error
	clipboardState vncClipboardStateReader
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
	if opts.setClipboard != nil {
		conn.SetReadLimit(6*vncClipboardMaxBytes + 1024)
	}
	ctx := c.Request.Context()
	clipboard := newVncClipboardObservation(ctx, opts.clipboardState)

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
	openCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
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

	pushJSON(gin.H{
		"type": "ready", "url": vncReadyURL(ctx, sess.Page(), navigateURL),
		"clipboard": opts.setClipboard != nil, "clipboardState": opts.clipboardState != nil,
	})

	go func() {
		select {
		case <-sess.Done():
			pushJSON(gin.H{"type": "closed", "reason": sess.Reason()})
			time.Sleep(50 * time.Millisecond)
			_ = conn.Close()
		case <-done:
		}
	}()

	// Client → upstream: keep clipboard writes and keyboard events in order.
	go func() {
		defer func() { _ = upstream.Close() }()
		activity := newVncToucher(sess.Touch)
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			activity.mark()
			if msgType == websocket.TextMessage {
				var m vncClientMsg
				if json.Unmarshal(data, &m) == nil {
					if opts.allowMsg != nil && !opts.allowMsg(m) {
						continue
					}
					switch m.Type {
					case "clipboard":
						if opts.setClipboard != nil {
							if !writeVncClipboard(ctx, upstream, opts.setClipboard, m, pushJSON) {
								return
							}
							clipboard.refresh()
						}
					case "clipboard-watch":
						clipboard.watch(m.ID)
					case "clipboard-reset":
						clipboard.reset(m.ID)
					case "clipboard-check":
						source := clipboard.pasteSource(m.ID)
						pushJSON(gin.H{
							"type": "clipboard-check-result", "id": m.ID, "source": source,
							"changed": source != vncClipboardPasteLocal, // Older clients use this to choose remote paste.
						})
					default:
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

// A watch freezes the latest completed snapshot before the reader forwards any
// input that could copy. The reader never waits for a pending snapshot on input.
type vncClipboardObservation struct {
	ctx    context.Context
	read   vncClipboardStateReader
	latest sandbox.DesktopClipboardState
	sample *vncClipboardSnapshot
	id     int
	before sandbox.DesktopClipboardState
}

type vncClipboardSnapshot struct {
	ready  chan struct{}
	cancel context.CancelFunc
	state  sandbox.DesktopClipboardState
}

type vncClipboardPasteSource string

const (
	vncClipboardPasteLocal      vncClipboardPasteSource = "local"
	vncClipboardPasteRemote     vncClipboardPasteSource = "remote"
	vncClipboardPasteRemoteOnce vncClipboardPasteSource = "remote-once"
)

const vncClipboardStateTimeout = 900 * time.Millisecond

func newVncClipboardObservation(ctx context.Context, read vncClipboardStateReader) *vncClipboardObservation {
	o := &vncClipboardObservation{ctx: ctx, read: read}
	o.refresh()
	return o
}

func (o *vncClipboardObservation) refresh() {
	if o.read == nil {
		return
	}
	o.acceptReadySnapshot()
	if o.sample != nil {
		o.sample.cancel()
	}
	o.id, o.before = 0, sandbox.DesktopClipboardState{}
	ctx, cancel := context.WithTimeout(o.ctx, vncClipboardStateTimeout)
	sample := &vncClipboardSnapshot{ready: make(chan struct{}), cancel: cancel}
	o.sample = sample
	go func() {
		defer close(sample.ready)
		defer cancel()
		if state, err := o.read(ctx, sandbox.DesktopClipboardState{}); err == nil {
			sample.state = state
		}
	}()
}

func (o *vncClipboardObservation) acceptReadySnapshot() {
	if o.sample == nil {
		return
	}
	select {
	case <-o.sample.ready:
		if o.sample.state.Generation != "" {
			o.latest = o.sample.state
		}
		o.sample = nil
	default:
	}
}

func (o *vncClipboardObservation) watch(id int) {
	if o.read == nil || id < 1 || o.id == id {
		return
	}
	o.acceptReadySnapshot()
	o.id, o.before = id, o.latest
}

func (o *vncClipboardObservation) reset(id int) {
	// A zero ID prepares a fresh baseline on takeover. It must leave a
	// pending copy observation alone.
	if o.id == id {
		o.refresh()
	}
}

func (o *vncClipboardObservation) pasteSource(id int) vncClipboardPasteSource {
	if id < 1 || o.id != id {
		return vncClipboardPasteLocal
	}
	before := o.before
	o.id, o.before = 0, sandbox.DesktopClipboardState{}
	if o.sample != nil {
		o.sample.cancel()
		o.sample = nil
	}
	ctx, cancel := context.WithTimeout(o.ctx, vncClipboardStateTimeout)
	defer cancel()
	after, err := o.read(ctx, before)
	if err == nil && after.Generation != "" && after.Generation != before.Generation && !after.Owned {
		// The copy may still be reaching the application. With no baseline in
		// this generation, give a new owner the same short wait as a normal check.
		after, err = o.read(ctx, after)
	}
	if err != nil || after.Generation == "" {
		// A later watch needs a baseline newer than the unverified input.
		o.latest = sandbox.DesktopClipboardState{}
		o.refresh()
		return vncClipboardPasteRemoteOnce
	}
	o.latest = after
	switch {
	case !after.Owned:
		return vncClipboardPasteLocal
	case after.Generation != before.Generation:
		// Preserve an existing remote clipboard when there is no comparable baseline.
		return vncClipboardPasteRemoteOnce
	case after.Revision > before.Revision:
		return vncClipboardPasteRemote
	default:
		return vncClipboardPasteLocal
	}
}

func writeVncClipboard(ctx context.Context, upstream *websocket.Conn, setText func(context.Context, string) error, m vncClientMsg, reply func(any)) bool {
	result := gin.H{"type": "clipboard-result", "id": m.ID, "ok": false}
	defer func() { reply(result) }()
	if m.Text == "" || len(m.Text) > vncClipboardMaxBytes {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := setText(ctx, m.Text); err != nil {
		var status *sandbox.ChatHTTPError
		if errors.As(err, &status) && status.StatusCode == http.StatusNotFound {
			result["code"] = "runtime-outdated"
		}
		return true
	}
	if m.Paste {
		if err := upstream.WriteMessage(websocket.BinaryMessage, rfbPasteKeys(m.Shift)); err != nil {
			return false
		}
	}
	result["ok"] = true
	return true
}

// rfbPasteKeys encodes the Linux paste shortcut using standard RFB KeyEvents.
func rfbPasteKeys(shift bool) []byte {
	const control, shiftKey = 0xffe3, 0xffe1
	v := uint32('v')
	var keys []byte
	appendKey := func(key uint32, down byte) {
		keys = binary.BigEndian.AppendUint32(append(keys, 4, down, 0, 0), key)
	}
	appendKey(control, 1)
	if shift {
		appendKey(shiftKey, 1)
		v = 'V'
	}
	appendKey(v, 1)
	appendKey(v, 0)
	if shift {
		appendKey(shiftKey, 0)
	}
	appendKey(control, 0)
	return keys
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
		if m.Action == "open" {
			if u, err := url.Parse(strings.TrimSpace(m.URL)); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
				if err := page.OpenTab(u.String()); err != nil {
					log.Warn().Err(err).Str("url", m.URL).Msg("desktop vnc OpenTab failed")
				}
			}
			return
		}
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
