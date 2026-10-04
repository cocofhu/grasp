package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
)

// Fixed remote desktop. The Xvfb framebuffer is 1920x1080 and the outer
// Chromium window covers it at 0,0, so the tab strip and address bar stay
// visible at the top of the preview. The CSS viewport is the content area
// below that toolbar, not the outer frame. Pinned via
// Emulation.setDeviceMetricsOverride at DSF 1, so captured pixels equal CSS
// pixels and client click coordinates map 1:1.
const (
	ViewportWidth  = 1920
	ViewportHeight = 1080

	// desktopFrameSlackPx is rounding tolerance for the outer frame origin and
	// size; Chromium reports a couple of pixels of border on Xvfb.
	desktopFrameSlackPx = 2
	// maxContentInsetXPx / maxContentInsetYPx bound the frame borders and the
	// tab strip plus address bar. A larger inset means the content size was
	// read before the window finished resizing, or the content area is short.
	maxContentInsetXPx   = 8
	maxContentInsetYPx   = 200
	desktopBoundsRetries = 3
	desktopBoundsRetry   = 80 * time.Millisecond
	// desktopWatchInterval re-checks the headed window after the viewer toggles
	// fullscreen, which changes the content area under the pinned viewport.
	desktopWatchInterval = 300 * time.Millisecond
	// desktopSettleStableReads is how many identical reads in a row count as
	// settled. The outer frame updates at once; innerHeight follows only after
	// the X11 resize and relayout.
	desktopSettleStableReads = 3
)

var (
	desktopSettlePoll    = 40 * time.Millisecond
	desktopSettleTimeout = time.Second
)

// ErrDesktopNotReady means the headed window does not cover the Xvfb desktop
// with a content area directly below its toolbar. Overlay inspect would
// mis-hit. Callers must refuse entering inspect and surface a not-ready
// control message. The error text includes content and outer sizes so a short
// content area is diagnosable even when the outer frame reads 1920x1080.
var ErrDesktopNotReady = errors.New("desktop window not ready for inspect")

// dialRod connects to a Chromium container's CDP endpoint (http://ip:9222) and
// returns an Engine. The browser is NOT bound to the caller's request context so
// it outlives individual requests; its lifetime is the container's.
func dialRod(_ context.Context, httpBase string) (Engine, error) {
	wsURL, err := launcher.ResolveURL(httpBase)
	if err != nil {
		return nil, fmt.Errorf("resolve cdp url: %w", err)
	}
	// Own the WebSocket so Close can drop the connection: rod's Browser.Close
	// sends Browser.close, which would kill the sandbox Chromium and its state.
	ws := &cdp.WebSocket{}
	if err := ws.Connect(context.Background(), wsURL, nil); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	// NoDefaultDevice: rod's laptop preset (1280x800) would override the layout
	// viewport before we can measure the real content area.
	b := rod.New().Client(cdp.New().Start(ws)).NoDefaultDevice()
	if err := b.Connect(); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &rodEngine{browser: b, ws: ws}, nil
}

type rodEngine struct {
	browser *rod.Browser
	ws      *cdp.WebSocket
}

func (e *rodEngine) NewTab(_ context.Context, url string) (Page, error) {
	// Default browser context: cookies and storage live in the Chromium profile
	// and survive viewer reconnects and Chromium restarts.
	page, err := e.browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}
	rp := &rodPage{engine: e, page: page}
	// Headed Chromium on Xvfb (no window manager) opens NewTab as another
	// window. presentDesktop covers the framebuffer with that window and pins
	// the CSS viewport to its content area (DSF 1). Tab open stays best-effort;
	// SetInspect(on) hard-gates on readiness.
	if err := rp.presentDesktop(); err != nil {
		log.Warn().Err(err).Msg("preview NewTab content area does not cover desktop; inspect stays disabled")
	}
	rp.installPickListener()
	return rp, nil
}

// desktopGeom is one read-back of the headed window: outer frame versus the
// visible content area (window.innerWidth/innerHeight), plus the outer origin.
type desktopGeom struct {
	left, top          int
	outerW, outerH     int
	contentW, contentH int
}

// windowStateOnlyBounds changes state without a size. Chromium ignores
// left/top/width/height when they share a request with a non-normal state, and
// on a window manager-less Xvfb it also drops the size when state is combined
// with the bounds. State and size must be two requests.
func windowStateOnlyBounds() *proto.BrowserBounds {
	return &proto.BrowserBounds{WindowState: proto.BrowserWindowStateNormal}
}

