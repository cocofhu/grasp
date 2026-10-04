package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
)

// Fixed remote desktop. The Xvfb framebuffer is 1920x1080; the CSS viewport is
// the visible content area after the window covers that framebuffer, not the
// outer frame (tab strip and address bar sit inside the outer frame). Pinned
// via Emulation.setDeviceMetricsOverride at DSF 1, so captured pixels equal CSS
// pixels and client click coordinates map 1:1.
const (
	ViewportWidth  = 1920
	ViewportHeight = 1080

	// contentOriginSlackPx is rounding tolerance for the content origin after
	// the toolbar is shifted off the framebuffer. It is not a size slack: a
	// content area shorter than the desktop is never treated as covered.
	contentOriginSlackPx = 2
	desktopBoundsRetries = 3
	desktopBoundsRetry   = 80 * time.Millisecond
	// desktopWatchInterval re-checks the headed window after the viewer toggles
	// fullscreen. Entering fullscreen hides the toolbar without moving a window
	// that was shifted to park that toolbar off-screen, which clips the page.
	desktopWatchInterval = 300 * time.Millisecond
)

// ErrDesktopNotReady means the headed window's visible content area does not
// cover the Xvfb desktop. Overlay inspect would mis-hit. Callers must refuse
// entering inspect and surface a not-ready control message. The error text
// includes content and outer sizes so a short content area is diagnosable
// even when the outer frame already reads ~1920x1080.
var ErrDesktopNotReady = errors.New("desktop window not ready for inspect")

// dialRod connects to a Chromium container's CDP endpoint (http://ip:9222) and
// returns an Engine. The browser is NOT bound to the caller's request context so
// it outlives individual requests; its lifetime is the container's.
func dialRod(_ context.Context, httpBase string) (Engine, error) {
	ws, err := launcher.ResolveURL(httpBase)
	if err != nil {
		return nil, fmt.Errorf("resolve cdp url: %w", err)
	}
	// NoDefaultDevice: rod's laptop preset (1280x800) would override the layout
	// viewport before we can measure the real content area.
	b := rod.New().ControlURL(ws).NoDefaultDevice()
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &rodEngine{browser: b}, nil
}

type rodEngine struct{ browser *rod.Browser }

func (e *rodEngine) NewTab(_ context.Context, url string) (Page, error) {
	// Each tab gets its own browser context (isolated cookies/storage), disposed
	// on close, so multiple viewers of the same app don't share login state.
	ctxRes, err := proto.TargetCreateBrowserContext{DisposeOnDetach: false}.Call(e.browser)
	if err != nil {
		return nil, fmt.Errorf("create browser context: %w", err)
	}
	page, err := e.browser.Page(proto.TargetCreateTarget{URL: url, BrowserContextID: ctxRes.BrowserContextID})
	if err != nil {
		_ = proto.TargetDisposeBrowserContext{BrowserContextID: ctxRes.BrowserContextID}.Call(e.browser)
		return nil, fmt.Errorf("create page: %w", err)
	}
	rp := &rodPage{engine: e, page: page, ctxID: ctxRes.BrowserContextID}
	// Headed Chromium on Xvfb (no window manager) opens NewTab as another
	// window. presentDesktop covers the framebuffer with the content area and
	// pins the CSS viewport to that content area (DSF 1). Tab open stays
	// best-effort; SetInspect(on) hard-gates on readiness.
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

// fitContentToDesktop returns the outer rectangle that places the content area
// over the whole Xvfb screen. The toolbar/tab-strip inset is shifted off the
// top and left of the framebuffer so it no longer consumes desktop pixels.
func fitContentToDesktop(g desktopGeom) (left, top, width, height int) {
	if g.contentW <= 0 || g.contentH <= 0 || g.outerW <= 0 || g.outerH <= 0 {
		return 0, 0, ViewportWidth, ViewportHeight
	}
	insetX := g.outerW - g.contentW
	insetY := g.outerH - g.contentH
	if insetX < 0 {
		insetX = 0
	}
	if insetY < 0 {
		insetY = 0
	}
	return -insetX, -insetY, ViewportWidth + insetX, ViewportHeight + insetY
}

// contentCoversDesktop reports whether the visible content area covers the
// Xvfb screen and starts at the screen origin. An outer frame of 1920x1080 is
// not enough: the toolbar lives inside that frame, so a short content area
// (or a content origin pushed down by the toolbar) stays not ready.
func contentCoversDesktop(g desktopGeom) bool {
	if g.contentW < ViewportWidth || g.contentH < ViewportHeight {
		return false
	}
	insetX := g.outerW - g.contentW
	insetY := g.outerH - g.contentH
	if insetX < 0 {
		insetX = 0
	}
	if insetY < 0 {
		insetY = 0
	}
	originX := g.left + insetX
	originY := g.top + insetY
	if originX > contentOriginSlackPx || originY > contentOriginSlackPx {
		return false
	}
	if originX < -contentOriginSlackPx || originY < -contentOriginSlackPx {
		return false
	}
	return true
}

// viewportForContent is the CSS viewport at DSF 1. It follows the content
// area. Callers must not pass the outer frame: a 1920x1080 outer size with a
// shorter content area would layout past what the window can paint.
// Content larger than the desktop is clamped to the framebuffer, which is the
// visible content once the toolbar has been shifted off-screen.
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

// presentDesktop focuses the tab, then sizes the headed window so its visible
// content area covers the Xvfb framebuffer. Window state and the outer
// rectangle are separate CDP requests. The CSS viewport is pinned to the
// content area only after that area covers the screen. Returns
// ErrDesktopNotReady (with content and outer sizes) when it still does not,
// so inspect must not enter Overlay searchForNode.
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
	if err != nil {
		log.Warn().Err(err).Msg("preview presentDesktop content area does not cover desktop")
		return err
	}
	rp.armDesktopWatchLocked()
	return nil
}

