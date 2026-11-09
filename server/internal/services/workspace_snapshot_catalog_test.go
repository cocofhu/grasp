package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func snapshotCatalogFixture(t *testing.T) (*WorkspaceSnapshotCatalog, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.WorkspaceSnapshot{}, &models.WorkspaceSnapshotJob{}); err != nil {
		t.Fatal(err)
	}
	authorize := func(_ context.Context, _ *gorm.DB, actor, kind, id string) error {
		if actor != "alice" || (kind != "run" || id != "shared-run") && (kind != "solo" || id != "alice-solo") {
			return gorm.ErrRecordNotFound
		}
		return nil
	}
	s, err := NewWorkspaceSnapshotCatalog(db, authorize)
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}

func insertCatalogSnapshot(t *testing.T, db *gorm.DB, id, kind, scope, state string, at time.Time) {
	t.Helper()
	// The unrelated original actor and unusable old source/payload references
	// deliberately cannot serve as current execution or storage permissions.
	values := map[string]any{"id": id, "scope_kind": kind, "scope_id": scope, "actor_id": "bob", "state": state, "lineage": "old-inactive-lineage", "source_sandbox_id": 9999, "source_generation": 17, "archive_ref": "missing-private-object", "manifest_ref": "missing-private-manifest", "created_at": at, "verified_at": at, "job_id": "capture-" + id, "publication_job_id": "publish-" + id, "bytes": 23, "entries": 2}
	if err := db.Table("workspace_snapshots").Create(values).Error; err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSnapshotCatalogHistorySurvivesInactiveSourceAndUsesCurrentScope(t *testing.T) {
	s, db := snapshotCatalogFixture(t)
	insertCatalogSnapshot(t, db, "shared-ready", "run", "shared-run", "ready", time.Now().UTC())
	row, err := s.Get(t.Context(), "alice", "run", "shared-run", "shared-ready")
	if err != nil || row.ID != "shared-ready" || row.State != "ready" || row.VerifiedAt == nil || row.Bytes != 23 {
		t.Fatalf("authorized shared historical metadata unavailable: %+v %v", row, err)
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"bob", "lineage", "archive", "manifest", "generation", "sandbox", "missing-private"} {
		if strings.Contains(strings.ToLower(string(raw)), secret) {
			t.Fatalf("private source or payload metadata leaked: %s", raw)
		}
	}
	for _, in := range [][3]string{{"bob", "run", "shared-run"}, {"alice", "solo", "other-solo"}, {"alice", "run", "other-run"}, {"alice", "solo", "alice-solo"}} {
		if _, err := s.Get(t.Context(), in[0], in[1], in[2], "shared-ready"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("wrong current account/scope exposed metadata: %v %v", in, err)
		}
	}
	if _, err := NewWorkspaceSnapshotCatalog(db, nil); !errors.Is(err, ErrWorkspaceJobAuthority) {
		t.Fatal("missing read authorization became an allow policy", err)
	}
}

func TestWorkspaceSnapshotCatalogPaginationCannotCrossScopeAndReportsUnknownJobs(t *testing.T) {
	s, db := snapshotCatalogFixture(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []string{"snapshot-a", "snapshot-b", "snapshot-c"} {
		insertCatalogSnapshot(t, db, id, "solo", "alice-solo", "capturing", at)
	}
	insertCatalogSnapshot(t, db, "foreign-cursor", "run", "shared-run", "ready", at)
	if err := db.Table("workspace_snapshot_jobs").Create(map[string]any{"id": "capture-snapshot-c", "state": "unknown"}).Error; err != nil {
		t.Fatal(err)
	}
	page, err := s.List(t.Context(), "alice", "solo", "alice-solo", "", 2)
	if err != nil || len(page.Items) != 2 || !page.HasMore || page.NextCursor != "snapshot-b" || page.Items[0].ID != "snapshot-c" || page.Items[0].State != "unknown" {
		t.Fatalf("first page or original unknown receipt lost: %+v %v", page, err)
	}
	last, err := s.List(t.Context(), "alice", "solo", "alice-solo", page.NextCursor, 2)
	if err != nil || last.HasMore || last.NextCursor != "" || len(last.Items) != 1 || last.Items[0].ID != "snapshot-a" {
		t.Fatalf("unstable cursor page: %+v %v", last, err)
	}
	if _, err := s.List(t.Context(), "alice", "solo", "alice-solo", "foreign-cursor", 2); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cursor crossed an otherwise accessible scope", err)
	}
	for _, limit := range []int{-1, 101} {
		if _, err := s.List(t.Context(), "alice", "solo", "alice-solo", "", limit); !errors.Is(err, ErrWorkspaceJobInvalid) {
			t.Fatal("unbounded pagination accepted", limit, err)
		}
	}
	if _, err := s.List(t.Context(), "deleted-account", "solo", "alice-solo", "", 1); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("account removal did not revoke metadata access", err)
	}
}
