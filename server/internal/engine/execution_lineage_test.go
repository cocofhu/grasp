package engine

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func executionEngineFixture(t *testing.T, seed func(*gorm.DB)) (*gorm.DB, *executionlineage.SQLAuthority, *executionlineage.Controller) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	if seed != nil {
		seed(db)
	}
	ctx := context.Background()
	authority, err := executionlineage.OpenSQL(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err = storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if err = executionlineage.EnsureRecoverySchema(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	ticket, err := authority.Initialize(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Transaction(func(tx *gorm.DB) error { _, err := authority.QuarantineTx(tx, owner, ticket); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err = authority.Activate(ctx, db, owner, ticket); err != nil {
		t.Fatal(err)
	}
	if err = storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	controller, err := executionlineage.NewLocalController(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	return db, authority, controller
}

func TestExecutionEnginePublishesPreparedRowsAndSerializesLiteralText(t *testing.T) {
	db, _, controller := executionEngineFixture(t, nil)
	if err := db.Table("runs").Create(map[string]any{"id": "active", "status": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	e := &Engine{db: db, execution: controller}
	text := "literal fileref:v1/text/" + strings.Repeat("a", 64)
	row := models.StateRun{RunID: "active", NodeID: "agent", Iteration: 1, Status: "running", OutputMd: text, Outputs: map[string]any{"version": float64(1)}}
	if result := e.executionCreate(context.Background(), "active", &row); result.Error != nil {
		t.Fatal(result.Error)
	}
	if row.ID == 0 || row.Mode != models.NodeModeAuto {
		t.Fatalf("generated fields not retained: %+v", row)
	}
	var got models.StateRun
	if err := db.First(&got, row.ID).Error; err != nil || got.OutputMd != text || got.Outputs["version"] != float64(1) {
		t.Fatalf("payload changed: %+v, %v", got, err)
	}
	row.OutputMd = "updated"
	row.Status = "completed"
	if result := e.executionSave(context.Background(), "active", &row, stateRunTakeoverCols...); result.Error != nil {
		t.Fatal(result.Error)
	}
	trace := []models.TraceEntry{{NodeID: "agent", Event: "exit", Detail: "done"}}
	result := e.executionMutation(context.Background(), "active", func(db *gorm.DB) *gorm.DB {
		return db.Model(&models.Run{}).Where("id = ?", "active").Select("Trace").Updates(&models.Run{Trace: trace})
	})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal(result.Error)
	}
	var run models.Run
	if err := db.First(&run, "id = ?", "active").Error; err != nil || len(run.Trace) != 1 || run.Trace[0].Detail != "done" {
		t.Fatalf("prepared mutation lost payload: %+v %v", run.Trace, err)
	}
	if err := db.First(&got, row.ID).Error; err != nil || got.OutputMd != "updated" || got.Status != "completed" {
		t.Fatal(got, err)
	}
}

func TestExecutionEngineSkipsRestoredHeadAndAdmitsExplicitNewRun(t *testing.T) {
	graph := models.Graph{Nodes: []models.Node{{ID: "input", Type: "input"}, {ID: "output", Type: "output"}}, Edges: []models.Edge{{ID: "edge", Source: "input", Target: "output"}}, Variables: []models.Variable{{Name: "idea", Type: "paragraph", Value: "kept"}}}
	db, _, controller := executionEngineFixture(t, func(db *gorm.DB) {
		if err := db.Create(&models.Run{ID: "restored-head", Status: "queued", Priority: models.PriorityHigh, Graph: graph}).Error; err != nil {
			t.Fatal(err)
		}
	})
	if err := db.Create(&models.WorkflowDef{ID: "wf", Name: "wf", Version: 1, Graph: graph}).Error; err != nil {
		t.Fatal(err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	provider := &fakeProvider{host: host}
	e := New(db, provider, host, arts, 5, controller)
	e.SetBlobStore(blob.NewMemory())
	t.Cleanup(e.Close)
	run, err := e.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var prior models.Run
	if err := db.Select("id", "status").First(&prior, "id = ?", "restored-head").Error; err != nil || prior.Status != "queued" {
		t.Fatal(prior, err)
	}
	var count int64
	if err := db.Model(&models.StateRun{}).Where("run_id = ?", "restored-head").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("restored head was executed", count, err)
	}
	if err := e.ResumePausedRun("restored-head", 0); !errors.Is(err, executionlineage.ErrQuarantined) {
		t.Fatal("restored controls escaped quarantine", err)
	}
}

type pausedExecutionPayload struct {
	objectstore.Store
	entered chan struct{}
	release chan struct{}
}

func (p *pausedExecutionPayload) Put(ctx context.Context, _ string, _ io.ReadSeeker, _ objectstore.Info) error {
	close(p.entered)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.release:
		return nil
	}
}

func TestExecutionEngineFencesPayloadCompletionDuringOfflineRestore(t *testing.T) {
	db, authority, controller := executionEngineFixture(t, nil)
	if err := db.Table("runs").Create(map[string]any{"id": "active", "status": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	e := &Engine{db: db, execution: controller}
	row := models.StateRun{RunID: "active", NodeID: "agent", Iteration: 1, Status: "running", OutputMd: "original"}
	if result := e.executionCreate(context.Background(), "active", &row); result.Error != nil {
		t.Fatal(result.Error)
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	backend := &pausedExecutionPayload{entered: make(chan struct{}), release: make(chan struct{})}
	if err = files.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	row.OutputMd = "late native output"
	finished := make(chan error, 1)
	go func() { finished <- e.executionSave(context.Background(), "active", &row).Error }()
	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("payload not staged")
	}
	if pool.Stats().InUse != 0 {
		close(backend.release)
		t.Fatal("payload preparation retained SQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	owner := uuid.NewString()
	if err = storageguard.Enter(ctx, db, owner, nil); err != nil {
		close(backend.release)
		t.Fatal("blocked payload held SQL lock", err)
	}
	if _, err = authority.BeginRestore(ctx, owner, strings.Repeat("f", 64)); err != nil {
		close(backend.release)
		t.Fatal(err)
	}
	close(backend.release)
	if err = <-finished; !errors.Is(err, executionlineage.ErrInactive) {
		t.Fatal("late payload published after restore", err)
	}
	var raw struct{ OutputMd string }
	if err = db.Table("state_runs").Select("output_md").Where("id = ?", row.ID).Take(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if !filestore.IsRef(raw.OutputMd) {
		t.Fatal("original payload reference lost", raw.OutputMd)
	}
	if err = storageguard.Leave(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
}
