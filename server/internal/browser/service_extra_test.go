package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeResolverSandbox struct {
	fakeSandbox
	addrs map[string]string
	err   error
}

func (d *fakeResolverSandbox) EndpointAddr(_ context.Context, _, key string) (string, error) {
	if d.err != nil {
		return "", d.err
	}
	if addr, ok := d.addrs[key]; ok && strings.TrimSpace(addr) != "" {
		return addr, nil
	}
	return "", fmt.Errorf("no %q endpoint", key)
}

func newResolverService(addrs map[string]string) (*Service, *fakeResolverSandbox) {
	d := &fakeResolverSandbox{addrs: addrs}
	s := New(d, Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	s.readyProbe = func(context.Context, string) bool { return true }
	s.dial = func(context.Context, string) (Engine, error) {
		return &fakeEngine{name: "sandbox"}, nil
	}
	return s, d
}

func TestOpenInSandboxDialAndTabFailures(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	s.dial = func(context.Context, string) (Engine, error) {
		return nil, errors.New("dial boom")
	}
	if _, err := s.OpenInSandbox(context.Background(), "sb", "10.0.0.1", "http://x/"); err == nil {
		t.Fatal("expected dial failure")
	}

	s2, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	s2.dial = func(context.Context, string) (Engine, error) {
		return &fakeEngine{failTab: true}, nil
	}
	if _, err := s2.OpenInSandbox(context.Background(), "sb", "10.0.0.1", "http://x/"); err == nil {
		t.Fatal("expected permanent NewTab failure")
	}
}

func TestEnsureSandboxVNCExecFailsStillProbes(t *testing.T) {
	s, d, _ := newFakeService(Config{})
	d.execErr = errors.New("exec failed")
	calls := 0
	s.readyProbe = func(context.Context, string) bool {
		calls++
		return calls >= 3 // first Ensure check false, then after exec loop true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.EnsureSandboxVNC(ctx, "sb", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	n := len(d.execs)
	d.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected one exec despite error, got %d", n)
	}
}

func TestCloseSessionAndTouchMissing(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	s.CloseSession("missing")
	s.touch("missing")
	sess := open(t, s, "sb-a")
	s.CloseSession(sess.ID)
	select {
	case <-sess.Done():
	default:
		t.Fatal("expected closed")
	}
	if sess.Reason() != "closed" {
		t.Fatalf("reason=%q", sess.Reason())
	}
}

func TestStartStopIdempotent(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1, TabIdleTTL: time.Hour, ContainerIdleTTL: time.Hour})
	s.Start()
	sess := open(t, s, "sb-a")
	s.Stop()
	select {
	case <-sess.Done():
	default:
		t.Fatal("expected shutdown close")
	}
	if sess.Reason() != "shutdown" {
		t.Fatalf("reason=%q", sess.Reason())
	}
	s.Stop() // second stop is safe via sync.Once
}

func TestResolvePreviewEndpointsMissingNamedDoesNotDialLB(t *testing.T) {
	s, _ := newResolverService(map[string]string{
		"session": "203.0.113.9:8765",
	})
	cdp, novnc, err := s.resolvePreviewEndpoints(context.Background(), "sb", "203.0.113.9")
	if !errors.Is(err, ErrInternalEndpointMissing) {
		t.Fatalf("want ErrInternalEndpointMissing, got cdp=%q novnc=%q err=%v", cdp, novnc, err)
	}
	if strings.Contains(cdp, "203.0.113.9:9222") || strings.Contains(novnc, "203.0.113.9:6080") ||
		cdp == "203.0.113.9:9222" || novnc == "203.0.113.9:6080" {
		t.Fatalf("must not fall back to LB_IP:9222/6080: cdp=%q novnc=%q", cdp, novnc)
	}
	if _, err := s.OpenInSandbox(context.Background(), "sb", "203.0.113.9", "http://127.0.0.1:3000/"); !errors.Is(err, ErrInternalEndpointMissing) {
		t.Fatalf("OpenInSandbox should fail closed, got %v", err)
	}
}

func TestResolvePreviewEndpointsRejectsPublishSurface(t *testing.T) {
	s, _ := newResolverService(map[string]string{
		"cdp":   "203.0.113.9:9222",
		"novnc": "203.0.113.9:6080",
	})
	cdp, novnc, err := s.resolvePreviewEndpoints(context.Background(), "sb", "203.0.113.9")
	if !errors.Is(err, ErrInternalEndpointMissing) {
		t.Fatalf("LB-published cdp/novnc must fail closed, got cdp=%q novnc=%q err=%v", cdp, novnc, err)
	}
}

func TestResolvePreviewEndpointsUsesClusterDNS(t *testing.T) {
	wantCDP := "sbx-m1.sandboxes.svc.cluster.local:9222"
	wantVNC := "sbx-m1.sandboxes.svc.cluster.local:6080"
	s, _ := newResolverService(map[string]string{
		"cdp":   wantCDP,
		"novnc": wantVNC,
	})
	cdp, novnc, err := s.resolvePreviewEndpoints(context.Background(), "sb", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if cdp != wantCDP || novnc != wantVNC {
		t.Fatalf("cdp=%q novnc=%q", cdp, novnc)
	}
}

func TestAudioWebSocketURL(t *testing.T) {
	s, _ := newResolverService(map[string]string{
		"cdp":   "10.88.0.12:9222",
		"novnc": "10.88.0.12:16080",
	})
	if u, err := s.AudioWebSocketURL(context.Background(), "sb", "10.1.2.3"); err != nil || u != "ws://10.88.0.12:16080/audio" {
		t.Fatalf("url=%q err=%v", u, err)
	}

	bare, _ := newResolverService(map[string]string{"cdp": "10.88.0.12:9222", "novnc": "10.88.0.12"})
	if u, err := bare.AudioWebSocketURL(context.Background(), "sb", "10.1.2.3"); err != nil || u != "ws://10.88.0.12:6080/audio" {
		t.Fatalf("bare host url=%q err=%v", u, err)
	}

	missing, _ := newResolverService(map[string]string{"session": "203.0.113.9:8765"})
	if _, err := missing.AudioWebSocketURL(context.Background(), "sb", "203.0.113.9"); !errors.Is(err, ErrInternalEndpointMissing) {
		t.Fatalf("missing novnc must fail closed, got %v", err)
	}

	legacy, _, _ := newFakeService(Config{MaxTabs: 1, MaxTabsPerContainer: 1, TabIdleTTL: time.Hour, ContainerIdleTTL: time.Hour})
	if u, err := legacy.AudioWebSocketURL(context.Background(), "sb", "10.1.2.3"); err != nil || u != "ws://10.1.2.3:6080/audio" {
		t.Fatalf("legacy url=%q err=%v", u, err)
	}
}

func TestResolvePreviewEndpointsUsesContainerIP(t *testing.T) {
	s, _ := newResolverService(map[string]string{
		"cdp":   "10.88.0.12:9222",
		"novnc": "10.88.0.12:6080",
	})
	cdp, novnc, err := s.resolvePreviewEndpoints(context.Background(), "sb", "10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if cdp != "10.88.0.12:9222" || novnc != "10.88.0.12:6080" {
		t.Fatalf("cdp=%q novnc=%q", cdp, novnc)
	}
}
