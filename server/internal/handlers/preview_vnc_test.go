package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/gateshare"
)

type vncRecPage struct {
	inspect    *bool
	inspectErr error
	offErr     error
	navs       []string
	gotos      []string
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
		{vncClientMsg{Type: "navigate", Action: "reload"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "http://127.0.0.1:3000/"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "https://localhost/x"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "http://[::1]:8080/"}, true},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "https://github.com/"}, false},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "file:///etc/passwd"}, false},
		{vncClientMsg{Type: "navigate", Action: "goto", URL: "about:blank"}, false},
		{vncClientMsg{Type: "navigate", Action: "goto"}, false},
		{vncClientMsg{Type: "navigate", URL: "http://127.0.0.1.evil.com/"}, false},
	}
	for _, c := range cases {
		if got := publicVncMsgAllowed(c.m); got != c.want {
			t.Errorf("publicVncMsgAllowed(%+v) = %v, want %v", c.m, got, c.want)
		}
	}
}

func TestPublicTicketPurpose(t *testing.T) {
	cases := []struct {
		port      gateshare.PublicPreviewPort
		requested string
		want      string
	}{
		{gateshare.PublicPreviewPort{Kind: "url", URL: "https://x/", DirectURL: "https://x/"}, "vnc", gateshare.PreviewPurposeAPI},
		{gateshare.PublicPreviewPort{Kind: "port", Port: 3000}, "api", gateshare.PreviewPurposeVNC},
		{gateshare.PublicPreviewPort{Kind: "port", Port: 3000, DirectURL: "http://10.0.0.1:3000/"}, "vnc", gateshare.PreviewPurposeVNC},
		{gateshare.PublicPreviewPort{Kind: "port", Port: 3000, DirectURL: "http://10.0.0.1:3000/"}, "", gateshare.PreviewPurposeVNC},
		{gateshare.PublicPreviewPort{Kind: "port", Port: 3000, DirectURL: "http://10.0.0.1:3000/"}, "api", gateshare.PreviewPurposeAPI},
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

func TestPreviewVNCOpenCtxBindsRequestContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	openCtx, openCancel := context.WithTimeout(parent, 90*time.Second)
	defer openCancel()

	cancel()
	select {
	case <-openCtx.Done():
		if !errors.Is(openCtx.Err(), context.Canceled) {
			t.Fatalf("openCtx.Err() = %v, want context.Canceled", openCtx.Err())
		}
	default:
		t.Fatal("openCtx should be canceled when request context is canceled")
	}
}
