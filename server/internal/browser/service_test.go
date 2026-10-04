package browser

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakePage struct {
	mu         sync.Mutex
	closed     bool
	onPick     func(Pick)
	onCanceled func()
	onFailed   func()
	mouse      []MouseEvent
	inspect    bool
	url        string
	urlErr     error
	gotos      []string
}

func (p *fakePage) StartScreencast(func(Frame)) error   { return nil }
func (p *fakePage) DispatchMouse(m MouseEvent) error    { p.mouse = append(p.mouse, m); return nil }
func (p *fakePage) DispatchKey(KeyEvent) error          { return nil }
func (p *fakePage) SetViewport(int, int, float64) error { return nil }
func (p *fakePage) SetInspect(on bool) error            { p.inspect = on; return nil }
func (p *fakePage) OnPick(cb func(Pick))                { p.onPick = cb }
func (p *fakePage) OnInspectCanceled(cb func())         { p.onCanceled = cb }
func (p *fakePage) OnDescribeFailed(cb func())          { p.onFailed = cb }
func (p *fakePage) Navigate(string) error               { return nil }
func (p *fakePage) Goto(u string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gotos = append(p.gotos, u)
	p.url = u
	return nil
}
func (p *fakePage) URL(context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url, p.urlErr
}
func (p *fakePage) setURLErr(err error) {
	p.mu.Lock()
	p.urlErr = err
	p.mu.Unlock()
}
func (p *fakePage) Close() error { p.closed = true; return nil }

type fakeEngine struct {
	name     string
	failTab  bool
	failOnce bool
	closed   bool
	pages    []*fakePage
}

func (e *fakeEngine) NewTab(_ context.Context, url string) (Page, error) {
	if e.failOnce {
		e.failOnce = false
		return nil, errors.New("tab failed")
	}
	if e.failTab {
		return nil, errors.New("tab failed")
	}
	p := &fakePage{url: url}
	e.pages = append(e.pages, p)
	return p, nil
}
func (e *fakeEngine) Close() error { e.closed = true; return nil }

// fakeSandbox implements SandboxExecer: the only sandbox-manager capability the
// browser subsystem uses (start VNC on demand). It records exec invocations.
type fakeSandbox struct {
	mu      sync.Mutex
	execs   []string
	execErr error
}

func (d *fakeSandbox) Exec(_ context.Context, name string, _ time.Duration, _ ...string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.execs = append(d.execs, name)
	return "", d.execErr
}

func newFakeService(cfg Config) (*Service, *fakeSandbox, *clock) {
	d := &fakeSandbox{}
	s := New(d, cfg)
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s.reg.now = c.now
	s.readyProbe = func(context.Context, string) bool { return true }
	s.dial = func(_ context.Context, _ string) (Engine, error) {
		return &fakeEngine{name: "sandbox"}, nil
	}
	return s, d, c
}

func TestOpenInSandboxAttachesWithoutPool(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	ctx := context.Background()
	sess, err := s.OpenInSandbox(ctx, "grasp-sb-preview", "10.0.0.9", "http://127.0.0.1:3000/")
	if err != nil {
		t.Fatalf("OpenInSandbox: %v", err)
	}
	defer sess.Close()
	if sess.container != "grasp-sb-preview" {
		t.Fatalf("container = %q", sess.container)
	}
	vnc, err := sess.VNCWebSocketURL()
	if err != nil {
		t.Fatal(err)
	}
	if vnc != "ws://10.0.0.9:6080" {
		t.Fatalf("vnc url = %q", vnc)
	}
	// Stop only disconnects; the sandbox lifecycle is not our concern.
	s.Stop()
}

func TestOpenInSandboxSupersedesExistingSession(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	ctx := context.Background()
	sandbox := "grasp-sb-preview"
	s1, err := s.OpenInSandbox(ctx, sandbox, "10.0.0.9", "http://127.0.0.1:3000/")
	if err != nil {
		t.Fatalf("first OpenInSandbox: %v", err)
	}
	s2, err := s.OpenInSandbox(ctx, sandbox, "10.0.0.9", "http://127.0.0.1:3000/")
	if err != nil {
		t.Fatalf("second OpenInSandbox: %v", err)
	}
	select {
	case <-s1.Done():
	default:
		t.Fatal("s1 should have been superseded")
	}
	if s1.Reason() != "superseded" {
		t.Fatalf("s1 reason = %q, want superseded", s1.Reason())
	}
	s.mu.Lock()
	count := s.reg.containerCount(sandbox)
	s.mu.Unlock()
	if count != 1 {
		t.Fatalf("containerCount = %d, want 1", count)
	}
	if s2.Reason() != "" {
		t.Fatalf("s2 should still be active, reason = %q", s2.Reason())
	}
}

