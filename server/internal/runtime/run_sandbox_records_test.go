package runtime

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// rowRegistry models the platform sandbox rows by name: BeginRunSandbox adds
// a placeholder row, RegisterRunSandbox adopts the run+node's creating row
// (or adds one) and UnregisterRunSandbox drops the row. live() lists rows a
// test expects to be gone.
type rowRegistry struct {
	mu       sync.Mutex
	rows     map[string]RunSandboxInfo
	creating map[string]string // run|node -> placeholder
	binding  *RunSandboxInfo
	retired  []string
}

func newRowRegistry() *rowRegistry {
	return &rowRegistry{rows: map[string]RunSandboxInfo{}, creating: map[string]string{}}
}

func (r *rowRegistry) BeginRunSandbox(info RunSandboxInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[info.Name] = info
	r.creating[info.RunID+"|"+info.NodeID] = info.Name
}

func (r *rowRegistry) RegisterRunSandbox(info RunSandboxInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := info.RunID + "|" + info.NodeID
	if ph, ok := r.creating[key]; ok {
		delete(r.rows, ph)
		delete(r.creating, key)
	}
	r.rows[info.Name] = info
}

func (r *rowRegistry) UnregisterRunSandbox(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, name)
	for k, ph := range r.creating {
		if ph == name {
			delete(r.creating, k)
		}
	}
}

func (r *rowRegistry) RetireRunSandbox(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retired = append(r.retired, name)
}

func (r *rowRegistry) FindRunSandbox(string) (RunSandboxInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.binding == nil {
		return RunSandboxInfo{}, false
	}
	return *r.binding, true
}

func (r *rowRegistry) live() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.rows))
	for name := range r.rows {
		out = append(out, name)
	}
	return out
}

// connectRefusingManager creates sandboxes whose bridge passes the ready probe
// (login + WebSocket upgrade) but drops the socket on the ACP connect frame.
type connectRefusingManager struct{ t *testing.T }

func (m connectRefusingManager) Create(context.Context, sandbox.Spec) (*sandbox.Sandbox, error) {
	mux := http.NewServeMux()
	handleTestBridgeLogin(mux)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := fakeUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_, _, _ = conn.ReadMessage()
		_ = conn.Close()
	})
	srv := httptest.NewServer(mux)
	m.t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	return &sandbox.Sandbox{Name: "refusing-sb", Host: "127.0.0.1", Port: port, WorkspaceDir: "/root/workspace", Password: testBridgePassword}, nil
}

// A node-private sandbox whose ACP connect fails must drop the platform row it
// began under the placeholder name; the row is never adopted by the real name.
func TestOpenPrivateSandboxConnectFailureDropsPlaceholderRow(t *testing.T) {
	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, idleChat)
	p := f.provider(t, BackendCursor)
	p.coordinator = nil
	p.mgr = connectRefusingManager{t: t}
	reg := newRowRegistry()
	p.registry = reg

	if _, _, _, err := p.openSandbox(context.Background(), f.req("node-a", "dev")); err == nil {
		t.Fatal("connect against a refusing bridge should fail")
	}
	if rows := reg.live(); len(rows) != 0 {
		t.Fatalf("placeholder row leaked after connect failure: %v", rows)
	}
}

// blockingManager holds Create until release is closed.
type blockingManager struct {
	*fakeManager
	entered chan struct{}
	release chan struct{}
}

func (m *blockingManager) Create(ctx context.Context, spec sandbox.Spec) (*sandbox.Sandbox, error) {
	close(m.entered)
	<-m.release
	return m.fakeManager.Create(ctx, spec)
}

// AbortRun finalizing the run while the shared container is still being
// created orphans the result: the creator destroys it and must also drop the
// placeholder row it began, so nothing is left in the sandbox list.
func TestSharedCreateOrphanedByAbortDropsPlaceholderRow(t *testing.T) {
	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, idleChat)
	p := f.provider(t, BackendCursor)
	bm := &blockingManager{fakeManager: f.mgr, entered: make(chan struct{}), release: make(chan struct{})}
	p.mgr = bm
	reg := newRowRegistry()
	p.registry = reg

	done := make(chan error, 1)
	go func() {
		_, _, _, err := p.openSandbox(context.Background(), f.req("node-a", "dev"))
		done <- err
	}()
	select {
	case <-bm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not start")
	}
	if _, ok := f.coord.finalize(f.runID); !ok {
		t.Fatal("finalize should detach the creating entry")
	}
	close(bm.release)
	if err := <-done; err == nil {
		t.Fatal("open must fail once the run is finalized")
	}
	if rows := reg.live(); len(rows) != 0 {
		t.Fatalf("orphaned shared create left rows behind: %v", rows)
	}
}
