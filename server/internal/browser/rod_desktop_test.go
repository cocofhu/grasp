package browser

import (
	"errors"
	"strings"
	"testing"

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

func TestPlan_g1_1_toolbarShiftedOffDesktop(t *testing.T) {
	// Outer frame already equals the screen, but the toolbar ate 120px of content.
	left, top, width, height := fitContentToDesktop(desktopGeom{
		outerW: ViewportWidth, outerH: ViewportHeight, contentW: ViewportWidth, contentH: ViewportHeight - 120,
	})
	if top != -120 || height != ViewportHeight+120 {
		t.Fatalf("top=%d height=%d want top=-120 height=%d", top, height, ViewportHeight+120)
	}
	if left != 0 || width != ViewportWidth {
		t.Fatalf("left=%d width=%d want a horizontal span of the desktop", left, width)
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

func TestPlan_g1_3_outerFrameDoesNotProveContentCovered(t *testing.T) {
	// Old gate treated outer 1920x1080, and even 80px short, as ready.
	outerOK := geomFromBounds(&proto.BrowserBounds{
		Width: intPtr(ViewportWidth), Height: intPtr(ViewportHeight),
	}, ViewportWidth, ViewportHeight-80)
	if contentCoversDesktop(outerOK) {
		t.Fatal("content 80px short of the screen must not be ready, even when the outer frame is 1920x1080")
	}
	oneShort := desktopGeom{
		outerW: ViewportWidth, outerH: ViewportHeight,
		contentW: ViewportWidth, contentH: ViewportHeight - 1,
	}
	if contentCoversDesktop(oneShort) {
		t.Fatal("content 1px short must not be ready")
	}
	if contentCoversDesktop(desktopGeom{}) {
		t.Fatal("empty geometry must not be ready")
	}
	exact := desktopGeom{
		outerW: ViewportWidth, outerH: ViewportHeight,
		contentW: ViewportWidth, contentH: ViewportHeight,
	}
	if !contentCoversDesktop(exact) {
		t.Fatal("content that covers the desktop at the origin should be ready")
	}
}

func TestPlan_g1_3_toolbarStillOnScreenIsNotCovered(t *testing.T) {
	// Window was grown downward so the content size matches the desktop, but
	// the toolbar still occupies the top of the framebuffer.
	onScreen := desktopGeom{
		left: 0, top: 0,
		outerW: ViewportWidth + 120, outerH: ViewportHeight + 120,
		contentW: ViewportWidth, contentH: ViewportHeight,
	}
	if contentCoversDesktop(onScreen) {
		t.Fatal("content origin pushed down by the toolbar must not count as covered")
	}
	onScreen.top = -120
	onScreen.left = -120
	if !contentCoversDesktop(onScreen) {
		t.Fatal("toolbar shifted off the framebuffer should count as covered")
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
