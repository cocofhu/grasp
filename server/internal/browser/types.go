// Package browser implements the server-side VNC preview: each preview-capable
// sandbox runs Xvfb+Chromium+x11vnc+websockify in-process; the platform dials
// that sandbox's CDP (:9222) and websockify (:6080). Pick/navigate use CDP;
// the desktop is streamed to the UI via noVNC.
package browser

import (
	"context"
	"time"
)

// Frame is one screencast frame (already-encoded image bytes plus the device
// pixel dimensions the browser rendered at).
type Frame struct {
	Data         []byte
	DeviceWidth  int
	DeviceHeight int
}

// MouseEvent is a pointer event forwarded from the client to the page.
type MouseEvent struct {
	Type       string  // "move" | "down" | "up" | "wheel"
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button"`     // "left" | "middle" | "right" | "none"
	Buttons    int     `json:"buttons"`    // bitmask of pressed buttons
	ClickCount int     `json:"clickCount"` // for double-click etc.
	DeltaX     float64 `json:"deltaX"`     // wheel
	DeltaY     float64 `json:"deltaY"`     // wheel
}

// KeyEvent is a keyboard event forwarded from the client to the page.
type KeyEvent struct {
	Type    string `json:"type"`    // "down" | "up" | "char"
	Key     string `json:"key"`     // KeyboardEvent.key
	Code    string `json:"code"`    // KeyboardEvent.code
	Text    string `json:"text"`    // resulting text (for char)
	KeyCode int    `json:"keyCode"` // windows virtual key code (best effort)
}

// Pick is a DOM element the user selected in inspect mode.
type Pick struct {
	Selector  string `json:"selector"`
	TagName   string `json:"tagName"`
	OuterHTML string `json:"outerHTML"`
	// URL is location.href at pick time (SPA navigations included).
	URL string     `json:"url,omitempty"`
	Box [4]float64 `json:"box"` // x, y, width, height (CSS px)
}

// Engine is one Chromium instance (one container) we can open tabs in.
type Engine interface {
	// OpenDesktop returns the page the sandbox desktop shows. It takes over the
	// browser's existing page, which the Agent's browser MCP also drives, and
	// navigates it to url when it shows another origin. It opens a page in the
	// default browser context only when none exists.
	OpenDesktop(ctx context.Context, url string) (Page, error)
	// Close disconnects from the browser. It must not stop Chromium.
	Close() error
}

// Page is one tab. All methods are safe to call from the single goroutine that
// owns the session; the Service serializes access per session.
type Page interface {
	// StartScreencast begins streaming frames; onFrame is invoked per frame
	// until the page is closed.
	StartScreencast(onFrame func(Frame)) error
	DispatchMouse(MouseEvent) error
	DispatchKey(KeyEvent) error
	SetViewport(width, height int, dpr float64) error
	// SetInspect toggles element-pick (inspect) mode; picks fire the OnPick cb.
	// When enabling, the implementation must refuse if the headed window is not
	// ready for Overlay hit-testing (see ErrDesktopNotReady).
	SetInspect(on bool) error
	OnPick(func(Pick))
	// OnInspectCanceled is invoked when the user cancels CDP inspect mode
	// (e.g. Esc → Overlay.inspectModeCanceled). Callers sync UI button state.
	// Enabling inspect may itself emit inspectModeCanceled for the previous
	// mode; implementations must not invoke this callback for that side effect.
	// Chromium does not leave searchForNode on this event — implementations
	// must SetInspect(false) before invoking the callback so cancel sticks.
	// Describe/recognition failures use OnDescribeFailed instead.
	OnInspectCanceled(func())
	// OnDescribeFailed is invoked when Overlay pick fires but describing the
	// backend node fails — distinct from user Esc cancel.
	OnDescribeFailed(func())
	// Navigate performs "reload" | "back" | "forward".
	Navigate(action string) error
	// Goto navigates the tab to url (e.g. about:blank or http://…).
	Goto(url string) error
	// URL returns the tab's current URL; an error means the tab is gone.
	URL(ctx context.Context) (string, error)
	Close() error
}

// SandboxExecer is the sole sandbox-manager capability the browser subsystem
// needs: run a command inside a sandbox (over SSH) to start the VNC stack on
// demand. The preview data path (CDP/websockify) is dialed via gateway
// endpoints when SandboxEndpointResolver is also implemented.
type SandboxExecer interface {
	// Exec runs a command inside a named sandbox (used to start VNC on demand).
	Exec(ctx context.Context, name string, timeout time.Duration, cmd ...string) (string, error)
}

// SandboxEndpointResolver resolves gateway-published "host:port" endpoints
// (cdp / novnc). When the sandbox manager implements this, browser requires
// named internal endpoints and will not fall back to sandboxIP:9222/6080
// (that host is usually the LB / bindIP publish surface).
type SandboxEndpointResolver interface {
	EndpointAddr(ctx context.Context, name, key string) (string, error)
}

// engineDialer connects to a Chromium container's CDP endpoint and returns an
// Engine. Overridable in tests; defaults to the go-rod implementation.
type engineDialer func(ctx context.Context, cdpWSURL string) (Engine, error)

// Config tunes the preview tab pool and lifecycle.
type Config struct {
	MaxTabs             int
	MaxTabsPerContainer int
	TabIdleTTL          time.Duration
	ContainerIdleTTL    time.Duration
	// DesktopIdleTTL releases a sandbox's desktop page after no viewer has been
	// attached for this long (Page.Close; the Agent's page is only detached).
	// 0 keeps it until the sandbox goes away.
	DesktopIdleTTL time.Duration
}
