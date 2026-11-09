package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"gorm.io/gorm"
)

func TestWorkspaceStrictAuthorizationCapturesUploadsAndPinsReadyWithOneRealSourceJob(t *testing.T) {
	a, target, _, _ := workspaceAuthorizationFixture(t)
	a.sandbox.execution = a.execution
	db := a.db
	if err := db.AutoMigrate(&models.PayloadUploadHold{}, &models.PayloadUploadBudget{}); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	objects := &snapshotObjects{objects: map[string][]byte{}, infos: map[string]objectstore.Info{}}
	// Object callbacks must be able to borrow the only SQL connection. Neither
	// credential proof preparation nor payload PUT/GET can hold an outer TX.
	objects.probe = func(ctx context.Context) error {
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return db.WithContext(bounded).Exec("SELECT 1").Error
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	remote := snapshotArchiveFixture(t)
	remote.caps.Jobs.Resource = "authorized-workspace-resource"
	remote.caps.Jobs.Lineage, err = a.execution.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	holdOptions, err := a.HoldOptions(payloadhold.Options{StorageFingerprint: strings.Repeat("f", 64), LeaseTTL: 2 * time.Second, RenewInterval: time.Second / 2})
	if err != nil {
		t.Fatal(err)
	}
	holdAuth := holdOptions.Authorize
	holdOptions.Authorize = func(ctx context.Context, tx *gorm.DB, id payloadhold.Identity) error {
		if err := holdAuth(ctx, tx, id); err != nil {
			return fmt.Errorf("hold publication authorization: %w", err)
		}
		return nil
	}
	holds, err := payloadhold.New(db, files, holdOptions)
	if err != nil {
		t.Fatal(err)
	}
	jobOptions, err := a.JobOptions(WorkspaceJobOptions{Remote: func(context.Context, uint) (WorkspaceJobRemote, error) { return remote, nil }, LeaseTTL: 2 * time.Second, RenewInterval: time.Second / 2, MaxDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	jobAuth, jobGuard := jobOptions.Authorize, jobOptions.GuardJobPublication
	jobOptions.Authorize = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if err := jobAuth(ctx, tx, row); err != nil {
			return fmt.Errorf("%s job authorization: %w", row.Kind, err)
		}
		return nil
	}
	jobOptions.GuardJobPublication = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if err := jobGuard(ctx, tx, row); err != nil {
			return fmt.Errorf("%s final authorization: %w", row.Kind, err)
		}
		return nil
	}
	p, err := NewWorkspaceSnapshotPipeline(db, a.sandbox, holds, files, payloadhold.DefaultLimits(), jobOptions)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := p.Capture(t.Context(), target.ActorID, WorkspaceCaptureInput{ClientRequestID: "strict-pipeline-request", ScopeKind: target.ScopeKind, ScopeID: target.ScopeID, SandboxID: target.SandboxID, Paths: []string{"."}, Duration: 20 * time.Second})
	if err != nil {
		t.Fatal("strict capture admission", err)
	}
	if err := p.Jobs.RunOne(t.Context(), capture.ID); err != nil {
		t.Fatal("strict capture execution", err)
	}
	if err := p.QueuePublication(t.Context(), target.ActorID, capture.ID); err != nil {
		t.Fatal("strict publication admission reused the capture's execution proof", err)
	}
	var snapshot models.WorkspaceSnapshot
	if err := db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.Jobs.RunOne(t.Context(), snapshot.PublicationJobID); err != nil {
		t.Fatal("strict read/verify/pin operation", err)
	}
	if err := db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
		t.Fatal(err)
	}
	var hold models.PayloadUploadHold
	if err := db.Take(&hold, "operation_id = ?", snapshot.PublicationJobID).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "ready" || snapshot.VerifiedAt == nil || snapshot.ArchiveRef == "" || snapshot.ManifestRef == "" || hold.State != payloadhold.StatePinned {
		t.Fatal("strict production composition failed its atomic verified publication", snapshot.State, hold.State)
	}
	if objects.puts.Load() != 2 || objects.gets.Load() < 2 {
		t.Fatal("real object upload and digest verification were skipped", objects.puts.Load(), objects.gets.Load())
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.creates != 2 || remote.finishes != 1 {
		t.Fatal("publication replayed a dispatched capture", remote.creates, remote.finishes)
	}
}