// windowSizeOnlyBounds sets the outer rectangle and omits window state.
func windowSizeOnlyBounds(left, top, width, height int) *proto.BrowserBounds {
	return &proto.BrowserBounds{
		Left: &left, Top: &top, Width: &width, Height: &height,
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// frameMatches reports whether the outer frame is the requested rectangle,
// within the border slack Chromium reports on Xvfb.
func frameMatches(g desktopGeom, left, top, width, height int) bool {
	return absInt(g.left-left) <= desktopFrameSlackPx &&
		absInt(g.top-top) <= desktopFrameSlackPx &&
		absInt(g.outerW-width) <= desktopFrameSlackPx &&
		absInt(g.outerH-height) <= desktopFrameSlackPx
}

// desktopReady reports whether the outer frame covers the Xvfb screen at the
// origin and the content area fills it below the toolbar. The toolbar stays
// on screen, so the content area is shorter than the desktop by the toolbar
// height (zero in fullscreen). An inset outside the toolbar bounds means the
// content size is stale or short, and the window is not ready.
func desktopReady(g desktopGeom) bool {
	if g.contentW <= 0 || g.contentH <= 0 {
		return false
	}
	if !frameMatches(g, 0, 0, ViewportWidth, ViewportHeight) {
		return false
	}
	insetX := g.outerW - g.contentW
	insetY := g.outerH - g.contentH
	return insetX >= 0 && insetX <= maxContentInsetXPx &&
		insetY >= 0 && insetY <= maxContentInsetYPx
}

// viewportForContent is the CSS viewport at DSF 1. It follows the content
// area. Callers must not pass the outer frame: a 1920x1080 outer size with a
// shorter content area would layout past what the window can paint.
// Content larger than the desktop is clamped to the framebuffer.
func viewportForContent(contentW, contentH int) (width, height int, dpr float64) {
	w, h := contentW, contentH
	if w > ViewportWidth {
		w = ViewportWidth
	}
	if h > ViewportHeight {
		h = ViewportHeight
	}
	return w, h, 1
}

// desktopNotReadyError is the diagnosable failure when the content area still
// does not cover the screen. Outer size is included so a large frame with a
// short content area is obvious in logs and in the inspect not-ready path.
func desktopNotReadyError(g desktopGeom) error {
	return fmt.Errorf("%w: content %dx%d at %d,%d does not cover %dx%d (outer %dx%d)",
		ErrDesktopNotReady, g.contentW, g.contentH, g.left, g.top, ViewportWidth, ViewportHeight, g.outerW, g.outerH)
}

// readWindowBounds uses Browser.getWindowForTarget (includes Bounds).
func (rp *rodPage) readWindowBounds() (*proto.BrowserBounds, error) {
	res, err := proto.BrowserGetWindowForTarget{TargetID: rp.page.TargetID}.Call(rp.page)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("getWindowForTarget: empty result")
	}
	return res.Bounds, nil
}

// presentDesktop focuses the tab, then places the headed window over the Xvfb
// framebuffer at 0,0 with the toolbar visible. Window state and the outer
// rectangle are separate CDP requests. The CSS viewport is pinned to the
// content area once the window settles. Returns ErrDesktopNotReady (with
// content and outer sizes) when it does not, so inspect must not enter Overlay
// searchForNode. The window stays at 0,0 either way, so a failure never clips
// the page.
func (rp *rodPage) presentDesktop() error {
	if rp == nil || rp.page == nil {
		return fmt.Errorf("%w: nil page", ErrDesktopNotReady)
	}
	rp.desktopMu.Lock()
	defer rp.desktopMu.Unlock()
	_ = proto.PageBringToFront{}.Call(rp.page)
	if _, err := rp.page.Activate(); err != nil {
		log.Debug().Err(err).Msg("preview presentDesktop Activate")
	}
	err := rp.presentNormalLocked()
	rp.armDesktopWatchLocked()
	if err != nil {
		log.Warn().Err(err).Msg("preview presentDesktop content area does not cover desktop")
		return err
	}
	return nil
}

// desktopWindow is the CDP surface the window fit needs, so the resize race
// can be exercised without Chromium.
type desktopWindow interface {
	setWindowBounds(bounds *proto.BrowserBounds) error
	readDesktopGeom() (desktopGeom, error)
}

// waitDesktopGeom polls until the outer frame is the requested rectangle and
// the geometry reads the same several times in a row. Chromium reports the new
// outer bounds right after setWindowBounds, but innerWidth/innerHeight keep
// the old size until the X11 resize and relayout finish. On timeout it returns
// the last geometry read; callers decide readiness from it.
func waitDesktopGeom(w desktopWindow, left, top, width, height int) (desktopGeom, error) {
	deadline := time.Now().Add(desktopSettleTimeout)
	var last desktopGeom
	have := false
	stable := 0
	var readErr error
	for {
		g, err := w.readDesktopGeom()
		if err != nil {
			readErr = err
			stable = 0
		} else {
			readErr = nil
			switch {
			case !frameMatches(g, left, top, width, height):
				stable = 0
			case have && g == last:
				stable++
			default:
				stable = 1
			}
			last, have = g, true
		}
		if stable >= desktopSettleStableReads {
			return last, nil
		}
		if !time.Now().Before(deadline) {
			if !have {
				return desktopGeom{}, readErr
			}
			return last, nil
		}
		time.Sleep(desktopSettlePoll)
	}
}

// presentNormal sets the normal state, then the 0,0 1920x1080 frame, and waits
// for the content area to settle below the toolbar.
func presentNormal(w desktopWindow) (desktopGeom, error) {
	var lastErr error
	for attempt := 0; attempt < desktopBoundsRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(desktopBoundsRetry)
		}
		if err := w.setWindowBounds(windowStateOnlyBounds()); err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop set window state")
			continue
		}
		if err := w.setWindowBounds(windowSizeOnlyBounds(0, 0, ViewportWidth, ViewportHeight)); err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop set window size")
			continue
		}
		geom, err := waitDesktopGeom(w, 0, 0, ViewportWidth, ViewportHeight)
		if err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop read geometry")
			continue
		}
		if desktopReady(geom) {
			return geom, nil
		}
		lastErr = desktopNotReadyError(geom)
		log.Debug().
			Int("content_w", geom.contentW).Int("content_h", geom.contentH).
			Int("outer_w", geom.outerW).Int("outer_h", geom.outerH).
			Int("left", geom.left).Int("top", geom.top).
			Int("attempt", attempt).
			Msg("preview presentDesktop content area short of desktop")
	}
	return desktopGeom{}, wrapNotReady(lastErr)
}

