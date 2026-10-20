package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// A node waiting for another node's shared create must not fail with that
// creator's (transient) error: it retries the create itself.
func TestSharedCreateFailureLetsWaiterCreate(t *testing.T) {
	waiting := make(chan struct{})
	sharedAcquireWaitHook = func(string) { close(waiting) }
	defer func() { sharedAcquireWaitHook = nil }()

	coord := newSharedSandboxCoordinator()
	release := make(chan struct{})
	creatorErr := make(chan error, 1)
	go func() {
		_, err := coord.acquire(context.Background(), "runY", "", func() (sharedCreateResult, error) {
			<-release
			return sharedCreateResult{}, context.DeadlineExceeded
		})
		creatorErr <- err
	}()
	for {
		coord.mu.Lock()
		_, ok := coord.runs["runY"]
		coord.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	type result struct {
		lease *sharedLease
		err   error
	}
	waiter := make(chan result, 1)
	go func() {
		l, err := coord.acquire(context.Background(), "runY", "", func() (sharedCreateResult, error) {
			return sharedCreateResult{sb: &sandbox.Sandbox{Name: "second"}}, nil
		})
		waiter <- result{l, err}
	}()
	<-waiting
	close(release)
	if err := <-creatorErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("creator error = %v, want its own create error", err)
	}
	r := <-waiter
	if r.err != nil {
		t.Fatalf("waiter failed because the creator failed: %v", r.err)
	}
	if r.lease == nil || r.lease.sb == nil || r.lease.sb.Name != "second" {
		t.Fatalf("waiter did not create the container itself: %+v", r.lease)
	}
}

// After the run is finalized a waiter still fails instead of re-creating.
func TestSharedCreateWaiterAfterAbortDoesNotRecreate(t *testing.T) {
	waiting := make(chan struct{})
	sharedAcquireWaitHook = func(string) { close(waiting) }
	defer func() { sharedAcquireWaitHook = nil }()

	coord := newSharedSandboxCoordinator()
	release := make(chan struct{})
	go func() {
		_, _ = coord.acquire(context.Background(), "runZ", "", func() (sharedCreateResult, error) {
			<-release
			return sharedCreateResult{}, context.DeadlineExceeded
		})
	}()
	for {
		coord.mu.Lock()
		_, ok := coord.runs["runZ"]
		coord.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	created := false
	waiter := make(chan error, 1)
	go func() {
		_, err := coord.acquire(context.Background(), "runZ", "", func() (sharedCreateResult, error) {
			created = true
			return sharedCreateResult{sb: &sandbox.Sandbox{Name: "late"}}, nil
		})
		waiter <- err
	}()
	<-waiting
	coord.finalize("runZ")
	close(release)
	if err := <-waiter; err == nil || created {
		t.Fatalf("waiter re-created after finalize: err=%v created=%v", err, created)
	}
}
