package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

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
		mu.Lock()
		created++
		mu.Unlock()
		return &sandbox.Sandbox{Name: "shared-4"}, "", "", nil
	}
	a, err := c.acquire(context.Background(), "run-4", create)
	if err != nil {
		t.Fatal(err)
	}
	c.release(a.sb)
	if created != 1 {
		t.Fatalf("created=%d", created)
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
