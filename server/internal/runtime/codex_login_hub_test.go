package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/codexoauth"
)

type fakeCodexFile struct {
	mu      sync.Mutex
	body    string
	writes  int
	fail    bool
	readErr error
}

func (f *fakeCodexFile) WriteFileAtomic(_ context.Context, _ string, content []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("sandbox gone")
	}
	f.body = string(content)
	f.writes++
	return nil
}

func (f *fakeCodexFile) ReadFile(context.Context, string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	return []byte(f.body), nil
}

func (f *fakeCodexFile) state() (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body, f.writes
}

func (f *fakeCodexFile) setFail(v bool) {
	f.mu.Lock()
	f.fail = v
	f.mu.Unlock()
}

// fakeCodexSource hands out numbered copies; ForceRefresh mints a new one
// unless the stale token is already outdated.
type fakeCodexSource struct {
	mu        sync.Mutex
	gen       int
	ensures   int
	refreshes int
	stales    []string
	err       error
	hub       *CodexLoginHub
}

func codexCopy(gen int) string {
	return fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":"at-%d","refresh_token":""}}`, gen)
}

func (s *fakeCodexSource) EnsureFresh(context.Context, string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensures++
	if s.err != nil {
		return "", "", s.err
	}
	c := codexCopy(s.gen)
	return c, CodexCopyVersion(c), nil
}

func (s *fakeCodexSource) ForceRefresh(_ context.Context, credID, stale string) (string, string, error) {
	s.mu.Lock()
	s.stales = append(s.stales, stale)
	if s.err != nil {
		s.mu.Unlock()
		return "", "", s.err
	}
	if stale == fmt.Sprintf("at-%d", s.gen) {
		s.gen++
		s.refreshes++
	}
	c := codexCopy(s.gen)
	hub := s.hub
	s.mu.Unlock()
	if hub != nil {
		hub.Broadcast(credID, c, CodexCopyVersion(c))
	}
	return c, CodexCopyVersion(c), nil
}

func TestCodexLoginHubNilIsInert(t *testing.T) {
	var h *CodexLoginHub
	out, version, err := h.Prepare(context.Background(), "cred", chatgptLogin)
	if err != nil || strings.Contains(out, `"rt"`) || version != CodexCopyVersion(out) {
		t.Fatalf("nil hub must strip the refresh token: %q %v", out, err)
	}
	h.Register("cred", "k", nil, "/p", "v")()
	h.Bind("k", &fakeCodexFile{})
	h.Unregister("k")
	h.Broadcast("cred", "x", "v")
	h.Forget("cred")
	h.SetSource(nil)
	if v, a := h.Snapshot("k"); v != "" || a != "" {
		t.Fatal("nil hub snapshot")
	}
	if h.Registered("k") || h.Recover(context.Background(), "k", "", "") || h.BeforeTurn(context.Background(), "k") != nil {
		t.Fatal("nil hub has no registrations")
	}
	if got := CodexSandboxCopy("not json"); got != "not json" {
		t.Fatalf("non JSON kept as-is: %q", got)
	}
}

func TestCodexLoginHubPrepareUsesSource(t *testing.T) {
	src := &fakeCodexSource{}
	h := NewCodexLoginHub(nil)
	h.SetSource(src)
	out, version, err := h.Prepare(context.Background(), " cred ", chatgptLogin)
	if err != nil || out != codexCopy(0) || version != CodexCopyVersion(out) || src.ensures != 1 {
		t.Fatalf("prepare %q %q %v", out, version, err)
	}
	if out, _, _ := h.Prepare(context.Background(), "", chatgptLogin); strings.Contains(out, `"rt"`) {
		t.Fatal("no credential: local strip")
	}
	src.err = errors.New("needs relogin")
	if _, _, err := h.Prepare(context.Background(), "cred", chatgptLogin); err == nil {
		t.Fatal("source error must surface")
	}
}

