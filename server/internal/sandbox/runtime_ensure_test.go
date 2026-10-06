package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeExit int

func (e fakeExit) Error() string   { return "exit " + strings.Repeat("x", int(e)%3) }
func (e fakeExit) ExitStatus() int { return int(e) }

// runtimeSSH answers the runtime contract commands; replies maps a command
// keyword to (output, error).
type runtimeSSH struct {
	mu      sync.Mutex
	calls   []string
	stdin   []byte
	replies map[string]func() ([]byte, error)
}

func (f *runtimeSSH) hook(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, key := range []string{"install-stdin", "version", "restart preview-inject", "restart backend"} {
		if !strings.Contains(strings.ReplaceAll(command, "'", ""), key) {
			continue
		}
		f.calls = append(f.calls, key)
		if stdin != nil {
			f.stdin, _ = io.ReadAll(stdin)
		}
		if r := f.replies[key]; r != nil {
			return r()
		}
		return nil, nil
	}
	f.calls = append(f.calls, "unexpected:"+command)
	return nil, errors.New("unexpected command")
}

func (f *runtimeSSH) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func reply(out string, err error) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(out), err }
}

func newRuntimeManager(t *testing.T) (*Manager, []byte, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rt.tgz")
	v := testVersion('e')
	data := writeRuntime(t, path, v, 1)
	return &Manager{runtime: NewRuntimeBundle(path)}, data, v
}

func installSSH(t *testing.T, f *runtimeSSH) {
	t.Helper()
	t.Cleanup(SetExecHook(f.hook))
}

func joined(s []string) string { return strings.Join(s, ",") }