func TestOpenInSandboxRedialsEngineOnNewTabFailure(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	dialCount := 0
	s.dial = func(_ context.Context, _ string) (Engine, error) {
		dialCount++
		return &fakeEngine{name: "sandbox", failOnce: dialCount == 1}, nil
	}
	ctx := context.Background()
	sess, err := s.OpenInSandbox(ctx, "grasp-sb-preview", "10.0.0.9", "http://127.0.0.1:3000/")
	if err != nil {
		t.Fatalf("OpenInSandbox: %v", err)
	}
	defer sess.Close()
	if dialCount != 2 {
		t.Fatalf("dial count = %d, want 2 (initial + redial)", dialCount)
	}
}

func TestOpenInSandboxRespectsCanceledContext(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	s.readyProbe = func(ctx context.Context, _ string) bool {
		<-ctx.Done()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.OpenInSandbox(ctx, "grasp-sb-preview", "10.0.0.9", "http://127.0.0.1:3000/")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// open is a helper that opens a tab in a distinct sandbox (each sandbox is one
// external CDP attachment).
func open(t *testing.T, s *Service, sandbox string) *Session {
	t.Helper()
	sess, err := s.OpenInSandbox(context.Background(), sandbox, "10.0.0.9", "http://127.0.0.1:3000/")
	if err != nil {
		t.Fatalf("OpenInSandbox(%s): %v", sandbox, err)
	}
	return sess
}

func TestEvictsLRUWhenFull(t *testing.T) {
	s, _, c := newFakeService(Config{MaxTabs: 2, MaxTabsPerContainer: 1})
	s1 := open(t, s, "sb-a")
	c.add(time.Second)
	s2 := open(t, s, "sb-b")
	c.add(time.Second)
	// Full (2/2): opening a third sandbox evicts the LRU (s1).
	s3 := open(t, s, "sb-c")
	select {
	case <-s1.Done():
	default:
		t.Fatal("s1 should have been evicted")
	}
	if s1.Reason() != "evicted" {
		t.Fatalf("s1 reason = %q, want evicted", s1.Reason())
	}
	if s1.page.(*fakePage).closed {
		t.Fatal("evicting a viewer must keep its desktop page open")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[s2.ID]; !ok {
		t.Fatal("s2 should still be present")
	}
	if _, ok := s.sessions[s3.ID]; !ok {
		t.Fatal("s3 should be present")
	}
	if s.reg.count() != 2 {
		t.Fatalf("tabs = %d, want 2", s.reg.count())
	}
}

func TestCapacityWhenMaxZero(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 0, MaxTabsPerContainer: 5})
	if _, err := s.OpenInSandbox(context.Background(), "sb-a", "10.0.0.9", "http://app/"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err = %v, want ErrCapacity", err)
	}
}

func TestCloseSession(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 10, MaxTabsPerContainer: 5})
	sess := open(t, s, "sb-a")
	page := sess.page.(*fakePage)
	page.inspect = true
	sess.Close()
	if page.closed {
		t.Fatal("detaching a viewer must keep the desktop page open")
	}
	if page.inspect {
		t.Fatal("detach should leave inspect mode")
	}
	select {
	case <-sess.Done():
	default:
		t.Fatal("Done not closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reg.count() != 0 {
		t.Fatalf("tabs = %d, want 0", s.reg.count())
	}
	if d := s.desktops["sb-a"]; d == nil || d.owner != nil {
		t.Fatalf("desktop should stay registered without owner: %+v", d)
	}
}

func TestSweepDetachesIdleViewerButKeepsDesktop(t *testing.T) {
	s, _, c := newFakeService(Config{MaxTabs: 10, MaxTabsPerContainer: 5, TabIdleTTL: time.Minute, ContainerIdleTTL: 2 * time.Minute})
	sess := open(t, s, "sb-a")
	c.add(90 * time.Second)
	s.sweep()
	if s.reg.count() != 0 {
		t.Fatalf("tabs after idle sweep = %d, want 0", s.reg.count())
	}
	if sess.Reason() != "idle" {
		t.Fatalf("reason = %q, want idle", sess.Reason())
	}
	// Past the container idle TTL the engine stays: it holds the desktop page.
	c.add(3 * time.Minute)
	s.sweep()
	s.mu.Lock()
	nContainers, nDesktops := len(s.containers), len(s.desktops)
	s.mu.Unlock()
	if nContainers != 1 || nDesktops != 1 {
		t.Fatalf("containers=%d desktops=%d, want 1/1", nContainers, nDesktops)
	}
	if sess.page.(*fakePage).closed {
		t.Fatal("desktop page closed by sweep without DesktopIdleTTL")
	}
}

