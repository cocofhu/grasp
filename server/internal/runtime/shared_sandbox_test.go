package runtime

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSharedSandboxCoordinatorCreatesOnceAndSerializesLeases(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := func() (*sandbox.Sandbox, string, string, error) {
		created++
		return &sandbox.Sandbox{Name: "shared", WorkspaceDir: "/root/workspace"}, "/tmp/home", "model", nil
	}
	a, err := c.acquire(context.Background(), "run-1", create)
	if err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan error, 1)
	go func() {
		b, err := c.acquire(context.Background(), "run-1", create)
		if err == nil && b != a {
			err = errDifferentEntry
		}
		if err == nil {
			c.release(b.sb)
		}
		secondDone <- err
	}()
	select {
	case <-secondDone:
		t.Fatal("second node acquired a busy shared sandbox")
	case <-time.After(20 * time.Millisecond):
	}
	c.release(a.sb)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("created=%d, want one run-level container", created)
	}
}

var errDifferentEntry = &coordinatorTestError{"different coordinator entry"}

type coordinatorTestError struct{ msg string }

func (e *coordinatorTestError) Error() string { return e.msg }

func TestSharedSandboxCoordinatorFinalizeIsIdempotent(t *testing.T) {
	c := newSharedSandboxCoordinator()
	entry, err := c.acquire(context.Background(), "run-2", func() (*sandbox.Sandbox, string, string, error) {
		return &sandbox.Sandbox{Name: "shared-2"}, "/tmp/home-2", "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c.release(entry.sb)
	got, ok := c.finalize("run-2")
	if !ok || got != entry {
		t.Fatalf("finalize=(%v,%v), want entry", got, ok)
	}
	if _, ok := c.finalize("run-2"); ok {
		t.Fatal("second finalize must be a no-op")
	}
	c.reopen("run-2")
	if _, err := c.acquire(context.Background(), "run-2", func() (*sandbox.Sandbox, string, string, error) {
		return &sandbox.Sandbox{Name: "shared-2-reopened"}, "", "", nil
	}); err != nil {
		t.Fatalf("reopen acquire: %v", err)
	}
}

func TestSharedSandboxCoordinatorWaiterCancellation(t *testing.T) {
	c := newSharedSandboxCoordinator()
	first, err := c.acquire(context.Background(), "run-3", func() (*sandbox.Sandbox, string, string, error) {
		return &sandbox.Sandbox{Name: "shared-3"}, "", "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = c.acquire(ctx, "run-3", func() (*sandbox.Sandbox, string, string, error) {
		t.Fatal("waiter unexpectedly created a second generation")
		return nil, "", "", nil
	})
	if err == nil {
		t.Fatal("expected waiter cancellation")
	}
	c.release(first.sb)
}

func TestSharedSandboxCoordinatorDiscardWakesWaiterForReplacement(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := func() (*sandbox.Sandbox, string, string, error) {
		created++
		return &sandbox.Sandbox{Name: "shared-replace"}, "", "", nil
	}
	first, err := c.acquire(context.Background(), "run-replace", create)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.acquire(context.Background(), "run-replace", create)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	if _, ok := c.discard("run-replace", first.sb); !ok {
		t.Fatal("discard did not detach generation")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("created=%d, want replacement generation", created)
	}
}

func TestSharedSandboxCoordinatorConcurrentCreation(t *testing.T) {
	c := newSharedSandboxCoordinator()
	var mu sync.Mutex
	created := 0
	create := func() (*sandbox.Sandbox, string, string, error) {
		time.Sleep(20 * time.Millisecond) // widen the create window under contention
		mu.Lock()
		created++
		mu.Unlock()
		return &sandbox.Sandbox{Name: "shared-4", WorkspaceDir: "/ws"}, "", "", nil
	}
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	sbs := make(chan *sandbox.Sandbox, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			entry, err := c.acquire(context.Background(), "run-4", create)
			if err != nil {
				errs <- err
				return
			}
			sbs <- entry.sb
			c.release(entry.sb)
		}()
	}
	wg.Wait()
	close(errs)
	close(sbs)
	for err := range errs {
		t.Fatalf("acquire: %v", err)
	}
	var first *sandbox.Sandbox
	for sb := range sbs {
		if first == nil {
			first = sb
			continue
		}
		if sb != first {
			t.Fatal("concurrent acquire must reuse one container")
		}
	}
	if created != 1 {
		t.Fatalf("created=%d, want 1 under concurrent acquire", created)
	}
}

func TestSharedSandboxCoordinatorAllowsNodeEnvironmentSwitch(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := func() (*sandbox.Sandbox, string, string, error) {
		created++
		return &sandbox.Sandbox{Name: "shared-env"}, "", "", nil
	}
	entry, err := c.acquire(context.Background(), "run-env", create)
	if err != nil {
		t.Fatal(err)
	}
	c.release(entry.sb)
	second, err := c.acquire(context.Background(), "run-env", create)
	if err != nil {
		t.Fatal(err)
	}
	defer c.release(second.sb)
	if created != 1 {
		t.Fatalf("created=%d, different node env must reuse one container", created)
	}
	if second.sb != entry.sb {
		t.Fatal("expected same shared sandbox across node env switch")
	}
}

func TestSharedSandboxCoordinatorIsolatesRuns(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := map[string]int{}
	makeCreate := func(runID string) func() (*sandbox.Sandbox, string, string, error) {
		return func() (*sandbox.Sandbox, string, string, error) {
			created[runID]++
			return &sandbox.Sandbox{Name: "sb-" + runID, WorkspaceDir: "/ws/" + runID}, "/home/" + runID, "", nil
		}
	}
	a, err := c.acquire(context.Background(), "run-a", makeCreate("run-a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.acquire(context.Background(), "run-b", makeCreate("run-b"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.release(a.sb)
	defer c.release(b.sb)
	if a.sb == b.sb || a.sb.Name == b.sb.Name {
		t.Fatal("different runs must not share one sandbox")
	}
	if a.sb.WorkspaceDir == b.sb.WorkspaceDir {
		t.Fatal("different runs must not share one workspace")
	}
	if created["run-a"] != 1 || created["run-b"] != 1 {
		t.Fatalf("created=%v", created)
	}
}

func TestSharedSandboxCoordinatorSequentialNodesReuseOne(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := func() (*sandbox.Sandbox, string, string, error) {
		created++
		return &sandbox.Sandbox{Name: "shared-seq", WorkspaceDir: "/ws/shared"}, "/home/shared", "", nil
	}
	var last *sandbox.Sandbox
	for i := 0; i < 3; i++ {
		entry, err := c.acquire(context.Background(), "run-seq", create)
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
		if last != nil && entry.sb != last {
			t.Fatalf("node %d got different sandbox", i)
		}
		last = entry.sb
		c.release(entry.sb)
	}
	if created != 1 {
		t.Fatalf("created=%d, want 1 for three sequential nodes", created)
	}
}


// TestSharedSandboxCoordinatorReleaseKeepsGeneration documents the connect-fail
// cleanup contract: after a successful create, a failed connectSandbox must
// release (not discard) so the next acquire reuses the same generation.
func TestSharedSandboxCoordinatorReleaseKeepsGeneration(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := func() (*sandbox.Sandbox, string, string, error) {
		created++
		return &sandbox.Sandbox{Name: "shared-connect"}, "/tmp/connect-home", "model", nil
	}
	first, err := c.acquire(context.Background(), "run-connect", create)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate openSandbox connectSandbox failure: release lease, keep binding.
	c.release(first.sb)

	second, err := c.acquire(context.Background(), "run-connect", func() (*sandbox.Sandbox, string, string, error) {
		t.Fatal("connect-fail cleanup must not create a replacement generation")
		return nil, "", "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.release(second.sb)
	if second.sb != first.sb {
		t.Fatal("expected same sandbox after release (no discard)")
	}
	if second.generation != first.generation || first.generation != 1 {
		t.Fatalf("generation=%d/%d, want unchanged 1", first.generation, second.generation)
	}
	if created != 1 {
		t.Fatalf("created=%d, want 1", created)
	}
}

// TestSharedSandboxOpenSandboxConnectFailReleasesNotDiscards drives openSandbox
// with a fake bridge that cannot CreateChat (shared path always allocates a
// node chat). Create succeeds once; connect fails; the coordinator must keep
// the generation so a later acquire reuses the same container.
func TestSharedSandboxOpenSandboxConnectFailReleasesNotDiscards(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	runID, nodeID := "run-conn-fail", "node-1"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })

	mgr := newFakeManager(t, host, runID, nodeID, tok, func(int) chatFunc {
		return func(int) turnAction { return turnAction{} }
	})
	opts := testOpts()
	opts.ProfilesRoot = writeAgent(t, "test-agent", `{"acpBackend":"cursor","env":{"GRASP_CURSOR_API_KEY":"fake"}}`)
	p, reg := newTestProvider(t, host, opts, mgr)
	p.coordinator = newSharedSandboxCoordinator()

	req := reqWithProfile(NodeReq{
		RunID: runID, NodeID: nodeID, NodeType: "agent", Caps: testPlainCaps, Token: tok,
		Config: map[string]any{"prompt": "x", "agent_profile": "test-agent"},
		Vars:   map[string]any{},
	})

	_, _, _, err := p.openSandbox(context.Background(), req)
	if err == nil {
		t.Fatal("expected connect/create-chat failure from fake bridge")
	}
	if mgr.createCount() != 1 {
		t.Fatalf("createCount=%d, want 1", mgr.createCount())
	}
	reg.mu.Lock()
	unregistered := reg.unregistered
	reg.mu.Unlock()
	if unregistered != 0 {
		t.Fatalf("connect fail must not Unregister/Destroy, unregistered=%d", unregistered)
	}

	entry, err := p.coordinator.acquire(context.Background(), runID, func() (*sandbox.Sandbox, string, string, error) {
		t.Fatal("must reuse existing shared sandbox after connect failure")
		return nil, "", "", nil
	})
	if err != nil {
		t.Fatalf("re-acquire after connect fail: %v", err)
	}
	defer p.coordinator.release(entry.sb)
	if entry.generation != 1 {
		t.Fatalf("generation=%d, want 1 (no discard)", entry.generation)
	}
	if mgr.createCount() != 1 {
		t.Fatalf("createCount after re-acquire=%d, want 1", mgr.createCount())
	}
}

// TestSharedSandboxCoordinatorFinalizeDuringCreateCleansUp covers the race
// where AbortRun/finalize detaches a still-creating entry (sb==nil). The
// creator must Destroy/removeHome the orphaned result itself.
func TestSharedSandboxCoordinatorFinalizeDuringCreateCleansUp(t *testing.T) {
	c := newSharedSandboxCoordinator()
	home, err := os.MkdirTemp("", "shared-orphan-home-*")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		_, err := c.acquire(context.Background(), "run-orphan", func() (*sandbox.Sandbox, string, string, error) {
			close(started)
			<-finish
			return &sandbox.Sandbox{Name: "orphan-sb"}, home, "", nil
		})
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("create did not start")
	}
	shared, ok := c.finalize("run-orphan")
	if !ok {
		t.Fatal("finalize during create should detach the creating entry")
	}
	if shared.sb != nil {
		t.Fatal("finalize during create should see sb==nil")
	}
	close(finish)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("creator should fail after finalize during create")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("creator did not return")
	}

	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("orphaned ConfigHome should be removed, stat err=%v", err)
	}
	if _, ok := c.finalize("run-orphan"); ok {
		t.Fatal("second finalize must be a no-op")
	}
}