func TestEnsureRuntimeUpdatesAndRestarts(t *testing.T) {
	m, data, v := newRuntimeManager(t)
	f := &runtimeSSH{replies: map[string]func() ([]byte, error){"version": reply("old\n", nil)}}
	installSSH(t, f)
	sb := &Sandbox{ID: "sb1", SSHHost: "h", SSHPort: 22}

	if err := m.EnsureRuntime(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	if got := joined(f.take()); got != "version,install-stdin,restart preview-inject,restart backend" {
		t.Fatalf("calls=%s", got)
	}
	if !bytes.Equal(f.stdin, data) {
		t.Fatal("install-stdin did not receive the bundle")
	}
	if !m.runtime.upToDate("sb1", v) {
		t.Fatal("sandbox should be up to date")
	}
	if err := m.EnsureRuntime(context.Background(), sb); err != nil || len(f.take()) != 0 {
		t.Fatal("up-to-date sandbox must not be touched")
	}
}

func TestEnsureRuntimeAlreadyInstalled(t *testing.T) {
	m, _, v := newRuntimeManager(t)
	f := &runtimeSSH{replies: map[string]func() ([]byte, error){"version": reply(v+"\n", nil)}}
	installSSH(t, f)
	if err := m.EnsureRuntime(context.Background(), &Sandbox{ID: "sb", SSHHost: "h"}); err != nil {
		t.Fatal(err)
	}
	if got := joined(f.take()); got != "version" {
		t.Fatalf("calls=%s", got)
	}
}

func TestEnsureRuntimeBackendBusyDefers(t *testing.T) {
	m, _, v := newRuntimeManager(t)
	busy := true
	f := &runtimeSSH{replies: map[string]func() ([]byte, error){
		"version": reply("", nil),
		"restart backend": func() ([]byte, error) {
			if busy {
				return []byte("busy"), fakeExit(runtimeExitBusy)
			}
			return nil, nil
		},
	}}
	installSSH(t, f)
	sb := &Sandbox{ID: "sb", SSHHost: "h"}
	if err := m.EnsureRuntime(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	f.take()
	if m.runtime.upToDate("sb", v) {
		t.Fatal("busy backend keeps the update pending")
	}
	busy = false
	if err := m.EnsureRuntime(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	if got := joined(f.take()); got != "restart backend" {
		t.Fatalf("pending retry should only restart backend, calls=%s", got)
	}
	if !m.runtime.upToDate("sb", v) {
		t.Fatal("should be up to date after restart")
	}
}

func TestEnsureRuntimeSkips(t *testing.T) {
	for name, tc := range map[string]struct {
		replies map[string]func() ([]byte, error)
		calls   string
	}{
		"image too old": {
			replies: map[string]func() ([]byte, error){
				"version":       reply("", nil),
				"install-stdin": reply("needs image 2", fakeExit(runtimeExitImageTooOld)),
			},
			calls: "version,install-stdin",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, _, v := newRuntimeManager(t)
			f := &runtimeSSH{replies: tc.replies}
			installSSH(t, f)
			sb := &Sandbox{ID: "sb", SSHHost: "h"}
			if err := m.EnsureRuntime(context.Background(), sb); err != nil {
				t.Fatal(err)
			}
			if got := joined(f.take()); got != tc.calls {
				t.Fatalf("calls=%s", got)
			}
			if !m.runtime.upToDate("sb", v) {
				t.Fatal("skipped sandbox counts as settled")
			}
			if err := m.EnsureRuntime(context.Background(), sb); err != nil || len(f.take()) != 0 {
				t.Fatal("skipped sandbox must not be retried")
			}
		})
	}
}

func TestEnsureRuntimeErrors(t *testing.T) {
	m, _, v := newRuntimeManager(t)
	f := &runtimeSSH{replies: map[string]func() ([]byte, error){
		"version":                reply("", nil),
		"install-stdin":          reply("disk full", fakeExit(1)),
		"restart preview-inject": reply("nope", fakeExit(1)),
	}}
	installSSH(t, f)
	sb := &Sandbox{ID: "sb", SSHHost: "h"}
	if err := m.EnsureRuntime(context.Background(), sb); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("install error=%v", err)
	}
	if m.runtime.upToDate("sb", v) {
		t.Fatal("failed install must be retried")
	}

	// preview-inject restart failure is logged only; backend failure is returned.
	f.replies["install-stdin"] = reply("", nil)
	f.replies["restart backend"] = reply("boom", fakeExit(1))
	f.take()
	if err := m.EnsureRuntime(context.Background(), sb); err == nil || !strings.Contains(err.Error(), "restart backend") {
		t.Fatalf("backend error=%v", err)
	}
	if got := joined(f.take()); got != "version,install-stdin,restart preview-inject,restart backend" {
		t.Fatalf("calls=%s", got)
	}
	f.replies["restart backend"] = nil
	if err := m.EnsureRuntime(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	if got := joined(f.take()); got != "restart backend" {
		t.Fatalf("calls=%s", got)
	}
}

func TestEnsureRuntimeGuards(t *testing.T) {
	var nilMgr *Manager
	if err := nilMgr.EnsureRuntime(context.Background(), &Sandbox{ID: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := (&Manager{}).EnsureRuntime(context.Background(), &Sandbox{ID: "x"}); err != nil {
		t.Fatal(err)
	}
	broken := &Manager{runtime: NewRuntimeBundle(filepath.Join(t.TempDir(), "none"))}
	if err := broken.EnsureRuntime(context.Background(), &Sandbox{ID: "x"}); err == nil {
		t.Fatal("missing bundle should error")
	}
	broken.ensureRuntimeAsync(&Sandbox{ID: "x"})
	(&Manager{}).ensureRuntimeAsync(&Sandbox{ID: "x"})

	m, _, _ := newRuntimeManager(t)
	f := &runtimeSSH{}
	installSSH(t, f)
	m.runtime.begin("busy")
	if err := m.EnsureRuntime(context.Background(), &Sandbox{ID: "busy", SSHHost: "h"}); err != nil || len(f.take()) != 0 {
		t.Fatal("in-flight sandbox must be left alone")
	}

	if code, ok := exitStatus(errors.New("plain")); ok || code != 0 {
		t.Fatal("plain error has no exit status")
	}
	if shortVersion("abc") != "abc" || shortVersion(testVersion('f')) != strings.Repeat("f", 12) {
		t.Fatal("shortVersion")
	}
}

func TestAttachTriggersRuntimeUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rt.tgz")
	v := testVersion('9')
	writeRuntime(t, path, v, 1)
	gw, fg := newInlineGW(t)
	fg.seed("sb-attach", "running")
	m := NewManager(gw, ManagerOptions{Image: "img:test", Runtime: NewRuntimeBundle(path)})

	done := make(chan struct{})
	var once sync.Once
	f := &runtimeSSH{replies: map[string]func() ([]byte, error){
		"version": reply(v, nil),
	}}
	t.Cleanup(SetExecHook(func(ctx context.Context, host string, port int, command string, stdin io.Reader) ([]byte, error) {
		defer once.Do(func() { close(done) })
		return f.hook(ctx, host, port, command, stdin)
	}))
	if _, err := m.Attach(context.Background(), "sb-attach"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Attach did not trigger EnsureRuntime")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !m.runtime.upToDate("sb-attach", v) {
		if time.Now().After(deadline) {
			t.Fatal("runtime state not recorded")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := m.Attach(context.Background(), "sb-attach"); err != nil {
		t.Fatal(err)
	}
	if got := joined(f.take()); got != "version" {
		t.Fatalf("second Attach must not re-run, calls=%s", got)
	}
}
