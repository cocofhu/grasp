package services

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func seedRunSandbox(t *testing.T, s *SandboxService, ds *dockerState, runID, name string) models.Sandbox {
	t.Helper()
	home := t.TempDir()
	row := models.Sandbox{Name: name, Purpose: "run", Scope: runtimeScopeRun, RunID: runID, NodeID: "n1", Status: "running", HomeDir: home}
	if err := s.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(name, "running")
	return row
}

// A failed container destroy keeps the row (due now, not in use) so the
// sweeper retries it; once the gateway destroys it the row and home go.
func TestSandboxDestroyFailureKeepsRowForSweeperRetry(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	row := seedRunSandbox(t, s, ds, "run-x", "gw-fail")

	ds.fg.FailDestroy = true
	if err := s.Destroy(context.Background(), row.ID); err == nil {
		t.Fatal("Destroy must report a failed container destroy")
	}
	var kept models.Sandbox
	if err := db.First(&kept, row.ID).Error; err != nil {
		t.Fatalf("row must be kept for retry: %v", err)
	}
	if kept.DestroyAt == nil || kept.Status != "error" || !ds.fg.Has("gw-fail") {
		t.Fatalf("row not queued for retry: status=%s destroyAt=%v container=%v", kept.Status, kept.DestroyAt, ds.fg.Has("gw-fail"))
	}
	if _, err := os.Stat(row.HomeDir); err != nil {
		t.Fatalf("home must stay until the container is gone: %v", err)
	}

	ds.fg.FailDestroy = false
	s.sweepOnce(context.Background())
	if ds.fg.Has("gw-fail") {
		t.Fatal("sweeper retry must destroy the container")
	}
	if err := db.First(&models.Sandbox{}, row.ID).Error; err == nil {
		t.Fatal("row must be dropped after a successful destroy")
	}
	if _, err := os.Stat(row.HomeDir); !os.IsNotExist(err) {
		t.Fatalf("home should be removed, stat err=%v", err)
	}
}

// The runtime's destroy-failure callback adopts the gateway id when the row
// still carries the creating placeholder, so the retry addresses the real
// container.
func TestMarkRunSandboxDestroyFailedAdoptsGatewayID(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	s.BeginRunSandbox(runtime.RunSandboxInfo{Name: "grasp-sb-placeholder", RunID: "run-p", NodeID: "n1", Scope: "node"})
	ds.setStatus("gw-real", "running")

	s.MarkRunSandboxDestroyFailed("grasp-sb-placeholder", "gw-real", errors.New("boom"))

	var row models.Sandbox
	if err := db.Where("run_id = ?", "run-p").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "gw-real" || row.DestroyAt == nil || s.isBusyRow(&row) {
		t.Fatalf("row not handed to the sweeper under the gateway id: %+v busy=%v", row, s.isBusyRow(&row))
	}
	s.sweepOnce(context.Background())
	if ds.fg.Has("gw-real") {
		t.Fatal("sweeper must destroy the adopted container")
	}
}

// BeginRunSandbox replaces only a previous placeholder of the run+node; a
// kept (retired or destroy-pending) row of the same node keeps its name, so
// its container is still tracked.
func TestBeginRunSandboxKeepsNonPlaceholderRowOfSameNode(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	seedRunSandbox(t, s, ds, "run-b", "gw-kept")

	s.BeginRunSandbox(runtime.RunSandboxInfo{Name: "grasp-sb-new", RunID: "run-b", NodeID: "n1", Scope: runtimeScopeRun})

	for _, name := range []string{"gw-kept", "grasp-sb-new"} {
		if err := db.Where("name = ?", name).First(&models.Sandbox{}).Error; err != nil {
			t.Fatalf("row %s missing: %v", name, err)
		}
	}
}

// Deleting a run destroys its containers before the rows (binding and
// generation records included) go. A failed destroy keeps the run and rows
// so deleting again retries.
func TestRunDeleteDestroysSandboxContainersFirst(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	sbx := newSandboxService(t, db, ds)
	runs := NewRunService(db)
	runs.SetSandboxReaper(sbx)
	db.Create(&models.Run{ID: "run-d", WorkflowID: "wf", Status: "completed"})
	row := seedRunSandbox(t, sbx, ds, "run-d", "gw-run-d")
	db.Create(&models.RunSandboxBinding{RunID: "run-d", SandboxID: row.ID, Generation: 1, Status: "retired", CreatedAt: time.Now()})
	db.Create(&models.RunSandboxGeneration{RunID: "run-d", SandboxID: row.ID, Generation: 1, Status: "retired", CreatedAt: time.Now()})

	ds.fg.FailDestroy = true
	if err := runs.Delete("run-d"); err == nil {
		t.Fatal("delete must fail while the container cannot be destroyed")
	}
	if _, ok := runs.Get("run-d"); !ok {
		t.Fatal("run must stay when its container was not destroyed")
	}
	if err := db.First(&models.Sandbox{}, row.ID).Error; err != nil {
		t.Fatalf("sandbox row must stay for retry: %v", err)
	}

	ds.fg.FailDestroy = false
	if err := runs.Delete("run-d"); err != nil {
		t.Fatalf("retried delete: %v", err)
	}
	if ds.fg.Has("gw-run-d") {
		t.Fatal("run delete left the container behind")
	}
	var n int64
	for name, m := range map[string]any{"sandbox": &models.Sandbox{}, "binding": &models.RunSandboxBinding{}, "generation": &models.RunSandboxGeneration{}} {
		db.Model(m).Where("run_id = ?", "run-d").Count(&n)
		if n != 0 {
			t.Fatalf("%s rows left after run delete: %d", name, n)
		}
	}
}

// Deleting a workflow destroys the containers of every run it cascades.
func TestWorkflowDeleteDestroysRunSandboxContainers(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	sbx := newSandboxService(t, db, ds)
	wfs := NewWorkflowService(db)
	wfs.SetSandboxReaper(sbx)
	wf := &models.WorkflowDef{ID: "wf-sb", ProjectID: models.DefaultProjectID, Name: "W", Graph: validGraph()}
	if err := wfs.Save(wf); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Run{ID: "run-w", WorkflowID: "wf-sb", Status: "completed"})
	seedRunSandbox(t, sbx, ds, "run-w", "gw-run-w")

	if err := wfs.Delete("wf-sb"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ds.fg.Has("gw-run-w") {
		t.Fatal("workflow delete left a run container behind")
	}
	var n int64
	db.Model(&models.Sandbox{}).Where("run_id = ?", "run-w").Count(&n)
	if n != 0 {
		t.Fatalf("sandbox rows left after workflow delete: %d", n)
	}
}
