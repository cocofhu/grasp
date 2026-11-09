package services

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestWorkspaceReleasePauseCountsPreviewBeforeItsFirstExternalPreparation(t *testing.T) {
	s, provider, _, _ := workspaceReleaseFixture(t)
	original := s.options.Prepare
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	s.options.Prepare = func(ctx context.Context) (context.Context, error) {
		if once.CompareAndSwap(false, true) {
			close(entered)
			<-proceed
		}
		return original(ctx)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Preview(t.Context(), "alice", "release-solo", WorkspaceReleasePreviewInput{ClientRequestID: "pending-preview"})
		done <- err
	}()
	<-entered
	s.Pause()
	if s.ActiveOperations() != 1 {
		t.Fatal("uncounted admitted preview could cross offline maintenance")
	}
	if _, err := s.Preview(t.Context(), "alice", "release-solo", WorkspaceReleasePreviewInput{ClientRequestID: "new-after-pause"}); !errors.Is(err, ErrWorkspaceReleaseBlocked) {
		t.Fatal("paused service accepted a new preview", err)
	}
	close(proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.ActiveOperations() != 0 || provider.confirms.Load() != 0 {
		t.Fatal("finished preview did not drain or dispatched deletion")
	}
	views, err := s.List(t.Context(), "alice", "release-solo")
	if err != nil || len(views) != 1 {
		t.Fatal("maintenance hid readonly receipts", views, err)
	}
}
func TestWorkspaceReleasePauseRetainsConfirmedIntentAndDrainsNativeOwner(t *testing.T) {
	s, provider, _, parent := workspaceReleaseFixture(t)
	view := workspaceReleaseConfirmed(t, s)
	entered, proceed := make(chan struct{}), make(chan struct{})
	provider.beforeConfirm = func() { close(entered); <-proceed }
	done := make(chan error, 1)
	go func() { done <- s.RunOne(t.Context(), view.ID) }()
	<-entered
	s.Pause()
	if s.ActiveOperations() != 1 {
		t.Fatal("native release was not counted")
	}
	if err := s.RunOne(t.Context(), view.ID); !errors.Is(err, ErrWorkspaceReleaseBlocked) {
		t.Fatal("paused discovery dispatched another operation", err)
	}
	if _, err := s.Confirm(t.Context(), "alice", parent.ID, view.ID, WorkspaceReleaseConfirmInput{ClientRequestID: "confirm-original", Confirmed: true, LossAccepted: true}); !errors.Is(err, ErrWorkspaceReleaseBlocked) {
		t.Fatal("paused service admitted a confirmation", err)
	}
	if _, err := s.Get(t.Context(), "alice", parent.ID, view.ID); err != nil {
		t.Fatal("original readonly receipt unavailable", err)
	}
	close(proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.ActiveOperations() != 0 || provider.confirms.Load() != 1 {
		t.Fatal("native release did not drain exactly once")
	}
}

type releaseBlockingRead struct {
	WorkspaceReleaseProvider
	entered, proceed chan struct{}
}

func (p *releaseBlockingRead) Get(ctx context.Context, resource, id string) (sandbox.EnvironmentReleaseView, error) {
	close(p.entered)
	select {
	case <-ctx.Done():
		return sandbox.EnvironmentReleaseView{}, ctx.Err()
	case <-p.proceed:
	}
	return p.WorkspaceReleaseProvider.Get(ctx, resource, id)
}
func TestWorkspaceReleasePauseCountsHTTPReconciliationAndKeepsUndispatchedReceipt(t *testing.T) {
	s, provider, _, parent := workspaceReleaseFixture(t)
	provider.lost = true
	provider.gone = false
	view := workspaceReleaseConfirmed(t, s)
	if err := s.RunOne(t.Context(), view.ID); !errors.Is(err, ErrWorkspaceReleaseUnknown) {
		t.Fatal(err)
	}
	provider.mu.Lock()
	provider.gone = true
	provider.mu.Unlock()
	blocked := &releaseBlockingRead{WorkspaceReleaseProvider: provider, entered: make(chan struct{}), proceed: make(chan struct{})}
	s.options.Provider = blocked
	done := make(chan error, 1)
	go func() { _, err := s.Reconcile(t.Context(), "alice", parent.ID, view.ID); done <- err }()
	<-blocked.entered
	s.Pause()
	if s.ActiveOperations() != 1 {
		t.Fatal("HTTP reconciliation escaped maintenance accounting")
	}
	close(blocked.proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.ActiveOperations() != 0 || provider.confirms.Load() != 1 {
		t.Fatal("HTTP read did not drain or replayed deletion")
	}
	// A paused background worker may not discover even an accepted intent.
	s2, p2, _, _ := workspaceReleaseFixture(t)
	queued := workspaceReleaseConfirmed(t, s2)
	s2.Pause()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	s2.Run(ctx)
	var row models.WorkspaceRelease
	if err := s2.db.Take(&row, "id = ?", queued.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != "confirmed" || row.DispatchedAt != nil || p2.confirms.Load() != 0 {
		t.Fatal("maintenance consumed an accepted undispatched receipt")
	}
}
