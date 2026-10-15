package runtime

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// createShared returns a create callback that counts invocations.
func createShared(count *int, sb *sandbox.Sandbox, home string) func() (sharedCreateResult, error) {
	return func() (sharedCreateResult, error) {
		*count++
		return sharedCreateResult{sb: sb, home: home}, nil
	}
}

func mustNotCreate(t *testing.T) func() (sharedCreateResult, error) {
	return func() (sharedCreateResult, error) {
		t.Fatal("unexpected shared sandbox creation")
		return sharedCreateResult{}, nil
	}
}

func TestSharedSandboxCoordinatorCreatesOnceAndSerializesLeases(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := createShared(&created, &sandbox.Sandbox{Name: "shared", WorkspaceDir: "/root/workspace"}, "/tmp/home")
	a, err := c.acquire(context.Background(), "run-1", "", create)
	if err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan error, 1)
	go func() {
		b, err := c.acquire(context.Background(), "run-1", "", create)
		if err == nil && b.sb != a.sb {
			err = errors.New("different coordinator entry")
		}
		if err == nil {
			c.release(b)
		}
		secondDone <- err
	}()
	select {
	case <-secondDone:
		t.Fatal("second node acquired a busy shared sandbox")
	case <-time.After(20 * time.Millisecond):
	}
	c.release(a)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("created=%d, want one run-level container", created)
	}
}

func TestSharedSandboxCoordinatorFinalizeIsIdempotent(t *testing.T) {
	c := newSharedSandboxCoordinator()
	n := 0
	lease, err := c.acquire(context.Background(), "run-2", "", createShared(&n, &sandbox.Sandbox{Name: "shared-2"}, "/tmp/home-2"))
	if err != nil {
		t.Fatal(err)
	}
	c.release(lease)
	got, ok := c.finalize("run-2")
	if !ok || got != lease.sharedSandbox {
		t.Fatalf("finalize=(%v,%v), want entry", got, ok)
	}
	if _, ok := c.finalize("run-2"); ok {
		t.Fatal("second finalize must be a no-op")
	}
	c.reopen("run-2")
	if _, err := c.acquire(context.Background(), "run-2", "", createShared(&n, &sandbox.Sandbox{Name: "shared-2-reopened"}, "")); err != nil {
		t.Fatalf("reopen acquire: %v", err)
	}
}

func TestSharedSandboxCoordinatorWaiterCancellation(t *testing.T) {
	c := newSharedSandboxCoordinator()
	n := 0
	first, err := c.acquire(context.Background(), "run-3", "", createShared(&n, &sandbox.Sandbox{Name: "shared-3"}, ""))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = c.acquire(ctx, "run-3", "", mustNotCreate(t)); err == nil {
		t.Fatal("expected waiter cancellation")
	}
	c.release(first)
}

