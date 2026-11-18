package services

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/solobackup"
)

// expireModelIdle moves the sandbox's model idle interval past the TTL.
func expireModelIdle(t *testing.T, s *SandboxService, id uint) time.Time {
	t.Helper()
	old := time.Now().Add(-s.IdleTTL() - time.Minute)
	s.mu.Lock()
	delete(s.modelActive, id)
	s.mu.Unlock()
	if err := s.db.Model(&models.Sandbox{}).Where("id = ?", id).Update("model_active_at", old).Error; err != nil {
		t.Fatal(err)
	}
	return old
}

func TestIdleSweepReadsLatestActivityNotStaleCandidate(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	old := expireModelIdle(t, f.sbx, row.ID)
	row.ModelActiveAt = &old
	f.sbx.MarkModelActivity(row.ID)
	f.sbx.sweepRows(context.Background(), []models.Sandbox{*row})
	if fresh := f.recovery(t); fresh.Status != "running" {
		t.Fatal("stale sweep reclaimed active sandbox")
	}
}

type blockingRecoveryStore struct {
	solobackup.Store
	started chan struct{}
	release chan struct{}
}

func (s *blockingRecoveryStore) Put(ctx context.Context, key string, r io.Reader) (solobackup.Object, error) {
	close(s.started)
	select {
	case <-ctx.Done():
		return solobackup.Object{}, ctx.Err()
	case <-s.release:
	}
	return s.Store.Put(ctx, key, r)
}