// presentNormalLocked parks the toolbar off the framebuffer and pins the CSS
// viewport to the content area. State and size are separate requests.
func (rp *rodPage) presentNormalLocked() error {
	rp.clearDeviceMetrics()
	left, top, width, height := 0, 0, ViewportWidth, ViewportHeight
	var lastErr error
	for attempt := 0; attempt < desktopBoundsRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(desktopBoundsRetry)
		}
		if err := rp.setWindowBounds(windowStateOnlyBounds()); err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop set window state")
			continue
		}
		if err := rp.setWindowBounds(windowSizeOnlyBounds(left, top, width, height)); err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop set window size")
			continue
		}
		geom, err := rp.readDesktopGeom()
		if err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop read geometry")
			continue
		}
		if contentCoversDesktop(geom) {
			return rp.finishDesktop(geom)
		}
		// Toolbar still inside the frame, or the window is short. Grow the
		// outer size by the chrome inset and shift that inset off-screen.
		left, top, width, height = fitContentToDesktop(geom)
		if err := rp.setWindowBounds(windowSizeOnlyBounds(left, top, width, height)); err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop fit content")
			continue
		}
		geom, err = rp.readDesktopGeom()
		if err != nil {
			lastErr = err
			log.Debug().Err(err).Int("attempt", attempt).Msg("preview presentDesktop read fitted geometry")
			continue
		}
		if contentCoversDesktop(geom) {
			return rp.finishDesktop(geom)
		}
		lastErr = desktopNotReadyError(geom)
		log.Debug().
			Int("content_w", geom.contentW).Int("content_h", geom.contentH).
			Int("outer_w", geom.outerW).Int("outer_h", geom.outerH).
			Int("left", geom.left).Int("top", geom.top).
			Int("attempt", attempt).
			Msg("preview presentDesktop content area short of desktop")
	}
	if lastErr == nil {
		lastErr = errors.New("unknown")
	}
	if !errors.Is(lastErr, ErrDesktopNotReady) {
		lastErr = fmt.Errorf("%w: %v", ErrDesktopNotReady, lastErr)
	}
	return lastErr
}

// presentFullscreenLocked keeps fullscreen (toolbar hidden) but places the
// outer frame on the Xvfb screen. A window that was shifted up to hide the
// toolbar would clip the page once fullscreen removes that toolbar in place.
// On a display without a window manager, size is ignored while fullscreen, so
// the frame is placed in the normal state and fullscreen is entered again.
func (rp *rodPage) presentFullscreenLocked() error {
	rp.clearDeviceMetrics()
	var lastErr error
	for attempt := 0; attempt < desktopBoundsRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(desktopBoundsRetry)
		}
		_ = rp.setWindowBounds(windowSizeOnlyBounds(0, 0, ViewportWidth, ViewportHeight))
		geom, err := rp.readDesktopGeom()
		if err == nil && contentCoversDesktop(geom) {
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
		time.Sleep(desktopBoundsRetry)
		geom, err = rp.readDesktopGeom()
		if err != nil {
			lastErr = err
			continue
		}
		if contentCoversDesktop(geom) {
			return rp.finishDesktop(geom)
		}
		lastErr = desktopNotReadyError(geom)
	}
	if lastErr == nil {
		lastErr = errors.New("unknown")
	}
	if !errors.Is(lastErr, ErrDesktopNotReady) {
		lastErr = fmt.Errorf("%w: %v", ErrDesktopNotReady, lastErr)
	}
	return lastErr
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

func (e *rodEngine) Close() error { return e.browser.Close() }

type rodPage struct {
	engine *rodEngine
	page   *rod.Page
	ctxID  proto.BrowserBrowserContextID

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

func (rp *rodPage) SetInspect(on bool) error {
	if !on {
		rp.inspectCancel.set(false)
		_ = proto.OverlayHideHighlight{}.Call(rp.page)
		err := proto.OverlaySetInspectMode{Mode: proto.OverlayInspectModeNone}.Call(rp.page)
		if err != nil {
			return err
		}
		_ = proto.OverlayDisable{}.Call(rp.page)
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
	content, border := 0.4, 1.0
	cfg := &proto.OverlayHighlightConfig{
		ContentColor: &proto.DOMRGBA{R: 111, G: 168, B: 220, A: &content},
		BorderColor:  &proto.DOMRGBA{R: 79, G: 133, B: 214, A: &border},
	}
	return proto.OverlaySetInspectMode{Mode: proto.OverlayInspectModeSearchForNode, HighlightConfig: cfg}.Call(rp.page)
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
	err := rp.page.Close()
	// Dispose the isolated browser context so it doesn't leak in a long-lived
	// container.
	_ = proto.TargetDisposeBrowserContext{BrowserContextID: rp.ctxID}.Call(rp.engine.browser)
	return err
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