func TestSweepClosesDesktopAfterIdleTTLThenDropsEngine(t *testing.T) {
	s, _, c := newFakeService(Config{MaxTabs: 10, MaxTabsPerContainer: 5, TabIdleTTL: time.Minute, ContainerIdleTTL: 2 * time.Minute, DesktopIdleTTL: 5 * time.Minute})
	sess := open(t, s, "sb-a")
	page := sess.page.(*fakePage)
	sess.Close()
	c.add(4 * time.Minute)
	s.sweep()
	if page.closed {
		t.Fatal("desktop closed before DesktopIdleTTL")
	}
	c.add(2 * time.Minute)
	s.sweep()
	if !page.closed {
		t.Fatal("desktop should close after DesktopIdleTTL")
	}
	c.add(3 * time.Minute)
	s.sweep()
	s.mu.Lock()
	nContainers := len(s.containers)
	s.mu.Unlock()
	if nContainers != 0 {
		t.Fatalf("containers after reap = %d, want 0", nContainers)
	}
}

func TestSweepForgetsDesktopWhosePageIsGone(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 10, MaxTabsPerContainer: 5})
	sess := open(t, s, "sb-a")
	sess.Close()
	sess.page.(*fakePage).setURLErr(errors.New("target closed"))
	s.sweep()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.desktops) != 0 || len(s.containers) != 0 {
		t.Fatalf("desktops=%d containers=%d, want 0/0", len(s.desktops), len(s.containers))
	}
}

func TestSweepKeepsAttachedDesktopEvenIfProbeWouldFail(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 10, MaxTabsPerContainer: 5})
	sess := open(t, s, "sb-a")
	defer sess.Close()
	sess.page.(*fakePage).setURLErr(errors.New("target closed"))
	s.sweep()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.desktops) != 1 {
		t.Fatalf("attached desktop should not be probed away, desktops=%d", len(s.desktops))
	}
}

func TestReattachReusesDesktopWithoutNavigating(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	eng := &fakeEngine{name: "sandbox"}
	s.dial = func(context.Context, string) (Engine, error) { return eng, nil }
	s1 := open(t, s, "sb-a")
	page := s1.page.(*fakePage)
	page.url = "http://127.0.0.1:3000/settings?tab=2"
	s1.Close()
	s2 := open(t, s, "sb-a")
	defer s2.Close()
	if len(eng.pages) != 1 {
		t.Fatalf("NewTab calls = %d, want 1", len(eng.pages))
	}
	if s2.page != Page(page) {
		t.Fatal("second viewer should attach to the same page")
	}
	if len(page.gotos) != 0 {
		t.Fatalf("same origin must not navigate, gotos=%v", page.gotos)
	}
}

func TestReattachOtherPortNavigatesSamePage(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	eng := &fakeEngine{name: "sandbox"}
	s.dial = func(context.Context, string) (Engine, error) { return eng, nil }
	s1 := open(t, s, "sb-a")
	s2, err := s.OpenInSandbox(context.Background(), "sb-a", "10.0.0.9", "http://127.0.0.1:5173/")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s1.Reason() != "superseded" {
		t.Fatalf("s1 reason = %q, want superseded", s1.Reason())
	}
	page := s2.page.(*fakePage)
	if len(eng.pages) != 1 || page.closed {
		t.Fatalf("NewTab calls = %d closed=%v, want one open page", len(eng.pages), page.closed)
	}
	if len(page.gotos) != 1 || page.gotos[0] != "http://127.0.0.1:5173/" {
		t.Fatalf("gotos = %v", page.gotos)
	}
}

func TestReattachAboutBlankKeepsPage(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	s1 := open(t, s, "sb-a")
	s1.Close()
	s2, err := s.OpenInSandbox(context.Background(), "sb-a", "10.0.0.9", "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if g := s2.page.(*fakePage).gotos; len(g) != 0 {
		t.Fatalf("about:blank must not navigate an existing page, gotos=%v", g)
	}
}

func TestReattachRebuildsDeadDesktop(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	var engines []*fakeEngine
	s.dial = func(context.Context, string) (Engine, error) {
		e := &fakeEngine{name: "sandbox"}
		engines = append(engines, e)
		return e, nil
	}
	s1 := open(t, s, "sb-a")
	old := s1.page.(*fakePage)
	old.setURLErr(errors.New("target closed"))
	s2 := open(t, s, "sb-a")
	defer s2.Close()
	if s2.page == Page(old) {
		t.Fatal("dead desktop should be replaced")
	}
	if s1.Reason() != "superseded" {
		t.Fatalf("s1 reason = %q", s1.Reason())
	}
	if len(engines) != 2 || !engines[0].closed {
		t.Fatalf("engine should be redialed after a dead page: engines=%d", len(engines))
	}
}

