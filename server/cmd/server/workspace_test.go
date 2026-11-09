package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

func TestProductionWorkspaceHistoryWorksWithClosedExecutionButNewWorkNeverDoes(t *testing.T) {
	db := persistenceDB(t)
	if err := db.AutoMigrate(&models.WorkspaceSnapshot{}, &models.WorkspaceSnapshotJob{}, &models.WorkspaceRestore{}); err != nil {
		t.Fatal(err)
	}
	previous := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(previous) })
	cfg := &config.Config{Auth: config.AuthConfig{Users: []config.AuthUser{{Username: "alice", PasswordHash: "test-configured-account"}}}}
	config.StoreConfig(cfg)
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	storage := &payloadstore.Storage{Files: files, Location: payloadstore.Location{Fingerprint: strings.Repeat("a", 64)}}
	execution, err := executionlineage.NewController(t.Context(), db, nil)
	if err == nil || execution == nil {
		t.Fatal("fixture did not keep a closed controller")
	}
	limits, err := (config.WorkspaceOperationsConfig{}).Resolved()
	if err != nil {
		t.Fatal(err)
	}
	runtimeConfig, err := workspaceRuntimeConfiguration(t.Context(), limits, execution)
	if err != nil || runtimeConfig != nil {
		t.Fatal("closed startup advertised native execution", runtimeConfig, err)
	}
	sbx := services.NewSandboxService(db, nil, nil, nil, services.SandboxOptions{Execution: execution})
	workspace, err := newProductionWorkspaceServices(db, sbx, storage, execution, limits, runtimeConfig)
	if err != nil || workspace.Catalog == nil || workspace.Restore == nil || workspace.Bindings != nil {
		t.Fatal("strict composition is incomplete", err)
	}
	if err := db.Table("solo_sessions").Create(map[string]any{"id": "history-solo", "user_id": "alice", "status": "closed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkspaceSnapshot{ID: "historical-ready", ActorID: "alice", ScopeKind: "solo", ScopeID: "history-solo", State: "ready", Lineage: "old-dataset"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Catalog.Get(t.Context(), "alice", "solo", "history-solo", "historical-ready"); err != nil {
		t.Fatal("history required old execution authority", err)
	}
	if _, err := workspace.Restore.Admit(t.Context(), "alice", services.WorkspaceRestoreInput{ClientRequestID: "new-restore-request", SoloID: "history-solo", SnapshotID: "historical-ready", ExpectedBinding: "caller-cannot-enable-execution", Duration: time.Minute}); !errors.Is(err, executionlineage.ErrUnavailable) {
		t.Fatal("closed controller permitted new native work", err)
	}
	cfg = &config.Config{}
	config.StoreConfig(cfg)
	if _, err := workspace.Catalog.Get(t.Context(), "alice", "solo", "history-solo", "historical-ready"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("removed account retained historical scope permission", err)
	}
}

func TestWorkspaceWorkerJoinDoesNotReportCancellationAsStoppedBeforeNativeWorkerExits(t *testing.T) {
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	worker := startWorkspaceWorker(t.Context(), func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release // Simulates waiting for the last remote ownership deadline.
		return context.Cause(ctx)
	})
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := worker.stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unjoined worker allowed removal of runtime presence", err)
	}
	<-cancelled
	close(release)
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := worker.stop(ctx); err != nil {
		t.Fatal("original worker did not finish its cancellation", err)
	}
}
