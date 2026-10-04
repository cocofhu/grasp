package browser

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

func TestPlan_g1_1_splitWindowStateAndSize(t *testing.T) {
	state := windowStateOnlyBounds()
	if state.WindowState != proto.BrowserWindowStateNormal {
		t.Fatalf("state=%q", state.WindowState)
	}
	if state.Left != nil || state.Top != nil || state.Width != nil || state.Height != nil {
		t.Fatal("state request must not carry a size; Chromium drops bounds when they share the state request")
	}
	size := windowSizeOnlyBounds(0, 0, ViewportWidth, ViewportHeight)
	if size.WindowState != "" {
		t.Fatalf("size request must omit window state, got %q", size.WindowState)
	}
	if size.Left == nil || size.Top == nil || size.Width == nil || size.Height == nil {
		t.Fatal("size request incomplete")
	}
	if *size.Left != 0 || *size.Top != 0 || *size.Width != ViewportWidth || *size.Height != ViewportHeight {
		t.Fatalf("size=%d,%d %dx%d", *size.Left, *size.Top, *size.Width, *size.Height)
	}
}

func TestPlan_g1_2_viewportUsesContentAreaNotOuterFrame(t *testing.T) {
	// Outer frame is 1920x1080; content is short by the toolbar. Viewport height
	// must be the content height, and device pixel ratio stays 1.
	w, h, dpr := viewportForContent(ViewportWidth, 960)
	if w != ViewportWidth || h != 960 || dpr != 1 {
		t.Fatalf("viewport=%dx%d dpr=%v, want content 1920x960 dpr 1 (not outer 1920x1080)", w, h, dpr)
	}
	w, h, dpr = viewportForContent(ViewportWidth, ViewportHeight)
	if w != ViewportWidth || h != ViewportHeight || dpr != 1 {
		t.Fatalf("covering content viewport=%dx%d dpr=%v", w, h, dpr)
	}
	// Content taller than the framebuffer clamps to the visible desktop, still not the outer frame.
	w, h, dpr = viewportForContent(ViewportWidth+80, ViewportHeight+120)
	if w != ViewportWidth || h != ViewportHeight || dpr != 1 {
		t.Fatalf("clamped viewport=%dx%d dpr=%v", w, h, dpr)
	}
}

func TestPlan_g1_3_toolbarOnScreenIsReady(t *testing.T) {
	// Window at the origin covering the screen; the toolbar takes the top 88px
	// and the content area fills the rest.
	g := desktopGeom{
		outerW: ViewportWidth, outerH: ViewportHeight,
		contentW: ViewportWidth, contentH: ViewportHeight - 88,
	}
	if !desktopReady(g) {
		t.Fatal("toolbar on screen with content filling the rest should be ready")
	}
	// Fullscreen: no toolbar, content equals the frame.
	g.contentH = ViewportHeight
	if !desktopReady(g) {
		t.Fatal("fullscreen content covering the frame should be ready")
	}
	// Chromium reports a couple of pixels of border on Xvfb.
	if !desktopReady(desktopGeom{left: -2, outerW: ViewportWidth + 2, outerH: ViewportHeight, contentW: ViewportWidth - 1, contentH: 992}) {
		t.Fatal("border slack should still be ready")
	}
}

func TestPlan_g1_3_shiftedOrStaleIsNotReady(t *testing.T) {
	cases := map[string]desktopGeom{
		"empty": {},
		// Toolbar parked above the screen clips the page top.
		"shifted up": {top: -88, outerW: ViewportWidth, outerH: ViewportHeight + 88, contentW: ViewportWidth, contentH: ViewportHeight},
		// Outer frame updated, innerHeight still from before the resize.
		"stale content": {outerW: ViewportWidth, outerH: ViewportHeight, contentW: ViewportWidth, contentH: 817},
		"short frame":   {outerW: ViewportWidth, outerH: 900, contentW: ViewportWidth, contentH: 812},
		"narrow":        {outerW: ViewportWidth, outerH: ViewportHeight, contentW: 1280, contentH: 992},
	}
	for name, g := range cases {
		if desktopReady(g) {
			t.Errorf("%s: %+v must not be ready", name, g)
		}
	}
}