func TestNewSoloTurnWaitsForSaveThenRestoresLatest(t *testing.T) {
	f := newStopFixture(t)
	store := &blockingRecoveryStore{Store: f.store, started: make(chan struct{}), release: make(chan struct{})}
	f.solo.SetBackupStore(store)
	chat := newRestoreChat(false)
	f.solo.SetChatterForTest(chat)
	row := f.recovery(t)
	expireModelIdle(t, f.sbx, row.ID)
	saved := make(chan struct{})
	go func() { f.sbx.sweepOnce(context.Background()); close(saved) }()
	select {
	case <-store.started:
	case <-time.After(3 * time.Second):
		t.Fatal("save did not start")
	}
	sent := make(chan error, 1)
	go func() {
		_, err := f.solo.SendWithReferences(f.id, "alice", "continue saved work", nil, nil)
		sent <- err
	}()
	select {
	case err := <-sent:
		t.Fatalf("message bypassed recovery lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(store.release)
	select {
	case <-saved:
	case <-time.After(3 * time.Second):
		t.Fatal("save did not finish")
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	select {
	case <-chat.started:
	case <-time.After(3 * time.Second):
		t.Fatal("queued work did not wake saved environment")
	}
	if len(f.exec.restoredArchives()) != 1 {
		t.Fatal("turn ran before restoring workspace")
	}
	waitFor(t, "turn complete", func() bool { c := f.solo.lookup(f.id); return c != nil && !c.sess.Snapshot().Busy })
}

func TestIdleSavedSoloDoesNotWakeInBackground(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	expireModelIdle(t, f.sbx, row.ID)
	f.sbx.sweepOnce(context.Background())
	f.sbx.RecoverSoloSandboxes(context.Background())
	if row := f.recovery(t); row.Status != "stopped" {
		t.Fatalf("idle recovery woke itself: %s", row.Status)
	}
	if f.row(t).Status != models.SoloStatusRunning {
		t.Fatal("automatic idle save closed the conversation")
	}
}

func TestPausedVMReleasesCapacityWithoutCompletingRecoveryIntent(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.fg.Driver = "e2b"
	f.fg.SetStatus(row.Name, "stopped")
	f.fg.FailStop = true
	if err := f.sbx.Reclaim(context.Background(), row.ID); err == nil {
		t.Fatal("incomplete snapshot operation reported success")
	}
	if row := f.recovery(t); row.Status != "stopped" || row.RecoveryPhase != "releasing" || row.Error == "" {
		t.Fatalf("incomplete recovery intent lost: status=%s phase=%s error=%q", row.Status, row.RecoveryPhase, row.Error)
	}
	capacity, _ := f.sbx.Capacity()
	if capacity.Used != 0 {
		t.Fatal("provider-confirmed paused workload occupied compute capacity")
	}
	if _, _, err := f.sbx.EnsureRunning(context.Background(), row.ID); err == nil {
		t.Fatal("wake bypassed the incomplete recovery operation")
	}
	f.fg.FailStop = false
	f.sbx.RetryRecoveryCleanup(context.Background())
	if row := f.recovery(t); row.Status != "stopped" || row.RecoveryPhase != "" || row.Error != "" {
		t.Fatalf("retry did not finish recovery: status=%s phase=%s error=%q", row.Status, row.RecoveryPhase, row.Error)
	}
}

func TestFailedSaveCannotOverwriteStillRunningWorkspace(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	// The archive is durable, but the provider never stopped the original VM
	// and its status endpoint was unavailable while the save failed.
	f.fg.SetStatus(row.Name, "running")
	f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("status", "saving")
	if _, warm, err := f.sbx.EnsureRunning(context.Background(), row.ID); err != nil || !warm {
		t.Fatalf("original guest not retained: warm=%v err=%v", warm, err)
	}
	if len(f.exec.restoredArchives()) != 0 {
		t.Fatal("an old archive overwrote the original running workspace")
	}
}

func TestSharedRunStaysBusyUntilEveryTaskReleases(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("purpose", "run")
	releaseA, err := f.sbx.AcquireRunSandbox(context.Background(), row.Name)
	if err != nil {
		t.Fatal(err)
	}
	releaseB, err := f.sbx.AcquireRunSandbox(context.Background(), row.Name)
	if err != nil {
		t.Fatal(err)
	}
	releaseA()
	row.Purpose = "run"
	if !f.sbx.isBusyRow(row) {
		t.Fatal("one completing node hid another active task")
	}
	if err := f.sbx.Reclaim(context.Background(), row.ID); err == nil {
		t.Fatal("shared active task was reclaimed")
	}
	releaseB()
	if f.sbx.isBusyRow(row) {
		t.Fatal("released tasks stayed busy")
	}
}

func TestCancelledWorkflowCleanupResumesAfterRestart(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"purpose": "run", "run_id": "cancelled"})
	f.db.Create(&models.Run{ID: "cancelled", Status: "cancelled"})
	f.sbx.RetryRecoveryCleanup(context.Background())
	waitFor(t, "cancelled workflow cleanup after restart", func() bool {
		_, err := f.sbx.Get(row.ID)
		return err != nil
	})
}

func TestCancellationSeparatesProviderComputeStatusFromBackupCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		probeFailure bool
		released     bool
	}{
		{name: "running", status: "running"},
		{name: "paused", status: "stopped", released: true},
		{name: "absent", status: "not_found", released: true},
		{name: "unavailable", status: "not_found", probeFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStopFixture(t)
			row := f.recovery(t)
			f.fg.Driver = "e2b"
			f.fg.FailDelete = true
			f.fg.SetStatus(row.Name, tc.status)
			f.fg.FailStatus = tc.probeFailure
			if err := f.sbx.CancelSandbox(context.Background(), row.ID); err == nil {
				t.Fatal("pending snapshot cleanup reported success")
			}
			pending := f.recovery(t)
			if !pending.CleanupPending || (pending.Status == "stopped") != tc.released {
				t.Fatalf("unproven deletion status=%s pending=%v", pending.Status, pending.CleanupPending)
			}
			wantUsed := 1
			if tc.released {
				wantUsed = 0
			}
			if capacity, err := f.sbx.Capacity(); err != nil || capacity.Used != wantUsed {
				t.Fatalf("capacity=%+v err=%v want=%d", capacity, err, wantUsed)
			}
			if _, _, err := f.sbx.EnsureRunning(context.Background(), row.ID); err == nil {
				t.Fatal("cancelled sandbox could be resumed")
			}
			f.fg.FailDelete, f.fg.FailStatus = false, false
			f.sbx.RetryRecoveryCleanup(context.Background())
			if _, err := f.sbx.Get(row.ID); err == nil {
				t.Fatal("successful API cleanup retry kept deletion intent")
			}
		})
	}
}

func TestOpeningSavedInteractiveEnvironmentRestoresSameWorkspace(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	if err := f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("purpose", "test").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.sbx.Reclaim(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	reopened, err := f.sbx.Open(context.Background(), row.Profile, nil)
	if err != nil || reopened == nil || reopened.ID != row.ID || reopened.Status != "running" {
		t.Fatalf("reopen=%+v err=%v", reopened, err)
	}
	if len(f.exec.restoredArchives()) != 1 {
		t.Fatal("reopened before restoring saved workspace")
	}
}
