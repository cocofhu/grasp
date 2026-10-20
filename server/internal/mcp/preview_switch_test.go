package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// switchPreviewOps is a direct-preview sandbox whose injector can switch ports.
type switchPreviewOps struct {
	*fakePreviewOps
	res         InjectSwitch
	err         error
	switches    []int
	injectOK    bool
	injectCalls []int
	published   []int
	upFor       map[int]string
}

func newSwitchOps(res InjectSwitch) *switchPreviewOps {
	return &switchPreviewOps{
		fakePreviewOps: &fakePreviewOps{name: "sb", ok: true, healthy: true, direct: true, up: "http://10.0.0.8:18081"},
		res:            res,
		injectOK:       true,
		upFor:          map[int]string{18081: "http://10.0.0.8:18081", 3001: "http://10.0.0.8:49153"},
	}
}

func (s *switchPreviewOps) SwitchInjectUpstream(_ context.Context, _ string, port int) (InjectSwitch, error) {
	s.switches = append(s.switches, port)
	if len(s.switches) > 1 {
		return InjectSwitch{Result: InjectReset, PreviewPort: s.res.PreviewPort}, nil
	}
	return s.res, s.err
}

func (s *switchPreviewOps) ProbeInjectUpstream(_ context.Context, _ string, previewPort, want int) bool {
	s.injectCalls = append(s.injectCalls, previewPort*100000+want)
	return s.injectOK
}

func (s *switchPreviewOps) EnsurePublishedPort(_ context.Context, _ string, port int) (string, bool) {
	s.published = append(s.published, port)
	return "", false
}

func (s *switchPreviewOps) PreviewUpstream(_ context.Context, _ string, port int) (string, bool) {
	if u, ok := s.upFor[port]; ok {
		return u, true
	}
	return "", false
}

func TestSetPreviewSwitchesInjectorToAppPort(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	store := &memPreviewStore{}
	h.SetPreviewStore(store)
	ops := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	h.SetPreviewSandboxOps(ops)

	reg, err := h.setPreviewPort("r", "n", 5173, "web")
	if err != nil {
		t.Fatal(err)
	}
	if !reg.Picked || reg.PublicURL != "http://10.0.0.8:18081/" || reg.Note != "" {
		t.Fatalf("reg=%+v", reg)
	}
	if len(ops.switches) != 1 || ops.switches[0] != 5173 {
		t.Fatalf("switches=%v", ops.switches)
	}
	if ops.probes != 0 {
		t.Fatalf("switched path must probe through PREVIEW_PORT, not the app port: ProbeHTTPPort calls=%d", ops.probes)
	}
	if len(ops.injectCalls) != 2 || ops.injectCalls[0] != 18081*100000+5173 {
		t.Fatalf("inject probes=%v", ops.injectCalls)
	}
	if ops.keepalives != 1 {
		t.Fatalf("keepalives=%d", ops.keepalives)
	}
	if len(ops.published) != 1 || ops.published[0] != 18081 {
		t.Fatalf("only PREVIEW_PORT may be published, got %v", ops.published)
	}
	if len(ops.shown) != 1 || ops.shown[0] != "sb:5173" {
		t.Fatalf("desktop must open the app's own port, not PREVIEW_PORT: %v", ops.shown)
	}
	if len(store.ports) != 1 || store.ports[0].Port != 5173 || store.ports[0].Host != "http://10.0.0.8:18081" {
		t.Fatalf("record must keep Port=app and Host=PREVIEW_PORT: %+v", store.ports)
	}
	ports := h.ListPreviewPorts("r", "n")
	if len(ports) != 1 || ports[0].DirectURL != "http://10.0.0.8:18081/" {
		t.Fatalf("direct url=%+v", ports)
	}
}

func TestSetPreviewSwitchProbeFailureResetsInjector(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	ops.injectOK = false
	h.SetPreviewSandboxOps(ops)

	_, err := h.setPreviewPort("r", "n", 5173, "")
	if err == nil || !strings.Contains(err.Error(), "访问不到") {
		t.Fatalf("err=%v", err)
	}
	if len(ops.switches) != 2 || ops.switches[1] != 18081 {
		t.Fatalf("a failed switch must reset the injector to PREVIEW_PORT: %v", ops.switches)
	}
	if ops.keepalives != 0 {
		t.Fatalf("keepalive must not run, got %d", ops.keepalives)
	}
}

func TestSetPreviewSwitchKeepaliveFailureResetsInjector(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	ops.keepaliveErr = errors.New("keepalive: no listener on port 5173")
	h.SetPreviewSandboxOps(ops)

	if _, err := h.setPreviewPort("r", "n", 5173, ""); err == nil || !strings.Contains(err.Error(), "保活失败") {
		t.Fatalf("err=%v", err)
	}
	if len(ops.switches) != 2 || ops.switches[1] != 18081 {
		t.Fatalf("switches=%v", ops.switches)
	}
}

