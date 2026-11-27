package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type vncRecPage struct {
	inspect    *bool
	inspectErr error
	offErr     error
	navs       []string
	gotos      []string
	tabs       []string
	url        string
	urlErr     error
}

func (p *vncRecPage) StartScreencast(func(browser.Frame)) error { return nil }
func (p *vncRecPage) DispatchMouse(browser.MouseEvent) error    { return nil }
func (p *vncRecPage) DispatchKey(browser.KeyEvent) error        { return nil }
func (p *vncRecPage) SetViewport(int, int, float64) error       { return nil }
func (p *vncRecPage) SetInspect(on bool) error {
	if p.inspectErr != nil && on {
		return p.inspectErr
	}
	if p.offErr != nil && !on {
		return p.offErr
	}
	p.inspect = &on
	return nil
}
func (p *vncRecPage) OnPick(func(browser.Pick)) {}
func (p *vncRecPage) OnInspectCanceled(func())  {}
func (p *vncRecPage) OnDescribeFailed(func())   {}
func (p *vncRecPage) Navigate(a string) error   { p.navs = append(p.navs, a); return nil }
func (p *vncRecPage) Goto(u string) error       { p.gotos = append(p.gotos, u); return nil }
func (p *vncRecPage) OpenTab(u string) error    { p.tabs = append(p.tabs, u); return nil }
func (p *vncRecPage) Close() error              { return nil }
func (p *vncRecPage) URL(context.Context) (string, error) {
	return p.url, p.urlErr
}

func TestVncReadyURLPrefersPageURL(t *testing.T) {
	ctx := context.Background()
	const nav = "http://127.0.0.1:3000/"
	if got := vncReadyURL(ctx, &vncRecPage{url: "http://127.0.0.1:3000/orders/7"}, nav); got != "http://127.0.0.1:3000/orders/7" {
		t.Fatalf("got %q", got)
	}
	if got := vncReadyURL(ctx, &vncRecPage{}, nav); got != nav {
		t.Fatalf("empty url should fall back, got %q", got)
	}
	if got := vncReadyURL(ctx, &vncRecPage{url: "x", urlErr: errors.New("gone")}, nav); got != nav {
		t.Fatalf("error should fall back, got %q", got)
	}
}

func TestPublicVncMsgAllowed(t *testing.T) {
	cases := []struct {
		m    vncClientMsg
		want bool
	}{
		{vncClientMsg{Type: "inspect", On: true}, true},
		{vncClientMsg{Type: "clipboard", Text: "example", Paste: true}, false},
		{vncClientMsg{Type: "clipboard-watch", ID: 1}, false},
		{vncClientMsg{Type: "clipboard-reset", ID: 1}, false},
		{vncClientMsg{Type: "clipboard-check", ID: 1}, false},
		{vncClientMsg{Type: "navigate", Action: "reload"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "http://127.0.0.1:3000/"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "https://localhost/x"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "http://[::1]:8080/"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "https://github.com/"}, false},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "file:///etc/passwd"}, false},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "about:blank"}, false},
		{vncClientMsg{Type: "navigate", Action: "goto"}, false},
		{vncClientMsg{Type: "navigate", URL: "http://127.0.0.1.evil.com/"}, false},
		{vncClientMsg{Type: "navigate", Action: "open", URL: "http://localhost:3000/"}, true},
		{vncClientMsg{Type: "navigate", Action: "open", URL: "https://github.com/"}, false},
		{vncClientMsg{Type: "navigate", Action: "open"}, false},
	}
	for _, c := range cases {
		if got := publicVncMsgAllowed(c.m); got != c.want {
			t.Errorf("publicVncMsgAllowed(%+v) = %v, want %v", c.m, got, c.want)
		}
	}
}

func TestVncClipboardSourceWithBaseline(t *testing.T) {
	baseline := sandbox.DesktopClipboardState{Generation: "x11", Revision: 7, Owned: true}
	for _, tc := range []struct {
		name  string
		after sandbox.DesktopClipboardState
		want  bool
	}{
		{"terminal interrupt", baseline, false},
		{"copy in the same generation", sandbox.DesktopClipboardState{Generation: "x11", Revision: 8, Owned: true}, true},
		{"lost owner", sandbox.DesktopClipboardState{Generation: "x11", Revision: 8}, false},
		{"new display with an owner", sandbox.DesktopClipboardState{Generation: "new", Revision: 9, Owned: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observations := 0
			read := func(_ context.Context, after sandbox.DesktopClipboardState) (sandbox.DesktopClipboardState, error) {
				if after == (sandbox.DesktopClipboardState{}) {
					observations++
					return baseline, nil
				}
				if after != baseline {
					t.Fatalf("unexpected comparison baseline: %+v", after)
				}
				return tc.after, nil
			}
			clipboard := newVncClipboardObservation(t.Context(), read)
			<-clipboard.sample.ready
			clipboard.watch(1)
			clipboard.watch(1)
			if clipboard.shouldPasteRemote(2) || clipboard.shouldPasteRemote(1) != tc.want || observations != 1 {
				t.Fatalf("wrong resolution for %s", tc.name)
			}
			if clipboard.shouldPasteRemote(1) {
				t.Fatal("an old observation must not resolve another paste")
			}
		})
	}
}