func TestSharedSandboxCoordinatorDiscardWakesWaiterForReplacement(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := createShared(&created, &sandbox.Sandbox{Name: "shared-replace"}, "")
	first, err := c.acquire(context.Background(), "run-replace", "", create)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		l, err := c.acquire(context.Background(), "run-replace", "", create)
		if err == nil && l.generation != 2 {
			err = errors.New("replacement must be generation 2")
		}
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
	create := func() (sharedCreateResult, error) {
		time.Sleep(20 * time.Millisecond) // widen the create window under contention
		mu.Lock()
		created++
		mu.Unlock()
		return sharedCreateResult{sb: &sandbox.Sandbox{Name: "shared-4", WorkspaceDir: "/ws"}}, nil
	}
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	sbs := make(chan *sandbox.Sandbox, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			lease, err := c.acquire(context.Background(), "run-4", "", create)
			if err != nil {
				errs <- err
				return
			}
			sbs <- lease.sb
			c.release(lease)
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

func TestSharedSandboxCoordinatorIsolatesRuns(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := map[string]int{}
	makeCreate := func(runID string) func() (sharedCreateResult, error) {
		return func() (sharedCreateResult, error) {
			created[runID]++
			return sharedCreateResult{sb: &sandbox.Sandbox{Name: "sb-" + runID, WorkspaceDir: "/ws/" + runID}, home: "/home/" + runID}, nil
		}
	}
	a, err := c.acquire(context.Background(), "run-a", "", makeCreate("run-a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.acquire(context.Background(), "run-b", "", makeCreate("run-b"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.release(a)
	defer c.release(b)
	if a.sb == b.sb || a.sb.WorkspaceDir == b.sb.WorkspaceDir {
		t.Fatal("different runs must not share one sandbox or workspace")
	}
	if created["run-a"] != 1 || created["run-b"] != 1 {
		t.Fatalf("created=%v", created)
	}
}

func TestSharedSandboxCoordinatorSequentialNodesReuseOne(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	create := createShared(&created, &sandbox.Sandbox{Name: "shared-seq", WorkspaceDir: "/ws/shared"}, "/home/shared")
	var last *sandbox.Sandbox
	for i := 0; i < 3; i++ {
		lease, err := c.acquire(context.Background(), "run-seq", "fp", func() (sharedCreateResult, error) {
			res, err := create()
			res.fingerprint = "fp"
			return res, err
		})
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
		if last != nil && lease.sb != last {
			t.Fatalf("node %d got different sandbox", i)
		}
		last = lease.sb
		c.release(lease)
	}
	if created != 1 {
		t.Fatalf("created=%d, want 1 for three sequential nodes", created)
	}
}

// A node whose fingerprint differs must not join (or wait for) the run's
// container, even while another node holds the lease.
func TestSharedSandboxCoordinatorRejectsIncompatibleWithoutWaiting(t *testing.T) {
	c := newSharedSandboxCoordinator()
	n := 0
	holder, err := c.acquire(context.Background(), "run-fp", "fp-a", func() (sharedCreateResult, error) {
		n++
		return sharedCreateResult{sb: &sandbox.Sandbox{Name: "shared-fp"}, fingerprint: "fp-a"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.release(holder)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	if _, err := c.acquire(ctx, "run-fp", "fp-b", mustNotCreate(t)); !errors.Is(err, errSharedSandboxIncompatible) {
		t.Fatalf("err=%v, want errSharedSandboxIncompatible", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("incompatible node waited for the busy lease")
	}
	if n != 1 {
		t.Fatalf("created=%d, want 1", n)
	}
}

// A stale or repeated release must not free a lease held by another node.
func TestSharedSandboxCoordinatorReleaseIsOwnerChecked(t *testing.T) {
	c := newSharedSandboxCoordinator()
	n := 0
	create := createShared(&n, &sandbox.Sandbox{Name: "shared-owner"}, "")
	a, err := c.acquire(context.Background(), "run-owner", "", create)
	if err != nil {
		t.Fatal(err)
	}
	c.release(a)
	b, err := c.acquire(context.Background(), "run-owner", "", create)
	if err != nil {
		t.Fatal(err)
	}
	c.release(a) // stale: a no longer holds the lease
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.acquire(ctx, "run-owner", "", create); err == nil {
		t.Fatal("stale release freed another node's lease")
	}
	c.release(b)
	if _, err := c.acquire(context.Background(), "run-owner", "", create); err != nil {
		t.Fatalf("acquire after owner release: %v", err)
	}
}

// TestSharedSandboxCoordinatorReleaseKeepsGeneration documents the connect-fail
// cleanup contract: after a successful create, a failed connectSandbox must
// release (not discard) so the next acquire reuses the same generation.
func TestSharedSandboxCoordinatorReleaseKeepsGeneration(t *testing.T) {
	c := newSharedSandboxCoordinator()
	created := 0
	first, err := c.acquire(context.Background(), "run-connect", "", createShared(&created, &sandbox.Sandbox{Name: "shared-connect"}, "/tmp/connect-home"))
	if err != nil {
		t.Fatal(err)
	}
	c.release(first)

	second, err := c.acquire(context.Background(), "run-connect", "", mustNotCreate(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.release(second)
	if second.sb != first.sb {
		t.Fatal("expected same sandbox after release (no discard)")
	}
	if second.generation != first.generation || first.generation != 1 {
		t.Fatalf("generation=%d/%d, want unchanged 1", first.generation, second.generation)
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

	if _, _, _, err := p.openSandbox(context.Background(), req); err == nil {
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

	ls, err := p.specLayered(req, true, false)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := p.coordinator.acquire(context.Background(), runID, ls.fingerprint, mustNotCreate(t))
	if err != nil {
		t.Fatalf("re-acquire after connect fail: %v", err)
	}
	defer p.coordinator.release(lease)
	if lease.generation != 1 {
		t.Fatalf("generation=%d, want 1 (no discard)", lease.generation)
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
		_, err := c.acquire(context.Background(), "run-orphan", "", func() (sharedCreateResult, error) {
			close(started)
			<-finish
			return sharedCreateResult{sb: &sandbox.Sandbox{Name: "orphan-sb"}, home: home}, nil
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