func wrapNotReady(err error) error {
	if err == nil {
		err = errors.New("unknown")
	}
	if !errors.Is(err, ErrDesktopNotReady) {
		err = fmt.Errorf("%w: %v", ErrDesktopNotReady, err)
	}
	return err
}

// presentNormalLocked places the window at 0,0 1920x1080 and pins the CSS
// viewport to the content area under the toolbar.
func (rp *rodPage) presentNormalLocked() error {
	rp.clearDeviceMetrics()
	geom, err := presentNormal(rp)
	if err != nil {
		// Note the frame anyway so the watch only refits when it changes
		// instead of retrying a window that cannot fit on every tick.
		if b, berr := rp.readWindowBounds(); berr == nil {
			rp.noteFromBounds(b)
		}
		return err
	}
	return rp.finishDesktop(geom)
}

// presentFullscreenLocked re-pins the viewport after the viewer enters
// fullscreen, which removes the toolbar and grows the content area. If the
// frame is off the screen, it is placed in the normal state and fullscreen is
// entered again: on a display without a window manager, size is ignored while
// fullscreen.
func (rp *rodPage) presentFullscreenLocked() error {
	rp.clearDeviceMetrics()
	var lastErr error
	for attempt := 0; attempt < desktopBoundsRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(desktopBoundsRetry)
		}
		geom, err := waitDesktopGeom(rp, 0, 0, ViewportWidth, ViewportHeight)
		if err == nil && desktopReady(geom) {
			return rp.finishDesktop(geom)
		}
		if err := rp.setWindowBounds(windowStateOnlyBounds()); err != nil {
			lastErr = err
			continue
		}
		if err := rp.setWindowBounds(windowSizeOnlyBounds(0, 0, ViewportWidth, ViewportHeight)); err != nil {
			lastErr = err
			continue
		}
		if err := rp.setWindowBounds(&proto.BrowserBounds{WindowState: proto.BrowserWindowStateFullscreen}); err != nil {
			lastErr = err
			continue
		}
		geom, err = waitDesktopGeom(rp, 0, 0, ViewportWidth, ViewportHeight)
		if err != nil {
			lastErr = err
			continue
		}
		if desktopReady(geom) {
			return rp.finishDesktop(geom)
		}
		lastErr = desktopNotReadyError(geom)
	}
	if b, berr := rp.readWindowBounds(); berr == nil {
		rp.noteFromBounds(b)
	}
	return wrapNotReady(lastErr)
}