func TestVncClipboardWatchDoesNotBlockDesktopInput(t *testing.T) {
	for _, copyOnInput := range []bool{true, false} {
		t.Run(fmt.Sprintf("copy=%t", copyOnInput), func(t *testing.T) {
			state := sandbox.DesktopClipboardState{Generation: "x11", Revision: 7, Owned: true}
			sampling, allowSample := make(chan struct{}), make(chan struct{})
			firstSample := true
			clipboard := newVncClipboardObservation(t.Context(), func(ctx context.Context, after sandbox.DesktopClipboardState) (sandbox.DesktopClipboardState, error) {
				if after.Generation == "" {
					if !firstSample {
						close(sampling)
						select {
						case <-allowSample:
						case <-ctx.Done():
							return sandbox.DesktopClipboardState{}, ctx.Err()
						}
					}
					firstSample = false
				}
				return state, nil
			})
			<-clipboard.sample.ready
			clipboard.reset(0)
			pending := clipboard.sample
			<-sampling
			nextInput := make(chan struct{})
			go func() {
				clipboard.watch(1)
				// The remote app processes the input before the delayed probe samples.
				if copyOnInput {
					state.Revision++
				}
				close(nextInput)
			}()
			select {
			case <-nextInput:
				close(allowSample)
			case <-time.After(250 * time.Millisecond):
				close(allowSample)
				t.Fatal("watch held up the next RFB frame")
			}
			<-pending.ready
			if got := clipboard.shouldPasteRemote(1); got != copyOnInput {
				t.Fatalf("use remote = %t after input copied = %t", got, copyOnInput)
			}
		})
	}
}

func TestVncClipboardSourceWithoutBaseline(t *testing.T) {
	unowned := sandbox.DesktopClipboardState{Generation: "x11", Revision: 7}
	owned := sandbox.DesktopClipboardState{Generation: "x11", Revision: 8, Owned: true}
	for _, tc := range []struct {
		name         string
		first, later sandbox.DesktopClipboardState
		err          error
		useRemote    bool
	}{
		{"existing owner", owned, owned, nil, true},
		{"copy still in flight", unowned, owned, nil, true},
		{"clipboard stays empty", unowned, unowned, nil, false},
		{"clipboard state timed out", sandbox.DesktopClipboardState{}, sandbox.DesktopClipboardState{}, context.DeadlineExceeded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clipboard := &vncClipboardObservation{
				ctx: t.Context(),
				read: func(_ context.Context, after sandbox.DesktopClipboardState) (sandbox.DesktopClipboardState, error) {
					if after.Generation != "" {
						return tc.later, tc.err
					}
					return tc.first, tc.err
				},
			}
			clipboard.watch(1)
			if got := clipboard.shouldPasteRemote(1); got != tc.useRemote {
				t.Fatalf("use remote = %t, want %t", got, tc.useRemote)
			}
		})
	}
}

func TestVncClipboardRefreshesBaselineOnTakeover(t *testing.T) {
	state := sandbox.DesktopClipboardState{Generation: "x11", Revision: 7, Owned: true}
	clipboard := newVncClipboardObservation(t.Context(), func(_ context.Context, _ sandbox.DesktopClipboardState) (sandbox.DesktopClipboardState, error) {
		return state, nil
	})
	<-clipboard.sample.ready
	// Clipboard changes while the viewer is still watching belong to the new baseline.
	state.Revision++
	clipboard.reset(0)
	<-clipboard.sample.ready
	clipboard.watch(1)
	if clipboard.shouldPasteRemote(1) {
		t.Fatal("clipboard change before takeover replaced the local paste source")
	}
}

