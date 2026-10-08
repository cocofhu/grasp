package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type memPreviewStore struct {
	ports []PreviewPort
	err   error
}

func (m *memPreviewStore) UpsertPreviewPort(rec PreviewPort) error {
	if m.err != nil {
		return m.err
	}
	key := PreviewItemKeyFor(rec)
	for i, p := range m.ports {
		if p.RunID == rec.RunID && p.NodeID == rec.NodeID && PreviewItemKeyFor(p) == key {
			m.ports[i] = rec
			return nil
		}
	}
	m.ports = append(m.ports, rec)
	return nil
}
func (m *memPreviewStore) ListPreviewPorts(runID, nodeID string) ([]PreviewPort, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []PreviewPort
	for _, p := range m.ports {
		if p.RunID == runID && p.NodeID == nodeID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (m *memPreviewStore) GetPreviewPort(runID, nodeID string, port int) (*PreviewPort, bool) {
	for i, p := range m.ports {
		if p.RunID == runID && p.NodeID == nodeID && p.Port == port {
			return &m.ports[i], true
		}
	}
	return nil, false
}
func (m *memPreviewStore) UpdatePreviewHealth(string, string, int, bool) error { return nil }

type fakePreviewOps struct {
	name    string
	ok      bool
	healthy bool
	up      string
	shown   []string
	direct  bool
	// probeSeq, when set, answers successive probes before falling back to healthy.
	probeSeq     []bool
	probes       int
	keepaliveErr error
	keepalives   int
	// keepaliveProbes records the probe count when keepalive ran.
	keepaliveProbes int
}

func (f *fakePreviewOps) SandboxForRunNode(string, string) (string, bool) { return f.name, f.ok }
func (f *fakePreviewOps) ProbeHTTPPort(context.Context, string, int) bool {
	f.probes++
	if f.probes <= len(f.probeSeq) {
		return f.probeSeq[f.probes-1]
	}
	return f.healthy
}
func (f *fakePreviewOps) KeepalivePort(context.Context, string, int) (int, error) {
	f.keepalives++
	f.keepaliveProbes = f.probes
	if f.keepaliveErr != nil {
		return 0, f.keepaliveErr
	}
	return 4242, nil
}

// listenPreviewOps adds in-sandbox listen addresses to fakePreviewOps.
type listenPreviewOps struct {
	*fakePreviewOps
	addrs []string
	err   error
}

func (l *listenPreviewOps) ListenAddrs(context.Context, string, int) ([]string, error) {
	return l.addrs, l.err
}

func fastPreviewProbe(t *testing.T) {
	t.Helper()
	prev := previewProbeInterval
	previewProbeInterval = time.Millisecond
	t.Cleanup(func() { previewProbeInterval = prev })
}

func TestSetPreviewProbesBeforeKeepalive(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := &fakePreviewOps{name: "sb", ok: true, healthy: true, probeSeq: []bool{false, false}, up: "http://10.0.0.1:1"}
	h.SetPreviewSandboxOps(ops)
	if _, err := h.setPreviewPort("r", "n", 3000, ""); err != nil {
		t.Fatalf("app that binds on the third probe should register: %v", err)
	}
	if ops.keepalives != 1 || ops.keepaliveProbes != 3 {
		t.Fatalf("keepalive must run after a successful probe: keepalives=%d probesBefore=%d", ops.keepalives, ops.keepaliveProbes)
	}
	if ops.probes != 4 {
		t.Fatalf("want a confirming probe after keepalive, probes=%d", ops.probes)
	}
}

func TestSetPreviewUnreachableSkipsKeepalive(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := &fakePreviewOps{name: "sb", ok: true, healthy: false}
	h.SetPreviewSandboxOps(ops)
	_, err := h.setPreviewPort("r", "n", 3000, "")
	if err == nil {
		t.Fatal("unreachable port should fail")
	}
	if ops.keepalives != 0 {
		t.Fatalf("keepalive must not run on an unreachable port, got %d", ops.keepalives)
	}
	if ops.probes != previewProbeAttempts {
		t.Fatalf("probes=%d want %d", ops.probes, previewProbeAttempts)
	}
	if !strings.Contains(err.Error(), "0.0.0.0:3000") {
		t.Fatalf("error should say how to bind: %v", err)
	}
}

func TestSetPreviewUnreachableNamesListenAddress(t *testing.T) {
	fastPreviewProbe(t)
	cases := []struct {
		name  string
		addrs []string
		err   error
		want  []string
	}{
		{"loopback", []string{"127.0.0.1:3000", "[::1]:3000"}, nil, []string{"只监听在 127.0.0.1:3000, [::1]:3000", "0.0.0.0:3000"}},
		{"none", nil, nil, []string{"没有进程在监听"}},
		{"wildcard", []string{"0.0.0.0:3000"}, nil, []string{"已在 0.0.0.0:3000 监听", "没有响应"}},
		{"inspect error", nil, errors.New("ssh down"), []string{"不可达", "0.0.0.0:3000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHost(&memStore{})
			h.SetPreviewSandboxOps(&listenPreviewOps{fakePreviewOps: &fakePreviewOps{name: "sb", ok: true}, addrs: tc.addrs, err: tc.err})
			_, err := h.setPreviewPort("r", "n", 3000, "")
			if err == nil {
				t.Fatal("want error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error %q missing %q", err, w)
				}
			}
		})
	}
}

func TestSetPreviewKeepaliveErrorCarriesReason(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := &fakePreviewOps{name: "sb", ok: true, healthy: true, keepaliveErr: errors.New("keepalive: cannot read cmdline for pid 7 (exit status 1)")}
	h.SetPreviewSandboxOps(ops)
	_, err := h.setPreviewPort("r", "n", 3000, "")
	if err == nil || !strings.Contains(err.Error(), "cannot read cmdline") || !strings.Contains(err.Error(), "可以访问") {
		t.Fatalf("keepalive failure should keep the script reason: %v", err)
	}
}

func TestAllLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:3000":   true,
		"[::1]:3000":       true,
		"localhost:3000":   true,
		"0.0.0.0:3000":     false,
		"*:3000":           false,
		"[::]:3000":        false,
		"10.0.0.5:3000":    false,
		"[fe80::1%eth0]:3": false,
	}
	for in, want := range cases {
		if got := allLoopback([]string{in}); got != want {
			t.Fatalf("allLoopback(%q)=%v want %v", in, got, want)
		}
	}
	if allLoopback([]string{"127.0.0.1:1", "0.0.0.0:1"}) {
		t.Fatal("mixed list is not loopback-only")
	}
}
func (f *fakePreviewOps) PreviewUpstream(context.Context, string, int) (string, bool) {
	if f.up == "" {
		return "", false
	}
	return f.up, true
}
func (f *fakePreviewOps) ShowPreviewOnDesktop(sandboxName string, port int) {
	f.shown = append(f.shown, fmt.Sprintf("%s:%d", sandboxName, port))
}
func (f *fakePreviewOps) DirectPreview(string, string) bool { return f.direct }