func (rp *rodPage) finishDesktop(g desktopGeom) error {
	if err := rp.pinContentViewport(g); err != nil {
		return err
	}
	if b, err := rp.readWindowBounds(); err == nil {
		rp.noteFromBounds(b)
	}
	return nil
}

func (rp *rodPage) clearDeviceMetrics() {
	// Drop any device-metrics override so innerWidth/innerHeight are the real
	// window content, not a forced viewport.
	if err := (proto.EmulationClearDeviceMetricsOverride{}).Call(rp.page); err != nil {
		log.Debug().Err(err).Msg("preview presentDesktop clear device metrics")
	}
}

// armDesktopWatchLocked starts a follower that re-fits the window when the
// viewer enters or leaves fullscreen. Caller holds desktopMu.
func (rp *rodPage) armDesktopWatchLocked() {
	if rp.desktopStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rp.desktopStop = cancel
	go rp.watchDesktop(ctx)
}

func (rp *rodPage) watchDesktop(ctx context.Context) {
	ticker := time.NewTicker(desktopWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rp.maintainDesktop()
		}
	}
}

func (rp *rodPage) maintainDesktop() {
	if rp == nil {
		return
	}
	rp.desktopMu.Lock()
	defer rp.desktopMu.Unlock()
	if rp.page == nil {
		return
	}
	bounds, err := rp.readWindowBounds()
	if err != nil || bounds == nil {
		return
	}
	if rp.desktopNoted.matches(bounds) {
		return
	}
	var fitErr error
	if bounds.WindowState == proto.BrowserWindowStateFullscreen {
		fitErr = rp.presentFullscreenLocked()
	} else {
		fitErr = rp.presentNormalLocked()
	}
	if fitErr != nil {
		log.Debug().Err(fitErr).Str("state", string(bounds.WindowState)).
			Msg("preview desktop watch content area does not cover desktop")
	}
}

func (rp *rodPage) pinContentViewport(g desktopGeom) error {
	w, h, dpr := viewportForContent(g.contentW, g.contentH)
	if w < 1 || h < 1 {
		return desktopNotReadyError(g)
	}
	if err := rp.SetViewport(w, h, dpr); err != nil {
		return fmt.Errorf("%w: set content viewport %dx%d: %v", ErrDesktopNotReady, w, h, err)
	}
	return nil
}

func (rp *rodPage) setWindowBounds(bounds *proto.BrowserBounds) error {
	res, err := proto.BrowserGetWindowForTarget{TargetID: rp.page.TargetID}.Call(rp.page)
	if err != nil {
		return err
	}
	if res == nil {
		return fmt.Errorf("getWindowForTarget: empty result")
	}
	return proto.BrowserSetWindowBounds{WindowID: res.WindowID, Bounds: bounds}.Call(rp.page)
}

func (rp *rodPage) readDesktopGeom() (desktopGeom, error) {
	bounds, err := rp.readWindowBounds()
	if err != nil {
		return desktopGeom{}, err
	}
	contentW, contentH, err := rp.readContentSize()
	if err != nil {
		return desktopGeom{}, err
	}
	return geomFromBounds(bounds, contentW, contentH), nil
}

func geomFromBounds(b *proto.BrowserBounds, contentW, contentH int) desktopGeom {
	g := desktopGeom{contentW: contentW, contentH: contentH}
	if b == nil {
		return g
	}
	if b.Left != nil {
		g.left = *b.Left
	}
	if b.Top != nil {
		g.top = *b.Top
	}
	if b.Width != nil {
		g.outerW = *b.Width
	}
	if b.Height != nil {
		g.outerH = *b.Height
	}
	return g
}