func TestVncClipboardPastesAfterTextIsSet(t *testing.T) {
	type frame struct {
		kind int
		data []byte
		err  error
	}
	frames := make(chan frame, 1)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			frames <- frame{err: err}
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		kind, data, err := conn.ReadMessage()
		frames <- frame{kind, data, err}
	}))
	defer peer.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(peer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	msg := vncClientMsg{Type: "clipboard", ID: 2, Paste: true, Shift: true, Text: "多行🙂\n code"}
	var clipboard string
	var result gin.H
	reply := func(msg any) { result = msg.(gin.H) }
	if !writeVncClipboard(t.Context(), nil, func(context.Context, string) error {
		return errors.New("desktop unavailable")
	}, msg, reply) || result["ok"] != false {
		t.Fatalf("failed write: %v", result)
	}
	if !writeVncClipboard(t.Context(), nil, func(context.Context, string) error {
		return &sandbox.ChatHTTPError{StatusCode: http.StatusNotFound}
	}, msg, reply) || result["ok"] != false || result["code"] != "runtime-outdated" {
		t.Fatalf("old runtime: %v", result)
	}
	copyOnly := msg
	copyOnly.Paste = false
	if !writeVncClipboard(t.Context(), nil, func(_ context.Context, text string) error {
		clipboard = text
		return nil
	}, copyOnly, reply) || result["ok"] != true || clipboard != msg.Text {
		t.Fatalf("copy only: clipboard=%q result=%v", clipboard, result)
	}
	if !writeVncClipboard(t.Context(), conn, func(_ context.Context, text string) error {
		clipboard = text
		return nil
	}, msg, reply) || result["ok"] != true || result["id"] != 2 || clipboard != msg.Text {
		t.Fatalf("clipboard=%q result=%v", clipboard, result)
	}
	got := <-frames
	want := []byte{
		4, 1, 0, 0, 0, 0, 0xff, 0xe3, // Ctrl down
		4, 1, 0, 0, 0, 0, 0xff, 0xe1, // Shift down
		4, 1, 0, 0, 0, 0, 0, 'V',
		4, 0, 0, 0, 0, 0, 0, 'V',
		4, 0, 0, 0, 0, 0, 0xff, 0xe1,
		4, 0, 0, 0, 0, 0, 0xff, 0xe3,
	}
	if got.err != nil || got.kind != websocket.BinaryMessage || !bytes.Equal(got.data, want) {
		t.Fatalf("paste frame: %+v", got)
	}
}

func TestPublicTicketPurpose(t *testing.T) {
	cases := []struct {
		port      gateshare.PublicPreview
		requested string
		want      string
	}{
		{gateshare.PublicPreview{Kind: "url", URL: "https://x/", DirectURL: "https://x/"}, "vnc", gateshare.PreviewPurposeAPI},
		{gateshare.PublicPreview{Kind: "port", Port: 3000}, "api", gateshare.PreviewPurposeVNC},
		{gateshare.PublicPreview{Kind: "port", Port: 3000, DirectURL: "http://10.0.0.1:3000/"}, "vnc", gateshare.PreviewPurposeVNC},
		{gateshare.PublicPreview{Kind: "port", Port: 3000, DirectURL: "http://10.0.0.1:3000/"}, "", gateshare.PreviewPurposeVNC},
		{gateshare.PublicPreview{Kind: "port", Port: 3000, DirectURL: "http://10.0.0.1:3000/"}, "api", gateshare.PreviewPurposeAPI},
	}
	for i, c := range cases {
		if got := publicTicketPurpose(c.port, c.requested); got != c.want {
			t.Errorf("case %d: got %q, want %q", i, got, c.want)
		}
	}
}

func decodeVnc(t *testing.T, raw string) vncClientMsg {
	t.Helper()
	var m vncClientMsg
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return m
}

func TestApplyVncMsgInspectNavigate(t *testing.T) {
	h := &Handlers{}
	p := &vncRecPage{}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"inspect","on":true}`), nil)
	if p.inspect == nil || !*p.inspect {
		t.Fatal("inspect should be enabled")
	}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"inspect","on":false}`), nil)
	if p.inspect == nil || *p.inspect {
		t.Fatal("inspect should be disabled on on:false")
	}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"reload"}`), nil)
	if len(p.navs) != 1 || p.navs[0] != "reload" {
		t.Fatalf("navigate wrong: %v", p.navs)
	}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"back"}`), nil)
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"forward"}`), nil)
	if len(p.navs) != 3 || p.navs[1] != "back" || p.navs[2] != "forward" {
		t.Fatalf("back/forward wrong: %v", p.navs)
	}
}

func TestApplyVncMsgIgnoresUnknown(t *testing.T) {
	h := &Handlers{}
	p := &vncRecPage{}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"bogus"}`), nil)
	if p.inspect != nil || len(p.navs) != 0 {
		t.Fatalf("unknown type should be no-op: inspect=%v navs=%v", p.inspect, p.navs)
	}
}