func TestCodexLoginHubBroadcastAndBeforeTurn(t *testing.T) {
	src := &fakeCodexSource{}
	h := NewCodexLoginHub(src)
	c0 := codexCopy(0)
	v0 := CodexCopyVersion(c0)
	if _, _, err := h.Prepare(context.Background(), "cred", ""); err != nil {
		t.Fatal(err)
	}
	files := make([]*fakeCodexFile, 3)
	for i := range files {
		files[i] = &fakeCodexFile{body: c0}
		h.Register("cred", fmt.Sprintf("k%d", i), files[i], "/root/.codex/auth.json", v0)
	}
	pending := h.Register("cred", "pending", nil, "/root/.codex/auth.json", v0)
	other := &fakeCodexFile{}
	h.Register("other", "o", other, "/p", "")

	// Up to date: no write before the turn.
	if err := h.BeforeTurn(context.Background(), "k0"); err != nil {
		t.Fatal(err)
	}
	if _, n := files[0].state(); n != 0 {
		t.Fatal("up-to-date sandbox must not be written")
	}

	files[2].setFail(true)
	c1 := codexCopy(1)
	h.Broadcast("cred", c1, CodexCopyVersion(c1))
	for i, f := range files[:2] {
		if body, n := f.state(); body != c1 || n != 1 {
			t.Fatalf("sandbox %d: %q %d", i, body, n)
		}
	}
	if _, n := other.state(); n != 0 {
		t.Fatal("other credential written")
	}
	// The failed push is repaired before the next turn.
	files[2].setFail(false)
	if err := h.BeforeTurn(context.Background(), "k2"); err != nil {
		t.Fatal(err)
	}
	if body, n := files[2].state(); body != c1 || n != 1 {
		t.Fatalf("repair: %q %d", body, n)
	}
	// A pending registration picks the copy up once bound.
	late := &fakeCodexFile{}
	h.Bind("pending", late)
	h.Bind("missing", late)
	h.Bind("pending", nil)
	if err := h.BeforeTurn(context.Background(), "pending"); err != nil {
		t.Fatal(err)
	}
	if body, _ := late.state(); body != c1 {
		t.Fatalf("pending: %q", body)
	}
	pending()
	if h.Registered("pending") {
		t.Fatal("unregister func")
	}
	// A replaced registration is not removed by the old unregister func.
	old := h.Register("cred", "dup", &fakeCodexFile{}, "/p", "")
	h.Register("cred", "dup", &fakeCodexFile{}, "/p", "")
	old()
	if !h.Registered("dup") {
		t.Fatal("old unregister removed the replacement")
	}
	h.Unregister("dup")
	if h.Registered("dup") || h.BeforeTurn(context.Background(), "dup") != nil {
		t.Fatal("unregistered key")
	}
	h.Broadcast("", "x", "v")
	if h.Register("", "k", nil, "/p", "") == nil {
		t.Fatal("unregister func must not be nil")
	}
}