// readContentSize is the visible content area. innerWidth/innerHeight include
// the layout viewport inside the toolbar; layout metrics are the fallback.
func (rp *rodPage) readContentSize() (int, int, error) {
	res, err := rp.page.Eval(`() => ({w: window.innerWidth, h: window.innerHeight})`)
	if err == nil && res != nil {
		w := int(res.Value.Get("w").Num())
		h := int(res.Value.Get("h").Num())
		if w > 0 && h > 0 {
			return w, h, nil
		}
	}
	metrics, merr := proto.PageGetLayoutMetrics{}.Call(rp.page)
	if merr != nil {
		if err != nil {
			return 0, 0, err
		}
		return 0, 0, merr
	}
	if metrics != nil && metrics.CSSLayoutViewport != nil {
		vp := metrics.CSSLayoutViewport
		if vp.ClientWidth > 0 && vp.ClientHeight > 0 {
			return vp.ClientWidth, vp.ClientHeight, nil
		}
	}
	if err != nil {
		return 0, 0, err
	}
	return 0, 0, fmt.Errorf("content size unavailable")
}

func (e *rodEngine) Close() error { return e.ws.Close() }

type rodPage struct {
	engine *rodEngine
	page   *rod.Page

	mu                sync.Mutex
	onPick            func(Pick)
	onInspectCanceled func()
	onDescribeFailed  func()
	inspectCancel     inspectCancelFilter

	desktopMu    sync.Mutex
	desktopStop  context.CancelFunc
	desktopNoted desktopNote
}

// desktopNote is the last outer frame we left covering the screen. The watch
// ignores ticks that still match it, and refits when fullscreen changes it.
type desktopNote struct {
	ok            bool
	state         proto.BrowserWindowState
	left, top     int
	width, height int
}

func (n desktopNote) matches(b *proto.BrowserBounds) bool {
	if !n.ok || b == nil || b.WindowState != n.state {
		return false
	}
	left, top, width, height := 0, 0, 0, 0
	if b.Left != nil {
		left = *b.Left
	}
	if b.Top != nil {
		top = *b.Top
	}
	if b.Width != nil {
		width = *b.Width
	}
	if b.Height != nil {
		height = *b.Height
	}
	return left == n.left && top == n.top && width == n.width && height == n.height
}

func (rp *rodPage) noteFromBounds(b *proto.BrowserBounds) {
	if b == nil {
		return
	}
	n := desktopNote{ok: true, state: b.WindowState}
	if b.Left != nil {
		n.left = *b.Left
	}
	if b.Top != nil {
		n.top = *b.Top
	}
	if b.Width != nil {
		n.width = *b.Width
	}
	if b.Height != nil {
		n.height = *b.Height
	}
	rp.desktopNoted = n
}

func (rp *rodPage) OnPick(cb func(Pick)) {
	rp.mu.Lock()
	rp.onPick = cb
	rp.mu.Unlock()
}

func (rp *rodPage) getPick() func(Pick) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.onPick
}

func (rp *rodPage) OnInspectCanceled(cb func()) {
	rp.mu.Lock()
	rp.onInspectCanceled = cb
	rp.mu.Unlock()
}

func (rp *rodPage) getInspectCanceled() func() {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.onInspectCanceled
}

func (rp *rodPage) OnDescribeFailed(cb func()) {
	rp.mu.Lock()
	rp.onDescribeFailed = cb
	rp.mu.Unlock()
}

func (rp *rodPage) getDescribeFailed() func() {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.onDescribeFailed
}

func (rp *rodPage) StartScreencast(onFrame func(Frame)) error {
	go rp.page.EachEvent(func(e *proto.PageScreencastFrame) {
		w, h := 0, 0
		if e.Metadata != nil {
			w, h = int(e.Metadata.DeviceWidth), int(e.Metadata.DeviceHeight)
		}
		onFrame(Frame{Data: e.Data, DeviceWidth: w, DeviceHeight: h})
		_ = proto.PageScreencastFrameAck{SessionID: e.SessionID}.Call(rp.page)
	})()
	// EveryNthFrame caps encoding at ~30fps (of a ~60fps compositor): a static page
	// emits no frames anyway, so this only bounds bursts and spares server JPEG-encode
	// CPU / bandwidth. Client + server both keep only the latest frame on top of this.
	q, nth := 85, 2
	return proto.PageStartScreencast{
		Format: proto.PageStartScreencastFormatJpeg, Quality: &q, EveryNthFrame: &nth,
	}.Call(rp.page)
}

