package workspacechanges

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestArchiveCleanupFollowsRetentionAndOwnerCommit(t *testing.T) {
	ctx := t.Context()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	store, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: models.WorkspaceChangesRun, OwnerID: "cleanup-run", Workspace: "sandbox", Repos: []models.SessionRepo{{RepoDirName: "project"}}}
	if err := db.Create(&models.Run{ID: scope.OwnerID, Repos: scope.Repos, StartedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	base := models.WorkspaceChangeBaseline{OwnerKind: scope.Kind, OwnerID: scope.OwnerID, Trees: Trees{"project": {Ref: "initial"}}}
	if err := db.Create(&base).Error; err != nil {
		t.Fatal(err)
	}
	shared, _, err := store.Put(ctx, strings.NewReader("initial\n"), filestore.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, 0)
	fixture := new(reviewGitFixture)
	var refs [][]string
	for _, value := range []string{"change-one", "change-two", "change-three"} {
		fixture.after = sandbox.WorkspaceTree{Ref: value}
		snapshot, err := service.record(ctx, fixture, scope, "", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		current := make([]string, 4)
		if err := db.Model(&models.WorkspaceChangeSnapshot{}).Select("repos").Where("id = ?", snapshot.ID).Row().Scan(&current[0]); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.WorkspaceChangeContent{}).Select([]string{"patch", "before", "after"}).
			Where("snapshot_id = ?", snapshot.ID).Row().Scan(&current[1], &current[2], &current[3]); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, current)
	}
	if refs[0][2] == refs[1][2] || refs[0][2] == shared {
		t.Fatal("snapshot bodies share a namespace")
	}
	assertRef := func(ref string, exists bool) {
		t.Helper()
		data, err := store.Read(ctx, ref)
		if (exists && (err != nil || len(data) == 0)) || (!exists && !errors.Is(err, os.ErrNotExist)) {
			t.Fatalf("archive object exists=%v, want %v (%v)", err == nil, exists, err)
		}
	}
	// Merely enqueuing a deletion must not remove files if SQL rolls back.
	rollback := errors.New("fixture rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := DeleteOwner(tx, scope.Kind, scope.OwnerID); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("owner deletion rollback: %v", err)
	}
	if err := service.collectArchives(ctx); err != nil {
		t.Fatal(err)
	}
	for i, group := range refs {
		for _, ref := range group {
			assertRef(ref, i > 0)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return DeleteOwner(tx, scope.Kind, scope.OwnerID) }); err != nil {
		t.Fatal(err)
	}
	if err := service.collectArchives(ctx); err != nil {
		t.Fatal(err)
	}
	for _, group := range refs[1:] {
		for _, ref := range group {
			assertRef(ref, false)
		}
	}
	assertRef(shared, true)
	stagedID := uuid.NewString()
	stagedCtx, err := service.stageArchive(ctx, stagedID)
	if err != nil {
		t.Fatal(err)
	}
	stagedRef, _, err := store.Put(stagedCtx, strings.NewReader("interrupted write"), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.collectArchives(ctx); err != nil {
		t.Fatal(err)
	}
	assertRef(stagedRef, true)
	service.abandonArchive(ctx, stagedID)
	if err := service.collectArchives(ctx); err != nil {
		t.Fatal(err)
	}
	assertRef(stagedRef, false)
}
