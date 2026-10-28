package chatsession

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryAdmissionLeaseKeepsOnePumpForConcurrentSenders(t *testing.T) {
	var registry Registry[*Session[string]]
	var creations atomic.Int32
	var running atomic.Int32
	var overlap atomic.Bool
	started := make(chan string, 2)
	finishA, finishB := make(chan struct{}), make(chan struct{})
	idle := make(chan struct{}, 1)
	create := func() *Session[string] {
		creations.Add(1)
		return New(Config[string]{
			View: func(id string) ItemView { return ItemView{ID: id} },
			Publish: func(string, map[string]any) {
				// Publishing may re-enter the registry; admission must not hold
				// its lock across Session.Enqueue or the provider callback.
				_, _ = registry.Get("chat")
			},
			Execute: func(_ context.Context, id string) (bool, error) {
				if running.Add(1) != 1 {
					overlap.Store(true)
				}
				started <- id
				if id == "a" {
					<-finishA
				} else {
					<-finishB
				}
				running.Add(-1)
				return false, nil
			},
			OnIdle: func() { registry.DropIfIdle("chat"); idle <- struct{}{} },
		})
	}
	first, releaseFirst := registry.AcquireOrCreate("chat", create)
	// Simulate the previous pump's cleanup between lookup and enqueue.
	registry.DropIfIdle("chat")
	second, releaseSecond := registry.AcquireOrCreate("chat", create)
	if first != second || creations.Load() != 1 {
		t.Fatal("idle cleanup detached a session with pending admission")
	}
	_, _ = first.Enqueue("a", nil)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn never started")
	}
	releaseFirst()
	releaseFirst() // Lease release is idempotent.
	_, _ = second.Enqueue("b", nil)
	releaseSecond()
	close(finishA)
	select {
	case id := <-started:
		if id != "b" {
			t.Fatalf("unexpected turn %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second turn never started")
	}
	close(finishB)
	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatal("session never became idle")
	}
	if overlap.Load() {
		t.Fatal("two pumps ran the same chat concurrently")
	}
	if _, ok := registry.Get("chat"); ok {
		t.Fatal("completed unleased session was not removed")
	}
	replacement, releaseReplacement := registry.AcquireOrCreate("chat", create)
	if replacement.Snapshot().SessionGeneration <= first.Snapshot().SessionGeneration {
		t.Fatal("replacement session must supersede all predecessor frames")
	}
	releaseReplacement()
	if _, ok := registry.Get("chat"); ok {
		t.Fatal("unused leased session was not released")
	}
}

func TestRegistryRejectedAdmissionReleasesIdleSession(t *testing.T) {
	var registry Registry[*Session[string]]
	session, release := registry.AcquireOrCreate("chat", func() *Session[string] {
		return New(Config[string]{View: func(id string) ItemView { return ItemView{ID: id} }})
	})
	_, err := session.Enqueue("rejected", func(string, bool, []string) error { return ErrNotFound })
	if err != ErrNotFound {
		t.Fatalf("enqueue: %v", err)
	}
	release()
	if _, ok := registry.Get("chat"); ok {
		t.Fatal("rejected idle admission leaked its session")
	}
}