func TestApplyVncMsgPingIsNoop(t *testing.T) {
	h := &Handlers{}
	p := &vncRecPage{}
	var pushed []any
	h.applyVncMsg(p, decodeVnc(t, `{"type":"ping"}`), func(v any) { pushed = append(pushed, v) })
	if p.inspect != nil || len(p.navs) != 0 || len(p.gotos) != 0 || len(pushed) != 0 {
		t.Fatalf("ping should only mark activity: inspect=%v navs=%v gotos=%v pushed=%v", p.inspect, p.navs, p.gotos, pushed)
	}
}

func TestVncToucherThrottles(t *testing.T) {
	n := 0
	now := time.Unix(1000, 0)
	tc := newVncToucher(func() { n++ })
	tc.now = func() time.Time { return now }

	tc.mark()
	if n != 1 {
		t.Fatalf("first mark should touch, got %d", n)
	}
	now = now.Add(vncTouchEvery - time.Second)
	tc.mark()
	if n != 1 {
		t.Fatalf("mark inside the window should be throttled, got %d", n)
	}
	now = now.Add(time.Second)
	tc.mark()
	if n != 2 {
		t.Fatalf("mark after the window should touch again, got %d", n)
	}
}

func TestApplyVncMsgGoto(t *testing.T) {
	h := &Handlers{}
	p := &vncRecPage{}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"goto","url":"https://example.com/"}`), nil)
	if len(p.gotos) != 1 || p.gotos[0] != "https://example.com/" {
		t.Fatalf("goto wrong: %v", p.gotos)
	}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"goto"}`), nil)
	if len(p.gotos) != 2 || p.gotos[1] != "about:blank" {
		t.Fatalf("empty goto should open about:blank: %v", p.gotos)
	}
}

func TestApplyVncMsgOpenTab(t *testing.T) {
	h := &Handlers{}
	p := &vncRecPage{}
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"open","url":"http://127.0.0.1:5173/"}`), nil)
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"open"}`), nil)
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"open","url":"javascript:alert(1)"}`), nil)
	h.applyVncMsg(p, decodeVnc(t, `{"type":"navigate","action":"open","url":"file:///etc/passwd"}`), nil)
	if len(p.tabs) != 1 || p.tabs[0] != "http://127.0.0.1:5173/" || len(p.gotos) != 0 {
		t.Fatalf("open should only open a new tab: tabs=%v gotos=%v", p.tabs, p.gotos)
	}
}

func TestApplyVncMsgInspectNotReadyPushes(t *testing.T) {
	h := &Handlers{}
	p := &vncRecPage{inspectErr: fmt.Errorf("wrap: %w", browser.ErrDesktopNotReady)}
	var pushed []string
	h.applyVncMsg(p, decodeVnc(t, `{"type":"inspect","on":true}`), func(v any) {
		b, _ := json.Marshal(v)
		pushed = append(pushed, string(b))
	})
	if p.inspect != nil {
		t.Fatalf("inspect should not stick when not-ready, got %v", p.inspect)
	}
	if len(pushed) != 1 || pushed[0] != `{"type":"not-ready"}` {
		t.Fatalf("want not-ready push, got %v", pushed)
	}
	// Closing inspect must not push not-ready even if SetInspect errors.
	p.inspectErr = browser.ErrDesktopNotReady
	h.applyVncMsg(p, decodeVnc(t, `{"type":"inspect","on":false}`), func(v any) {
		pushed = append(pushed, "unexpected")
	})
	if len(pushed) != 1 {
		t.Fatalf("on:false should not push not-ready: %v", pushed)
	}
}

func TestApplyVncMsgInspectOffFailurePushes(t *testing.T) {
	h := &Handlers{}
	p := &vncRecPage{offErr: errors.New("highlight configuration parameter is missing")}
	var pushed []string
	h.applyVncMsg(p, decodeVnc(t, `{"type":"inspect","on":false}`), func(v any) {
		b, _ := json.Marshal(v)
		pushed = append(pushed, string(b))
	})
	if len(pushed) != 1 || pushed[0] != `{"type":"inspect-off-failed"}` {
		t.Fatalf("want inspect-off-failed push, got %v", pushed)
	}
}