func TestSetPreviewSwitchReleasesDisplacedRegistration(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	store := &memPreviewStore{ports: []PreviewPort{{
		RunID: "r", NodeID: "n", Kind: PreviewKindPort, Port: 3001,
		ProxyURL: "/preview/r/n/3001/", Host: "http://10.0.0.8:18081", Healthy: true,
	}}}
	h.SetPreviewStore(store)
	ops := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	h.SetPreviewSandboxOps(ops)

	if _, err := h.setPreviewPort("r", "n", 5173, ""); err != nil {
		t.Fatal(err)
	}
	byPort := map[int]PreviewPort{}
	for _, p := range store.ports {
		byPort[p.Port] = p
	}
	if byPort[3001].Host != "http://10.0.0.8:49153" {
		t.Fatalf("displaced 3001 must go back to its own address: %+v", byPort[3001])
	}
	if byPort[5173].Host != "http://10.0.0.8:18081" {
		t.Fatalf("5173=%+v", byPort[5173])
	}
}

func TestSetPreviewResetUsesNormalPath(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := newSwitchOps(InjectSwitch{Result: InjectReset, PreviewPort: 18081})
	h.SetPreviewSandboxOps(ops)

	reg, err := h.setPreviewPort("r", "n", 18081, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reg.Picked || reg.Note != "" || reg.PublicURL != "http://10.0.0.8:18081/" {
		t.Fatalf("reg=%+v", reg)
	}
	if ops.probes == 0 || len(ops.injectCalls) != 0 {
		t.Fatalf("PREVIEW_PORT itself uses the plain probe: probes=%d inject=%v", ops.probes, ops.injectCalls)
	}
}

func TestSetPreviewBusyKeepsPreviewPortAndExplains(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := newSwitchOps(InjectSwitch{Result: InjectBusy, PreviewPort: 18081})
	h.SetPreviewSandboxOps(ops)

	reg, err := h.setPreviewPort("r", "n", 3001, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Picked || !strings.Contains(reg.Note, "已被另一个页面占用") || !strings.Contains(reg.Note, "18081") {
		t.Fatalf("reg=%+v", reg)
	}
	if len(ops.published) != 1 || ops.published[0] != 3001 {
		t.Fatalf("busy falls back to today's path: published=%v", ops.published)
	}
}

func TestSetPreviewSwitchErrorFallsBackWithNote(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := newSwitchOps(InjectSwitch{})
	ops.err = errors.New("ssh down")
	ops.upFor[5173] = "http://10.0.0.8:49160"
	h.SetPreviewSandboxOps(ops)

	reg, err := h.setPreviewPort("r", "n", 5173, "")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Picked || !strings.Contains(reg.Note, "运行时较旧") || reg.PublicURL != "http://10.0.0.8:49160/" {
		t.Fatalf("reg=%+v", reg)
	}
}

func TestSetPreviewSwitchDeniedAndNoListen(t *testing.T) {
	fastPreviewProbe(t)
	for _, tc := range []struct {
		res  string
		want string
	}{
		{InjectDenied, "沙箱自带的服务"},
		{InjectNoListen, "没有进程在监听"},
	} {
		h := NewHost(&memStore{})
		ops := newSwitchOps(InjectSwitch{Result: tc.res, PreviewPort: 18081})
		h.SetPreviewSandboxOps(ops)
		_, err := h.setPreviewPort("r", "n", 4000, "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err=%v", tc.res, err)
		}
		if ops.keepalives != 0 || ops.probes != 0 {
			t.Fatalf("%s: must stop before probe/keepalive", tc.res)
		}
	}
}

func TestSetPreviewRejectsSandboxServicePorts(t *testing.T) {
	h := NewHost(&memStore{})
	ops := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	h.SetPreviewSandboxOps(ops)
	for _, p := range []int{22, 5900, 6080, 8744, 8765, 9222, 17980} {
		_, err := h.setPreviewPort("r", "n", p, "")
		if err == nil || !strings.Contains(err.Error(), "沙箱自带的服务") {
			t.Fatalf("port %d: err=%v", p, err)
		}
	}
	if len(ops.switches) != 0 || ops.keepalives != 0 {
		t.Fatalf("reserved ports must be refused before touching the sandbox: switches=%v keepalives=%d", ops.switches, ops.keepalives)
	}
}

func TestSetPreviewNonDirectNeverSwitches(t *testing.T) {
	fastPreviewProbe(t)
	h := NewHost(&memStore{})
	ops := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	ops.direct = false
	ops.upFor[5173] = "http://10.0.0.8:49160"
	h.SetPreviewSandboxOps(ops)

	reg, err := h.setPreviewPort("r", "n", 5173, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops.switches) != 0 || reg.PublicURL != "" || reg.Note != "" {
		t.Fatalf("switches=%v reg=%+v", ops.switches, reg)
	}
}

func TestPreviewPortResult(t *testing.T) {
	got := previewPortResult(5173, PreviewRegistration{ProxyURL: "/preview/r/n/5173/", PublicURL: "http://10.0.0.8:18081/", Picked: true})
	for _, want := range []string{"审批人入口 http://10.0.0.8:18081/(已注入取点脚本)", "http://127.0.0.1:5173/", "你自测也用这个地址", "/preview/r/n/5173/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing %q", got, want)
		}
	}
	got = previewPortResult(3001, PreviewRegistration{ProxyURL: "/p/", PublicURL: "http://10.0.0.8:49153/", Note: "busy"})
	if strings.Contains(got, "已注入取点脚本") || !strings.HasSuffix(got, ";注意:busy") {
		t.Fatalf("%q", got)
	}
	got = previewPortResult(3000, PreviewRegistration{ProxyURL: "/p/"})
	if strings.Contains(got, "审批人入口") {
		t.Fatalf("non-direct has no public entry: %q", got)
	}
}