func (rp *rodPage) DispatchMouse(m MouseEvent) error {
	ev := proto.InputDispatchMouseEvent{X: m.X, Y: m.Y, ClickCount: m.ClickCount, Button: mapButton(m.Button)}
	buttons := m.Buttons
	ev.Buttons = &buttons
	switch m.Type {
	case "move":
		ev.Type = proto.InputDispatchMouseEventTypeMouseMoved
	case "down":
		ev.Type = proto.InputDispatchMouseEventTypeMousePressed
	case "up":
		ev.Type = proto.InputDispatchMouseEventTypeMouseReleased
	case "wheel":
		ev.Type = proto.InputDispatchMouseEventTypeMouseWheel
		ev.DeltaX = m.DeltaX
		ev.DeltaY = m.DeltaY
	default:
		return fmt.Errorf("unknown mouse event %q", m.Type)
	}
	return ev.Call(rp.page)
}

func mapButton(b string) proto.InputMouseButton {
	switch b {
	case "left":
		return proto.InputMouseButtonLeft
	case "middle":
		return proto.InputMouseButtonMiddle
	case "right":
		return proto.InputMouseButtonRight
	default:
		return proto.InputMouseButtonNone
	}
}

func (rp *rodPage) DispatchKey(k KeyEvent) error {
	ev := proto.InputDispatchKeyEvent{Key: k.Key, Code: k.Code, Text: k.Text, WindowsVirtualKeyCode: k.KeyCode}
	switch k.Type {
	case "down":
		ev.Type = proto.InputDispatchKeyEventTypeKeyDown
	case "up":
		ev.Type = proto.InputDispatchKeyEventTypeKeyUp
	case "char":
		ev.Type = proto.InputDispatchKeyEventTypeChar
	default:
		return fmt.Errorf("unknown key event %q", k.Type)
	}
	return ev.Call(rp.page)
}

func (rp *rodPage) SetViewport(width, height int, dpr float64) error {
	if dpr <= 0 {
		dpr = 1
	}
	return proto.EmulationSetDeviceMetricsOverride{
		Width: width, Height: height, DeviceScaleFactor: dpr, Mobile: false,
	}.Call(rp.page)
}

// inspectHighlightConfig is required on every Overlay.setInspectMode call:
// Chromium rejects even mode none without it ("highlight configuration
// parameter is missing") and searchForNode stays armed.
func inspectHighlightConfig() *proto.OverlayHighlightConfig {
	content, border := 0.4, 1.0
	return &proto.OverlayHighlightConfig{
		ContentColor: &proto.DOMRGBA{R: 111, G: 168, B: 220, A: &content},
		BorderColor:  &proto.DOMRGBA{R: 79, G: 133, B: 214, A: &border},
	}
}

func inspectModeRequest(on bool) proto.OverlaySetInspectMode {
	mode := proto.OverlayInspectModeNone
	if on {
		mode = proto.OverlayInspectModeSearchForNode
	}
	return proto.OverlaySetInspectMode{Mode: mode, HighlightConfig: inspectHighlightConfig()}
}

func (rp *rodPage) SetInspect(on bool) error {
	if !on {
		rp.inspectCancel.set(false)
		_ = proto.OverlayHideHighlight{}.Call(rp.page)
		modeErr := inspectModeRequest(false).Call(rp.page)
		// Overlay.disable also leaves inspect mode, so it is the fallback when
		// setInspectMode is rejected.
		disableErr := proto.OverlayDisable{}.Call(rp.page)
		if modeErr != nil && disableErr != nil {
			return fmt.Errorf("leave inspect: %v; overlay disable: %w", modeErr, disableErr)
		}
		return nil
	}
	// Hard gate: refuse Overlay searchForNode when geometry is still wrong —
	// otherwise VNC clicks hit the page (focus/input) instead of inspect.
	if err := rp.presentDesktop(); err != nil {
		return err
	}
	rp.inspectCancel.set(true)
	_ = proto.DOMEnable{}.Call(rp.page)
	_ = proto.OverlayEnable{}.Call(rp.page)
	return inspectModeRequest(true).Call(rp.page)
}

