package runtime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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
	// destroyFailed records MarkRunSandboxDestroyFailed as "row->gateway id".
	destroyFailed []string
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

func (r *rowRegistry) MarkRunSandboxDestroyFailed(name, gatewayID string, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.destroyFailed = append(r.destroyFailed, name+"->"+gatewayID)
}

// destroyingManager adds DestroyByName (recording ids, failing while fail is
// set) and attach capabilities that fail, like a gateway that lost the
// container's endpoint.
type destroyingManager struct {
	*fakeManager
	mu        sync.Mutex
	fail      bool
	destroyed []string
}

func (m *destroyingManager) DestroyByName(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.destroyed = append(m.destroyed, id)
	if m.fail {
		return errors.New("gateway unavailable")
	}
	return nil
}

func (m *destroyingManager) Attach(context.Context, string) (*sandbox.Sandbox, error) {
	return nil, errors.New("no session endpoint")
}

func (m *destroyingManager) AttachNoEnsure(context.Context, string) (*sandbox.Sandbox, error) {
	return nil, errors.New("no session endpoint")
}

func (m *destroyingManager) destroyedIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.destroyed...)
}

// AbortRun of a run whose shared container was left by a previous process
// destroys it by name even though Attach fails, and only then drops the row
// and ConfigHome. A failed destroy keeps both and reports the row for retry.
func TestDetachedSharedTeardownDestroysByNameBeforeForgetting(t *testing.T) {
	for _, fail := range []bool{true, false} {
		f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, idleChat)
		p := f.provider(t, BackendCursor)
		dm := &destroyingManager{fakeManager: f.mgr, fail: fail}
		p.mgr = dm
		reg := newRowRegistry()
		home := t.TempDir()
		info := RunSandboxInfo{Name: "gw-old", RunID: f.runID, HomeDir: home, Scope: sandboxScopeRun}
		reg.rows[info.Name] = info
		reg.binding = &info
		p.registry = reg

		p.AbortRun(f.runID)

		if got := dm.destroyedIDs(); len(got) != 1 || got[0] != "gw-old" {
			t.Fatalf("fail=%v: DestroyByName calls %v, want [gw-old]", fail, got)
		}
		_, statErr := os.Stat(home)
		rows := reg.live()
		if fail {
			if len(rows) != 1 || statErr != nil || len(reg.destroyFailed) != 1 {
				t.Fatalf("failed destroy must keep row/home and report retry: rows=%v home=%v retry=%v", rows, statErr, reg.destroyFailed)
			}
			continue
		}
		if len(rows) != 0 || !os.IsNotExist(statErr) {
			t.Fatalf("destroyed container must drop row and home: rows=%v home=%v", rows, statErr)
		}
	}
}

// When the durable shared container cannot be re-attached the provider
// starts a replacement generation; the old container is destroyed by name
// rather than only forgotten.
func TestSharedReattachFailureDestroysOldGeneration(t *testing.T) {
	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, idleChat)
	p := f.provider(t, BackendCursor)
	dm := &destroyingManager{fakeManager: f.mgr}
	p.mgr = dm
	reg := newRowRegistry()
	info := RunSandboxInfo{Name: "gw-gen1", RunID: f.runID, NodeID: "node-a", HomeDir: t.TempDir(), Scope: sandboxScopeRun, Generation: 1}
	reg.rows[info.Name] = info
	reg.binding = &info
	p.registry = reg

	sb, acp, home, err := p.openSandbox(context.Background(), f.req("node-a", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.retireRunSandbox(sb, acp, home)
	if got := dm.destroyedIDs(); len(got) != 1 || got[0] != "gw-gen1" {
		t.Fatalf("old generation not destroyed: %v", got)
	}
	if _, ok := reg.rows["gw-gen1"]; ok {
		t.Fatal("old generation row should be dropped after its destroy")
	}
}
