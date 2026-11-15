package services

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestInterruptedReleaseRetriesPendingGatewayBeforeRestore(t *testing.T) {
	for _, driver := range []string{"docker", "kubernetes", "e2b"} {
		t.Run(driver, func(t *testing.T) {
			f := newStopFixture(t)
			row := f.recovery(t)
			f.fg.Driver, f.fg.FailStop = driver, true
			stops := 0
			f.fg.OnStop = func() { stops++; f.fg.SetStatus(row.Name, "pending") }
			if err := f.sbx.Reclaim(context.Background(), row.ID); err == nil {
				t.Fatal("partial release reported success")
			}
			fresh := f.recovery(t)
			if fresh.Status != "saving" || fresh.RecoveryPhase != "releasing" {
				t.Fatalf("release intent lost: %+v", fresh)
			}
			if capacity, _ := f.sbx.Capacity(); capacity.Used != 1 {
				t.Fatal("unconfirmed release freed quota")
			}
			f.fg.FailStop = false
			// Simulate process-local connection state disappearing. Only the DB
			// intent and the gateway's pending state survive.
			f.sbx.teardownLive(row.ID)
			f.sbx.RetryRecoveryCleanup(context.Background())
			fresh = f.recovery(t)
			if fresh.Status != "stopped" || fresh.RecoveryPhase != "" || stops != 2 {
				t.Fatalf("retry did not finish release: status=%s phase=%s stops=%d", fresh.Status, fresh.RecoveryPhase, stops)
			}
			if _, _, err := f.sbx.EnsureRunning(context.Background(), row.ID); err != nil {
				t.Fatal(err)
			}
			if driver != "e2b" && len(f.exec.restoredArchives()) != 1 {
				t.Fatal("file restore did not follow completed release")
			}
		})
	}
}

func TestInterruptedArchiveCannotUseOlderPointToAuthorizeRelease(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	// There is a valid older backup, but a newer archive did not finish.
	f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"status": "saving", "recovery_phase": "archiving"})
	f.fg.SetStatus(row.Name, "pending")
	stops := 0
	f.fg.OnStop = func() { stops++ }
	f.sbx.RetryRecoveryCleanup(context.Background())
	if fresh := f.recovery(t); fresh.Status != "saving" || stops != 0 {
		t.Fatal("old point authorized a destructive stop")
	}
	f.fg.SetStatus(row.Name, "running")
	f.sbx.RetryRecoveryCleanup(context.Background())
	if fresh := f.recovery(t); fresh.Status != "running" || stops != 0 || len(f.exec.restoredArchives()) != 0 {
		t.Fatal("original running workspace was replaced")
	}
}

func TestSoloWakeFailureKeepsReleaseIntentRetryable(t *testing.T) {
	for _, driver := range []string{"docker", "kubernetes", "e2b"} {
		t.Run(driver, func(t *testing.T) {
			f := newStopFixture(t)
			row := f.recovery(t)
			f.fg.Driver, f.fg.FailStop = driver, true
			stops := 0
			f.fg.OnStop = func() { stops++; f.fg.SetStatus(row.Name, "pending") }
			if err := f.sbx.Reclaim(context.Background(), row.ID); err == nil {
				t.Fatal("partial release reported success")
			}
			if err := f.sbx.EnsureSoloRunning(context.Background(), row.ID); err == nil {
				t.Fatal("uncompleted release reported a running Solo environment")
			}
			if fresh := f.recovery(t); fresh.Status != "saving" || fresh.RecoveryPhase != "releasing" || stops != 2 {
				t.Fatalf("Solo error erased pending release: status=%s phase=%s stops=%d", fresh.Status, fresh.RecoveryPhase, stops)
			}
			f.fg.FailStop = false
			if err := f.sbx.EnsureSoloRunning(context.Background(), row.ID); err != nil {
				t.Fatal(err)
			}
			if fresh := f.recovery(t); fresh.Status != "running" || fresh.RecoveryPhase != "" || stops < 3 {
				t.Fatalf("Solo could not finish release and restore: status=%s phase=%s stops=%d", fresh.Status, fresh.RecoveryPhase, stops)
			}
			if driver != "e2b" && len(f.exec.restoredArchives()) != 1 {
				t.Fatal("Solo did not restore its saved workspace")
			}
		})
	}
}

func TestReleaseIntentSurvivesLegacyErrorStatus(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.fg.FailStop = true
	f.fg.OnStop = func() { f.fg.SetStatus(row.Name, "pending") }
	if err := f.sbx.Reclaim(context.Background(), row.ID); err == nil {
		t.Fatal("partial release reported success")
	}
	// An older error reporter changed presentation state but not the durable
	// release phase. A restart must still finish that exact operation.
	if err := f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("status", "error").Error; err != nil {
		t.Fatal(err)
	}
	f.fg.FailStop = false
	f.sbx.RetryRecoveryCleanup(context.Background())
	if fresh := f.recovery(t); fresh.Status != "stopped" || fresh.RecoveryPhase != "" {
		t.Fatalf("legacy error lost release retry: status=%s phase=%s", fresh.Status, fresh.RecoveryPhase)
	}
}

func TestCommittedReleaseReconcilesManualStopAfterInterruptedSessionUpdate(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	// The workload release was durable, but the process exited before updating
	// the conversation. Cleanup retries must finish the session transition too.
	if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("status", models.SoloStatusStopping).Error; err != nil {
		t.Fatal(err)
	}
	f.sbx.RetryRecoveryCleanup(context.Background())
	if f.row(t).Status != models.SoloStatusStopped || f.recovery(t).Name != row.Name {
		t.Fatal("completed release left the session permanently stopping")
	}
}

func TestRunCancellationPersistsAllIntentBeforeSlowProviderCleanup(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"purpose": "run", "run_id": "cancelled-run"})
	second := models.Sandbox{Name: "another-workload", Purpose: "run", RunID: "cancelled-run", Status: "running"}
	if err := f.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	f.fg.Seed(second.Name)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	original := f.fg.Server.Config.Handler
	f.fg.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			once.Do(func() { close(entered); <-release; close(finished) })
		}
		original.ServeHTTP(w, r)
	})
	returned := make(chan error, 1)
	go func() { returned <- f.sbx.CancelRunSandboxes(context.Background(), "cancelled-run") }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("cancellation waited for provider cleanup")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("background cleanup did not start")
	}
	var rows []models.Sandbox
	if err := f.db.Where("run_id = ?", "cancelled-run").Find(&rows).Error; err != nil {
		close(release)
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].CleanupPending || !rows[1].CleanupPending {
		close(release)
		t.Fatal("cancel returned before persisting every cleanup intent")
	}
	if _, _, err := f.sbx.EnsureRunning(context.Background(), second.ID); err == nil {
		close(release)
		t.Fatal("cancelled workload could be rebuilt")
	}
	close(release)
	<-finished
	waitFor(t, "cancel cleanup", func() bool {
		var count int64
		return f.db.Model(&models.Sandbox{}).Where("run_id = ?", "cancelled-run").Count(&count).Error == nil && count == 0
	})
}
