package services

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

type snapshotPreparedCompletion struct {
	prepare func(context.Context, *gorm.DB) error
	commit  func(context.Context, *gorm.DB) error
}

func (c snapshotPreparedCompletion) PrepareTx(ctx context.Context, tx *gorm.DB) error {
	return c.prepare(ctx, tx)
}
func (c snapshotPreparedCompletion) Commit(ctx context.Context, tx *gorm.DB) error {
	return c.commit(ctx, tx)
}
func (c snapshotPreparedCompletion) Close() error { return nil }

func TestWorkspaceSnapshotPublicationPreparesParentBeforePhysicalFenceWithFreshProof(t *testing.T) {
	s, _, _, input := snapshotJobs(t)
	s.options.LeaseTTL, s.options.RenewInterval = 5*time.Second, time.Second
	row, err := s.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	_, lease, err := s.claim(t.Context(), row.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	type proofKey struct{}
	var mu sync.Mutex
	events := []string{}
	record := func(event string) { mu.Lock(); defer mu.Unlock(); events = append(events, event) }
	if err := s.db.Callback().Query().Before("gorm:query").Register("test:snapshot_publication_lock_order", func(tx *gorm.DB) {
		if tx.Statement.Table == "sandboxes" && slices.Contains(tx.Statement.Selects, "operation_owner") {
			record("physical_fence")
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.db.Callback().Query().Remove("test:snapshot_publication_lock_order") }()
	original := s.options.Authorize
	s.options.PrepareAuthorization = func(ctx context.Context) (context.Context, error) {
		record("fresh_external_proof")
		return context.WithValue(ctx, proofKey{}, "fresh"), nil
	}
	s.options.Authorize = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
			if ctx.Value(proofKey{}) != "fresh" {
				return errors.New("publication reused an old process proof")
			}
			record("parent_authorize")
		}
		return original(ctx, tx, row)
	}
	err = s.owned(t.Context(), lease, false, func(work context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease) error {
		completion := snapshotPreparedCompletion{
			prepare: func(ctx context.Context, tx *gorm.DB) error {
				if ctx.Value(proofKey{}) != "fresh" {
					return errors.New("metadata prepare missing proof")
				}
				record("source_restore_metadata")
				return nil
			},
			commit: func(ctx context.Context, tx *gorm.DB) error {
				if ctx.Value(proofKey{}) != "fresh" {
					return errors.New("commit missing fresh proof")
				}
				record("pure_completion")
				return nil
			},
		}
		return s.publishCompletion(context.WithValue(work, proofKey{}, "expired-process-proof"), row, lease, []string{"queued"}, map[string]any{"state": "executed"}, completion)
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	positions := map[string]int{}
	for i, event := range events {
		if _, exists := positions[event]; !exists {
			positions[event] = i
		}
	}
	for _, event := range []string{"fresh_external_proof", "parent_authorize", "source_restore_metadata", "physical_fence", "pure_completion"} {
		if _, exists := positions[event]; !exists {
			t.Fatalf("missing %s in publication order %v", event, events)
		}
	}
	if positions["fresh_external_proof"] > positions["parent_authorize"] || positions["parent_authorize"] > positions["source_restore_metadata"] || positions["source_restore_metadata"] > positions["physical_fence"] || positions["physical_fence"] > positions["pure_completion"] {
		t.Fatal("publication reversed higher-level admission locks", events)
	}
}