func TestCodexLoginHubBeforeTurnFallsBackToSource(t *testing.T) {
	src := &fakeCodexSource{}
	h := NewCodexLoginHub(src)
	f := &fakeCodexFile{}
	h.Register("cred", "k", f, "/p", "")
	if err := h.BeforeTurn(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	if body, _ := f.state(); body != codexCopy(0) || src.ensures != 1 {
		t.Fatalf("fallback: %q ensures=%d", body, src.ensures)
	}
	if err := h.BeforeTurn(context.Background(), "k"); err != nil || src.ensures != 1 {
		t.Fatal("cached copy must be reused")
	}
	h.Forget("cred")
	src.err = errors.New("offline")
	if err := h.BeforeTurn(context.Background(), "k"); err == nil {
		t.Fatal("source error must be reported")
	}
	noSource := NewCodexLoginHub(nil)
	noSource.Register("cred", "k", f, "/p", "")
	if err := noSource.BeforeTurn(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexLoginHubRecover(t *testing.T) {
	src := &fakeCodexSource{}
	h := NewCodexLoginHub(src)
	src.hub = h
	c0 := codexCopy(0)
	if _, _, err := h.Prepare(context.Background(), "cred", ""); err != nil {
		t.Fatal(err)
	}
	f := &fakeCodexFile{body: c0}
	h.Register("cred", "k", f, "/p", CodexCopyVersion(c0))
	v, a := h.Snapshot("k")
	if v != CodexCopyVersion(c0) || a != "at-0" {
		t.Fatalf("snapshot %q %q", v, a)
	}
	if !h.Recover(context.Background(), "k", v, a) {
		t.Fatal("renewed login must allow a retry")
	}
	if body, _ := f.state(); body != codexCopy(1) || src.stales[0] != "at-0" {
		t.Fatalf("recover: %q stales=%v", body, src.stales)
	}
	// A copy that arrived during the turn is retried without a refresh.
	if !h.Recover(context.Background(), "k", v, a) || len(src.stales) != 1 {
		t.Fatalf("newer copy must retry without refresh: %v", src.stales)
	}
	// Unknown version: the stale token is read from the sandbox.
	f2 := &fakeCodexFile{body: codexCopy(1)}
	h.Register("cred", "k2", f2, "/p", "")
	if !h.Recover(context.Background(), "k2", "", "") || src.stales[1] != "at-1" {
		t.Fatalf("stale from file: %v", src.stales)
	}
	// Read failure: the source is asked without a stale token.
	f3 := &fakeCodexFile{readErr: errors.New("gone")}
	h.Register("cred", "k3", f3, "/p", "")
	h.Recover(context.Background(), "k3", "", "")
	if src.stales[2] != "" {
		t.Fatalf("stale %v", src.stales)
	}
	v, a = h.Snapshot("k")
	src.err = errors.New("refresh_token_reused")
	if h.Recover(context.Background(), "k", v, a) {
		t.Fatal("terminal refresh failure must not retry")
	}
	src.err = nil
	f.setFail(true)
	if h.Recover(context.Background(), "k", v, a) {
		t.Fatal("a failed install must not retry")
	}
	if h.Recover(context.Background(), "missing", "", "") {
		t.Fatal("unregistered key")
	}
}

func TestCodexLoginHubConcurrentRecoverRefreshesOnce(t *testing.T) {
	src := &fakeCodexSource{}
	h := NewCodexLoginHub(src)
	src.hub = h
	c0 := codexCopy(0)
	if _, _, err := h.Prepare(context.Background(), "cred", ""); err != nil {
		t.Fatal(err)
	}
	files := make([]*fakeCodexFile, 5)
	for i := range files {
		files[i] = &fakeCodexFile{body: c0}
		h.Register("cred", fmt.Sprintf("k%d", i), files[i], "/p", CodexCopyVersion(c0))
	}
	type snap struct{ v, a string }
	seen := make([]snap, len(files))
	for i := range files {
		seen[i].v, seen[i].a = h.Snapshot(fmt.Sprintf("k%d", i))
	}
	var wg sync.WaitGroup
	retried := make([]bool, len(files))
	for i := range files {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			retried[i] = h.Recover(context.Background(), fmt.Sprintf("k%d", i), seen[i].v, seen[i].a)
		}(i)
	}
	wg.Wait()
	if src.refreshes != 1 {
		t.Fatalf("refreshes=%d, want 1", src.refreshes)
	}
	for i, f := range files {
		if body, _ := f.state(); body != codexCopy(1) || !retried[i] {
			t.Fatalf("sandbox %d: %q retried=%v", i, body, retried[i])
		}
	}
}

func TestCodexCopyAccess(t *testing.T) {
	if codexCopyAccess("x") != "" {
		t.Fatal("non JSON")
	}
	a := codexoauth.NewAuthFile(codexoauth.Tokens{AccessToken: "tok"}, time.Now())
	body, _ := a.Encode()
	if codexCopyAccess(body) != "tok" {
		t.Fatal("access token")
	}
}
