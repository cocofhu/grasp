package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// Return a real gateway-backed handle for each fake ACP process, so an old
// inline Sandbox.Destroy would issue an observable DELETE and remove its only
// file copy. The Retirer is the same capability wired by server main.
type abortRetentionManager struct {
	*fakeManager
	native  *sandbox.Manager
	mu      sync.Mutex
	records map[string]sandbox.GWSandbox
	homes   []string
	files   string
	deletes atomic.Int32
}

func retainAbortManager(t *testing.T, base *fakeManager) *abortRetentionManager {
	t.Helper()
	m := &abortRetentionManager{fakeManager: base, records: map[string]sandbox.GWSandbox{}, files: t.TempDir()}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/sandboxes/")
		if r.Method == http.MethodDelete {
			m.deletes.Add(1)
			_ = os.Remove(filepath.Join(m.files, name))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		m.mu.Lock()
		record, ok := m.records[name]
		m.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(record)
	}))
	t.Cleanup(gateway.Close)
	m.native = sandbox.NewManager(sandbox.NewGatewayClient(gateway.URL, ""), sandbox.ManagerOptions{})
	t.Cleanup(func() {
		m.mu.Lock()
		homes := append([]string(nil), m.homes...)
		m.mu.Unlock()
		for _, home := range homes {
			removeHome(home)
		}
	})
	return m
}

func (m *abortRetentionManager) Create(ctx context.Context, spec sandbox.Spec) (*sandbox.Sandbox, error) {
	handle, err := m.fakeManager.Create(ctx, spec)
	if err != nil {
		return handle, err
	}
	if err = os.WriteFile(filepath.Join(m.files, handle.Name), []byte("original dirty and untracked file bytes"), 0600); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.records[handle.Name] = sandbox.GWSandbox{ID: handle.Name, Status: "running", Endpoints: map[string]string{"session": fmt.Sprintf("%s:%d", handle.Host, handle.Port)}}
	m.homes = append(m.homes, spec.ConfigHome)
	m.mu.Unlock()
	actual, err := m.native.AttachNoEnsure(ctx, handle.Name)
	if err != nil {
		return nil, err
	}
	actual.Password = handle.Password
	return actual, nil
}

func assertAbortWorkspaceRetained(t *testing.T, m *abortRetentionManager, reg *retiringRegistry) {
	t.Helper()
	reg.mu.Lock()
	retired, unregistered := reg.retired, reg.unregistered
	reg.mu.Unlock()
	if retired < 1 || unregistered != 0 || m.deletes.Load() != 0 {
		t.Fatalf("cancel/failure bypassed retention: retired=%d unregister=%d DELETE=%d", retired, unregistered, m.deletes.Load())
	}
	body, err := os.ReadFile(filepath.Join(m.files, "fake-sb-0"))
	if err != nil || string(body) != "original dirty and untracked file bytes" {
		t.Fatalf("unique workspace file lost: %q %v", body, err)
	}
	m.mu.Lock()
	homes := append([]string(nil), m.homes...)
	m.mu.Unlock()
	for _, home := range homes {
		if _, err = os.Stat(home); err != nil {
			t.Fatalf("original config/worktree home removed: %v", err)
		}
	}
}

func TestRunAbortOrdinaryInflightRetainsOriginalWorkspace(t *testing.T) {
	p, _, _, base, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{stall: true, ackCancel: true} }
	})
	manager := retainAbortManager(t, base)
	p.mgr = manager
	registry := &retiringRegistry{}
	p.registry = registry
	p.opts.SandboxMaxAttempts = 4
	req.Config["produces"] = ""
	done := make(chan error, 1)
	go func() { _, err := p.RunAgent(context.Background(), req); done <- err }()
	waitFirstPromptOn(t, base, 0)
	deadline := time.Now().Add(time.Second)
	for {
		base.bridge(0).mu.Lock()
		pending := base.bridge(0).stalledOp
		base.bridge(0).mu.Unlock()
		if pending != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native turn did not become cancellable")
		}
		time.Sleep(time.Millisecond)
	}
	// Another task's cancellation cannot revoke or retire this workspace.
	p.AbortRun("unrelated-run")
	registry.mu.Lock()
	retired := registry.retired
	registry.mu.Unlock()
	if retired != 0 {
		t.Fatal("other run retired this active workspace")
	}
	var aborts sync.WaitGroup
	for range 16 {
		aborts.Add(1)
		go func() { defer aborts.Done(); p.AbortRun(req.RunID) }()
	}
	aborts.Wait()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("aborted native request reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retained ACP connection did not cancel the in-flight request")
	}
	if manager.createCount() != 1 || base.bridge(0).promptAt(1) != "" {
		t.Fatal("abort replayed in a replacement workspace")
	}
	assertAbortWorkspaceRetained(t, manager, registry)
	deadline = time.Now().Add(time.Second)
	for {
		base.bridge(0).mu.Lock()
		pending := base.bridge(0).stalledOp
		base.bridge(0).mu.Unlock()
		if pending == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Abort closed the socket without delivering native cancel")
		}
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	live, driving, parked, watches := len(p.live), len(p.inflightACP), len(p.sessions), len(p.runAborts)
	p.mu.Unlock()
	if live != 0 || driving != 0 || parked != 0 || watches != 0 {
		t.Fatalf("aborted driving state leaked: %d/%d/%d/%d", live, driving, parked, watches)
	}
	// Repeated cancellation is observational once the original driving state is gone.
	registry.mu.Lock()
	before := registry.retired
	registry.mu.Unlock()
	p.AbortRun(req.RunID)
	registry.mu.Lock()
	after := registry.retired
	registry.mu.Unlock()
	if after != before {
		t.Fatal("later abort retired an already removed session again")
	}
}

func TestRunFinishAndFailureUseSameRetainedWorkspaceBoundary(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint("failed-", failure), func(t *testing.T) {
			p, _, _, base, _, req := setupProvider(t, func(int) chatFunc {
				return func(int) turnAction {
					if failure {
						return turnAction{sendError: "synthetic nonretryable agent failure"}
					}
					return turnAction{narration: "completed"}
				}
			})
			manager := retainAbortManager(t, base)
			p.mgr = manager
			registry := &retiringRegistry{}
			p.registry = registry
			req.Config["produces"] = ""
			_, err := p.RunAgent(t.Context(), req)
			if (err != nil) != failure {
				t.Fatalf("unexpected final result: %v", err)
			}
			p.AbortRun(req.RunID)
			assertAbortWorkspaceRetained(t, manager, registry)
			if manager.createCount() != 1 {
				t.Fatal("terminal result created another workspace")
			}
		})
	}
}

func TestRunAbortRacingNaturalCompletionKeepsOriginalWorkspace(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	p, _, _, base, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { <-release; return turnAction{narration: "completed concurrently"} }
	})
	manager := retainAbortManager(t, base)
	p.mgr = manager
	registry := &retiringRegistry{}
	p.registry = registry
	req.Config["produces"] = ""
	done := make(chan error, 1)
	go func() { _, err := p.RunAgent(context.Background(), req); done <- err }()
	waitFirstPromptOn(t, base, 0)
	var both sync.WaitGroup
	both.Add(2)
	go func() { defer both.Done(); p.AbortRun(req.RunID) }()
	go func() { defer both.Done(); once.Do(func() { close(release) }) }()
	both.Wait()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("abort/finish race leaked a native request")
	}
	assertAbortWorkspaceRetained(t, manager, registry)
	if manager.createCount() != 1 {
		t.Fatal("terminal race replaced original environment")
	}
}