func TestPickRoutesToAttachedViewerOnly(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	s1 := open(t, s, "sb-a")
	var got1, got2 []string
	var canceled2, failed2 int
	s1.Page().OnPick(func(p Pick) { got1 = append(got1, p.Selector) })
	page := s1.page.(*fakePage)
	page.onPick(Pick{Selector: "#a"})

	s2 := open(t, s, "sb-a")
	defer s2.Close()
	s2.Page().OnPick(func(p Pick) { got2 = append(got2, p.Selector) })
	s2.Page().OnInspectCanceled(func() { canceled2++ })
	s2.Page().OnDescribeFailed(func() { failed2++ })
	page.onPick(Pick{Selector: "#b"})
	page.onCanceled()
	page.onFailed()
	if len(got1) != 1 || got1[0] != "#a" {
		t.Fatalf("superseded viewer got %v", got1)
	}
	if len(got2) != 1 || got2[0] != "#b" || canceled2 != 1 || failed2 != 1 {
		t.Fatalf("attached viewer got picks=%v canceled=%d failed=%d", got2, canceled2, failed2)
	}

	s2.Close()
	page.inspect = true
	page.onPick(Pick{Selector: "#c"})
	page.onCanceled()
	page.onFailed()
	if page.inspect {
		t.Fatal("a pick with no viewer attached should leave inspect mode")
	}
	if len(got2) != 1 {
		t.Fatalf("detached viewer still received picks: %v", got2)
	}
}

func TestViewerPageCloseIsNoop(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	sess := open(t, s, "sb-a")
	defer sess.Close()
	if err := sess.Page().Close(); err != nil {
		t.Fatal(err)
	}
	if sess.page.(*fakePage).closed {
		t.Fatal("viewer Close must not close the desktop page")
	}
}

func TestStopKeepsDesktopPagesOpen(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 1})
	sess := open(t, s, "sb-a")
	s.Stop()
	if sess.page.(*fakePage).closed {
		t.Fatal("Stop must leave sandbox pages open")
	}
	if sess.Reason() != "shutdown" {
		t.Fatalf("reason = %q", sess.Reason())
	}
}

func TestNeedsNavigate(t *testing.T) {
	cases := []struct {
		cur, target string
		want        bool
	}{
		{"http://127.0.0.1:3000/a?b=1", "http://127.0.0.1:3000/", false},
		{"http://127.0.0.1:3000/", "http://127.0.0.1:5173/", true},
		{"about:blank", "http://127.0.0.1:3000/", true},
		{"chrome-error://chromewebdata/", "http://127.0.0.1:3000/", true},
		{"http://127.0.0.1:3000/", "about:blank", false},
		{"http://127.0.0.1:3000/", "", false},
		{"%zz", "http://127.0.0.1:3000/", true},
		{"http://127.0.0.1:3000/", "http://%zz", true},
	}
	for _, c := range cases {
		if got := needsNavigate(c.cur, c.target); got != c.want {
			t.Errorf("needsNavigate(%q, %q) = %v, want %v", c.cur, c.target, got, c.want)
		}
	}
}

func TestSetMaxTabsRejectsBelowOne(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 4, MaxTabsPerContainer: 5})
	if err := s.SetMaxTabs(0); !errors.Is(err, ErrInvalidMaxTabs) {
		t.Fatalf("SetMaxTabs(0) = %v, want ErrInvalidMaxTabs", err)
	}
	st := s.Stats()
	if st.MaxTabs != 4 {
		t.Fatalf("MaxTabs = %d, want 4 (unchanged)", st.MaxTabs)
	}
}

func TestSetMaxTabsPassiveShrink(t *testing.T) {
	s, _, c := newFakeService(Config{MaxTabs: 2, MaxTabsPerContainer: 5})
	s1 := open(t, s, "sb-a")
	c.add(time.Second)
	s2 := open(t, s, "sb-b")
	if err := s.SetMaxTabs(1); err != nil {
		t.Fatal(err)
	}
	for _, sess := range []*Session{s1, s2} {
		select {
		case <-sess.Done():
			t.Fatalf("session %s closed during passive shrink", sess.ID)
		default:
		}
	}
	st := s.Stats()
	if st.TabCount != 2 || st.MaxTabs != 1 {
		t.Fatalf("after shrink: %+v, want tabCount=2 maxTabs=1", st)
	}
}

func TestStatsSnapshot(t *testing.T) {
	s, _, _ := newFakeService(Config{MaxTabs: 8, MaxTabsPerContainer: 5})
	open(t, s, "sb-a")
	st := s.Stats()
	if st.TabCount != 1 || st.MaxTabs != 8 || st.ContainerCount != 1 {
		t.Fatalf("Stats = %+v", st)
	}
}
