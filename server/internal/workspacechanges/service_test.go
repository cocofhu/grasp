package workspacechanges

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type reviewGitFixture struct {
	after sandbox.WorkspaceTree
}

func (f *reviewGitFixture) CaptureWorkspaceTrees(context.Context, []sandbox.WorkspaceTreeQuery) ([]sandbox.WorkspaceTree, error) {
	return []sandbox.WorkspaceTree{f.after}, nil
}

func (f *reviewGitFixture) CompareWorkspaceTrees(_ context.Context, queries []sandbox.WorkspaceTreeQuery) ([]sandbox.WorkspaceTreeChanges, error) {
	result := make([]sandbox.WorkspaceTreeChanges, len(queries))
	for i, query := range queries {
		if query.From == "" || query.To == "" {
			result[i].Unavailable = true
			continue
		}
		if query.From == query.To {
			continue
		}
		value := []byte(query.To + "\n")
		old := []byte(query.From + "\n")
		patch := []byte("@@ -1 +1 @@\n-" + string(old) + "+" + string(value))
		added, deleted := 1, 1
		result[i] = sandbox.WorkspaceTreeChanges{Added: 1, Deleted: 1, FileCount: 1,
			Files: []sandbox.WorkspaceTreeFile{{Path: "README.md", Status: "M", Added: &added, Deleted: &deleted, Patch: &patch, Before: &old, After: &value}},
		}
	}
	return result, nil
}

func (*reviewGitFixture) ExportWorkspaceTree(context.Context, sandbox.WorkspaceTreeQuery) (io.ReadCloser, error) {
	return nil, errors.New("no baseline pack in fixture")
}

func (*reviewGitFixture) RestoreWorkspaceTree(context.Context, sandbox.WorkspaceTreeQuery, io.Reader) error {
	return errors.New("no baseline pack in fixture")
}

func TestWorkspaceChangesArchiveUsesOwnerAndStableFileVersion(t *testing.T) {
	ctx := t.Context()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	scope := Scope{Kind: models.WorkspaceChangesRun, OwnerID: "run-review", Workspace: "workspace-a", NodeID: "build", Iteration: 1,
		Repos: []models.SessionRepo{{Name: "Project", RepoDirName: "project"}}}
	if err := db.Create(&models.Run{ID: scope.OwnerID, Repos: scope.Repos, WorkspaceChangesEnabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	initial := Trees{"project": {Ref: "initial", Head: "initial", Branch: "main"}}
	if err := db.Create(&models.WorkspaceChangeBaseline{OwnerKind: scope.Kind, OwnerID: scope.OwnerID, Trees: initial}).Error; err != nil {
		t.Fatal(err)
	}
	service := New(db, 0)
	git := &reviewGitFixture{after: sandbox.WorkspaceTree{Ref: "after", Head: "commit", Branch: "feature"}}
	before := Trees{"project": {Ref: "dirty before", Head: "initial", Branch: "feature"}}
	total, err := service.record(ctx, git, scope, "turn-a", before, true)
	if err != nil || total == nil || total.Repos[0].BaseSHA != "initial" {
		t.Fatalf("archive: %+v (%v)", total, err)
	}
	turn, err := service.List(ctx, scope, "", "turn-a", true)
	if err != nil || turn.Snapshot == nil || !turn.Snapshot.Interrupted {
		t.Fatalf("turn: %+v (%v)", turn, err)
	}
	id := turn.Snapshot.ID
	fileID := turn.Snapshot.Repos[0].Files[0].ID
	git.after.Ref = "later"
	latest, err := service.record(ctx, git, scope, "", nil, false)
	if err != nil || latest.ID == total.ID {
		t.Fatalf("refresh: %+v (%v)", latest, err)
	}
	duplicate, err := service.record(ctx, git, scope, "", nil, false)
	if err != nil || duplicate.ID != latest.ID {
		t.Fatalf("unchanged comparison did not reuse archive: %+v (%v)", duplicate, err)
	}
	file, err := service.File(ctx, scope.Kind, scope.OwnerID, id, fileID)
	if err != nil || file.Before == nil || file.After == nil || *file.Before != "dirty before\n" || *file.After != "after\n" {
		t.Fatalf("archived payload changed: %+v (%v)", file, err)
	}
	if _, err := service.File(ctx, scope.Kind, "another-run", id, fileID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-run file read: %v", err)
	}
	if _, err := service.File(ctx, scope.Kind, scope.OwnerID, latest.ID, fileID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-snapshot file read: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return DeleteOwner(tx, scope.Kind, scope.OwnerID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := service.File(ctx, scope.Kind, scope.OwnerID, id, fileID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("orphan file remains addressable: %v", err)
	}
}
