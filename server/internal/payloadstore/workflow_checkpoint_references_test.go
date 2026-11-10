package payloadstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkflowCheckpointOutcomeRootAndMissingTableCompatibility(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "checkpoints.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	ctx := context.Background()
	files, _ := filestore.FromDB(db)
	objects := &replicaStore{objects: map[string]replicaBytes{}}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"logical":"fileref:v1/json/` + strings.Repeat("a", 64) + `"}`
	prepared, err := files.Prepare(ctx, strings.NewReader(body), filestore.JSON)
	if err != nil {
		t.Fatal(err)
	}
	row := models.WorkflowVisitCheckpoint{ID: strings.Repeat("b", 64), RunID: "retained-run", StateRunID: 1, OutcomeRef: prepared.Ref(), RouteState: "sealed", FileState: "blocked"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	report, err := VerifyReferences(ctx, db, objects)
	if err != nil || report.Payloads != 1 || report.Cells < 1 {
		t.Fatal("sealed outcome omitted from portable payload catalogue", report, err)
	}
	if err := db.Migrator().DropColumn(&models.WorkflowVisitCheckpoint{}, "OutcomeRef"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("existing malformed checkpoint table silently skipped")
	}
	if err := db.Migrator().DropTable(&models.WorkflowVisitCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	if skip, err := filestore.SkipMissingPayloadTable(ctx, db, "workflow_visit_checkpoints"); err != nil || !skip {
		t.Fatal("old empty snapshot rejected", skip, err)
	}
	if err := db.Create(&models.StateRun{RunID: "owner", WorkspaceCheckpointID: row.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if skip, err := filestore.SkipMissingPayloadTable(ctx, db, "workflow_visit_checkpoints"); err == nil || skip {
		t.Fatal("lost table owning current visit silently accepted", skip, err)
	}
}
