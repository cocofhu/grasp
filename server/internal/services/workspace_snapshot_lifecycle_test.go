package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkspaceMaintenancePauseRetainsQueuedReceiptAndDrainsOwnedOperation(t *testing.T) {
	jobs, remote, _, input := snapshotJobs(t)
	first, err := jobs.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	input.ClientRequestID = "queued-during-other-operation"
	second, err := jobs.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	remote.started, remote.duration = started, time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- jobs.RunOne(ctx, first.ID) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first native operation did not start")
	}
	jobs.Pause()
	if jobs.ActiveOperations() == 0 {
		t.Fatal("maintenance ignored an already owned native operation")
	}
	if err := jobs.RunOne(t.Context(), second.ID); !errors.Is(err, ErrWorkspaceJobOwner) {
		t.Fatal("paused worker claimed a queued intent", err)
	}
	var queued models.WorkspaceSnapshotJob
	if err := jobs.db.Take(&queued, "id = ?", second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if queued.State != "queued" || queued.DispatchedAt != nil {
		t.Fatal("maintenance rewrote an accepted never-dispatched receipt", queued.State)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("native owner did not quiesce within its existing lease")
	}
	if jobs.ActiveOperations() != 0 {
		t.Fatal("finished native owner retained a false active count")
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.creates != 1 {
		t.Fatal("maintenance replayed or dispatched an unrelated queued intent", remote.creates)
	}
}