func (rp *rodPage) Navigate(action string) error {
	switch action {
	case "reload":
		return rp.page.Reload()
	case "back":
		return rp.page.NavigateBack()
	case "forward":
		return rp.page.NavigateForward()
	default:
		return fmt.Errorf("unknown navigate action %q", action)
	}
}

func (rp *rodPage) Goto(url string) error {
	if url == "" {
		url = "about:blank"
	}
	return rp.page.Navigate(url)
}

func (rp *rodPage) Close() error {
	rp.desktopMu.Lock()
	if rp.desktopStop != nil {
		rp.desktopStop()
		rp.desktopStop = nil
	}
	rp.desktopMu.Unlock()
	rp.inspectCancel.stop()
	return rp.page.Close()
}

func (rp *rodPage) URL(ctx context.Context) (string, error) {
	info, err := proto.TargetGetTargetInfo{TargetID: rp.page.TargetID}.Call(rp.page.Context(ctx))
	if err != nil {
		return "", err
	}
	return info.TargetInfo.URL, nil
}

// installPickListener wires Overlay pick + cancel events to page callbacks.
func (rp *rodPage) installPickListener() {
	go rp.page.EachEvent(
		func(e *proto.OverlayInspectNodeRequested) {
			cb := rp.getPick()
			if cb == nil {
				_ = rp.SetInspect(false)
				return
			}
			pick, err := rp.describeBackendNode(e.BackendNodeID)
			// One-shot: leave inspect mode after a pick attempt.
			_ = rp.SetInspect(false)
			if err != nil {
				// Distinct from Esc cancel so the UI can show describe-failed tip.
				log.Warn().Err(err).Msg("preview describeBackendNode failed")
				if fail := rp.getDescribeFailed(); fail != nil {
					fail()
				}
				return
			}
			cb(pick)
		},
		func(_ *proto.OverlayInspectModeCanceled) {
			// Enabling inspect often fires this for the previous mode; ignore that
			// or the UI toggle desyncs and cancel never sticks.
			if !rp.inspectCancel.onCanceled() {
				return
			}
			// Chromium only *notifies* on Esc; searchForNode stays armed until we
			// set mode none. Leave inspect here so the next toolbar click can
			// cancel instead of re-entering.
			_ = rp.SetInspect(false)
			if cb := rp.getInspectCanceled(); cb != nil {
				cb()
			}
		},
	)()
}

// pickScript computes a stable-ish CSS selector, tag, outerHTML and box for the
// picked element. `this` is bound to the element by rod's Element.Eval.
const pickScript = `() => {
  const el = this;
  function seg(e){
    if (e.id) return '#' + CSS.escape(e.id);
    let s = e.tagName.toLowerCase();
    const p = e.parentElement;
    if (!p) return s;
    const same = Array.from(p.children).filter(c => c.tagName === e.tagName);
    if (same.length > 1) s += ':nth-of-type(' + (same.indexOf(e)+1) + ')';
    return s;
  }
  function path(e){
    const parts = [];
    while (e && e.nodeType === 1 && e.tagName.toLowerCase() !== 'html'){
      const s = seg(e);
      parts.unshift(s);
      if (s.charAt(0) === '#') break;
      e = e.parentElement;
    }
    return parts.join(' > ');
  }
  const r = el.getBoundingClientRect();
  return {
    selector: path(el),
    tagName: el.tagName.toLowerCase(),
    outerHTML: (el.outerHTML || '').slice(0, 4000),
    url: String(location.href || ''),
    x: r.x, y: r.y, width: r.width, height: r.height
  };
}`

func (rp *rodPage) describeBackendNode(id proto.DOMBackendNodeID) (Pick, error) {
	el, err := rp.page.ElementFromNode(&proto.DOMNode{BackendNodeID: id})
	if err != nil {
		return Pick{}, err
	}
	res, err := el.Eval(pickScript)
	if err != nil {
		return Pick{}, err
	}
	v := res.Value
	return Pick{
		Selector:  v.Get("selector").Str(),
		TagName:   v.Get("tagName").Str(),
		OuterHTML: v.Get("outerHTML").Str(),
		URL:       v.Get("url").Str(),
		Box:       [4]float64{v.Get("x").Num(), v.Get("y").Num(), v.Get("width").Num(), v.Get("height").Num()},
	}, nil
}