// fakeDesktop models Chromium on Xvfb: the outer frame changes as soon as
// setWindowBounds returns, but innerHeight lags for a few reads.
type fakeDesktop struct {
	toolbar    int
	lagReads   int
	staleH     int
	fit        bool
	geom       desktopGeom
	pending    int
	lastBounds *proto.BrowserBounds
}

func (f *fakeDesktop) setWindowBounds(b *proto.BrowserBounds) error {
	if b.Width == nil {
		return nil
	}
	f.lastBounds = b
	f.staleH = f.geom.contentH
	f.geom.left, f.geom.top = *b.Left, *b.Top
	f.geom.outerW, f.geom.outerH = *b.Width, *b.Height
	f.pending = f.lagReads
	return nil
}

func (f *fakeDesktop) readDesktopGeom() (desktopGeom, error) {
	g := f.geom
	g.contentW = g.outerW
	g.contentH = g.outerH - f.toolbar
	if !f.fit {
		g.contentH = g.outerH / 2
	}
	if f.pending > 0 {
		f.pending--
		g.contentH = f.staleH
	}
	f.geom.contentH = g.contentH
	return g, nil
}

func fastSettle(t *testing.T) {
	t.Helper()
	poll, timeout := desktopSettlePoll, desktopSettleTimeout
	desktopSettlePoll, desktopSettleTimeout = time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { desktopSettlePoll, desktopSettleTimeout = poll, timeout })
}

func TestPlan_g1_4_staleInnerHeightWaitsForResize(t *testing.T) {
	fastSettle(t)
	f := &fakeDesktop{toolbar: 88, lagReads: 2, fit: true, geom: desktopGeom{contentH: 817}}
	g, err := presentNormal(f)
	if err != nil {
		t.Fatalf("presentNormal: %v", err)
	}
	if g.top != 0 || g.left != 0 {
		t.Fatalf("window moved to %d,%d; the toolbar must stay on screen", g.left, g.top)
	}
	if g.contentW != ViewportWidth || g.contentH != ViewportHeight-88 {
		t.Fatalf("content=%dx%d, want settled 1920x992 (not the stale 817)", g.contentW, g.contentH)
	}
}

func TestPlan_g1_4_failureLeavesWindowAtOrigin(t *testing.T) {
	fastSettle(t)
	f := &fakeDesktop{toolbar: 88, fit: false}
	_, err := presentNormal(f)
	if !errors.Is(err, ErrDesktopNotReady) {
		t.Fatalf("err=%v, want ErrDesktopNotReady", err)
	}
	b := f.lastBounds
	if b == nil || *b.Left != 0 || *b.Top != 0 || *b.Width != ViewportWidth || *b.Height != ViewportHeight {
		t.Fatalf("last bounds %+v, want 0,0 1920x1080 so the page top is not clipped", b)
	}
}

func TestPlan_g1_5_inspectOffCarriesHighlightConfig(t *testing.T) {
	off := inspectModeRequest(false)
	if off.Mode != proto.OverlayInspectModeNone {
		t.Fatalf("mode=%q", off.Mode)
	}
	if off.HighlightConfig == nil {
		t.Fatal("Chromium rejects setInspectMode none without highlightConfig")
	}
	on := inspectModeRequest(true)
	if on.Mode != proto.OverlayInspectModeSearchForNode || on.HighlightConfig == nil {
		t.Fatalf("on request %+v", on)
	}
}

func TestPlan_g1_3_notReadyErrorNamesContentAndOuter(t *testing.T) {
	err := desktopNotReadyError(desktopGeom{
		left: 0, top: 0,
		outerW: ViewportWidth, outerH: ViewportHeight,
		contentW: ViewportWidth, contentH: 960,
	})
	if !errors.Is(err, ErrDesktopNotReady) {
		t.Fatal("wrapped ErrDesktopNotReady must match errors.Is")
	}
	msg := err.Error()
	for _, want := range []string{"content 1920x960", "does not cover 1920x1080", "outer 1920x1080"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic %q missing %q", msg, want)
		}
	}
}

func intPtr(v int) *int { return &v }