func TestParsePreviewPortTypes(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{float64(3000), 3000, true},
		{int(80), 80, true},
		{int64(443), 443, true},
		{"8080", 8080, true},
		{" 9 ", 9, true},
		{"bad", 0, false},
		{true, 0, false},
	}
	for _, tc := range cases {
		got, err := parsePreviewPort(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("in=%v got=%d err=%v", tc.in, got, err)
			}
		} else if err == nil {
			t.Fatalf("in=%v want error", tc.in)
		}
	}
}

func TestListPreviewPortsMergeStore(t *testing.T) {
	h := NewHost(&memStore{})
	store := &memPreviewStore{ports: []PreviewPort{{
		RunID: "r", NodeID: "n", Port: 1, Label: "db", ProxyURL: "/preview/r/n/1/",
	}}}
	h.SetPreviewStore(store)
	h.SetPreviewSandboxOps(&fakePreviewOps{name: "sb", ok: true, healthy: true, up: "http://10.0.0.1:1"})

	// memory + db merge
	h.mu.Lock()
	h.previewMem = map[string][]PreviewPort{"r|n": {{
		RunID: "r", NodeID: "n", Port: 2, Label: "mem", ProxyURL: "/preview/r/n/2/",
	}}}
	h.mu.Unlock()
	ports := h.ListPreviewPorts("r", "n")
	if len(ports) != 2 {
		t.Fatalf("merged=%d %+v", len(ports), ports)
	}

	url, err := h.setPreviewPort("r", "n", 3000, "web")
	if err != nil || url == "" {
		t.Fatalf("setPreviewPort: %v %q", err, url)
	}
	ops := h.previewOps.(*fakePreviewOps)
	if len(ops.shown) != 1 || ops.shown[0] != "sb:3000" {
		t.Fatalf("shown=%v", ops.shown)
	}
	if _, err := h.setPreviewPort("r", "n", 0, ""); err == nil {
		t.Fatal("port 0")
	}
	if _, err := h.setPreviewPort("r", "n", 70000, ""); err == nil {
		t.Fatal("port high")
	}
	_ = time.Second
}

func TestPreviewReadySignalAndKeepalivePID(t *testing.T) {
	h := NewHost(&memStore{})
	h.SetPreviewSandboxOps(&fakePreviewOps{name: "sb", ok: true, healthy: true, up: "http://10.0.0.1:3000"})
	ch := h.PreviewReadyChan("r", "n")
	select {
	case <-ch:
		t.Fatal("should not be ready yet")
	default:
	}
	if _, err := h.setPreviewPort("r", "n", 3000, "web"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("expected PreviewReady after healthy set_preview")
	}
	pids := h.ListPreviewKeepalivePIDs("r", "n")
	if len(pids) != 1 || pids[0] != 4242 {
		t.Fatalf("keepalive pids=%v", pids)
	}
	if !h.IsPreviewKeepalivePID("r", 4242) {
		t.Fatal("expected whitelist hit")
	}
	if h.IsPreviewKeepalivePID("r", 1) {
		t.Fatal("unexpected whitelist hit")
	}
}

func TestSetPreviewDirectStillShowsDesktopAndSetsDirectURL(t *testing.T) {
	h := NewHost(&memStore{})
	h.SetPreviewSandboxOps(&fakePreviewOps{name: "sb", ok: true, healthy: true, up: "http://10.0.0.8:18081", direct: true})
	if _, err := h.setPreviewPort("r", "n", 18081, "web"); err != nil {
		t.Fatal(err)
	}
	ops := h.previewOps.(*fakePreviewOps)
	if len(ops.shown) != 1 || ops.shown[0] != "sb:18081" {
		t.Fatalf("direct mode must still show the app on the desktop: %v", ops.shown)
	}
	ports := h.ListPreviewPorts("r", "n")
	if len(ports) != 1 || ports[0].Mode != "direct" || ports[0].DirectURL != "http://10.0.0.8:18081/" {
		t.Fatalf("ports=%+v", ports)
	}
}
