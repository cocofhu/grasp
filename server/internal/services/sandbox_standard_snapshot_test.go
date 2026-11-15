package services

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSnapshotWakeUsesNativeRecoveryWithoutAnotherSave(t *testing.T) {
	for _, tc := range []struct {
		name, status, kind, providerStatus string
	}{
		{name: "saved", status: "stopped", kind: "snapshot", providerStatus: "stopped"},
		{name: "provider-autopause", status: "running", providerStatus: "stopped"},
		{name: "interrupted-resume", status: "resuming", kind: "snapshot", providerStatus: "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStopFixture(t)
			row := f.recovery(t)
			f.fg.Driver, f.fg.FailStop = "e2b", true
			f.fg.SetStatus(row.Name, tc.providerStatus)
			if err := f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{
				"status": tc.status, "recovery_kind": tc.kind,
			}).Error; err != nil {
				t.Fatal(err)
			}
			var stops, starts atomic.Int32
			f.fg.OnStop = func() { stops.Add(1) }
			f.fg.OnStart = func(sandbox.GWStartOptions) { starts.Add(1) }
			sb, warm, err := f.sbx.EnsureRunning(context.Background(), row.ID)
			if err != nil || !warm || sb == nil || sb.Name != row.Name || sb.Password != row.Token {
				t.Fatalf("snapshot recovery changed identity or failed: sandbox=%+v warm=%v err=%v", sb, warm, err)
			}
			if stops.Load() != 0 || starts.Load() != 1 {
				t.Fatalf("wake created another recovery point: stops=%d starts=%d", stops.Load(), starts.Load())
			}
			if fresh := f.recovery(t); fresh.Status != "running" || fresh.Name != row.Name || len(fresh.RecoveryPoints) != 0 {
				t.Fatalf("unexpected recovered state: %+v", fresh)
			}
			if f.exec.archives != 0 || len(f.exec.restoredArchives()) != 0 {
				t.Fatal("native snapshot recovery copied workspace archives")
			}
		})
	}
}

func TestInterruptedSnapshotResumeObservesPauseWithoutCreatingSnapshot(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.fg.Driver, f.fg.FailStop = "e2b", true
	f.fg.SetStatus(row.Name, "stopped")
	if err := f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status": "resuming", "recovery_kind": "snapshot",
	}).Error; err != nil {
		t.Fatal(err)
	}
	var stops atomic.Int32
	f.fg.OnStop = func() { stops.Add(1) }
	f.sbx.RetryRecoveryCleanup(context.Background())
	if fresh := f.recovery(t); fresh.Status != "stopped" || fresh.RecoveryPhase != "" || stops.Load() != 0 {
		t.Fatalf("restart replayed a save: status=%s phase=%s stops=%d", fresh.Status, fresh.RecoveryPhase, stops.Load())
	}
	if capacity, err := f.sbx.Capacity(); err != nil || capacity.Used != 0 {
		t.Fatalf("paused snapshot consumed capacity after restart: %+v %v", capacity, err)
	}
}

func TestSnapshotReleaseRetryDoesNotAnnounceSuccessfulManualStopEarly(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.fg.Driver, f.fg.FailStop = "e2b", true
	f.fg.OnStop = func() { f.fg.SetStatus(row.Name, "stopped") }
	if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("status", models.SoloStatusStopping).Error; err != nil {
		t.Fatal(err)
	}
	f.solo.finishStop(f.row(t))
	for range 2 {
		if fresh := f.recovery(t); fresh.Status != "stopped" || fresh.RecoveryPhase != "releasing" {
			t.Fatalf("release intent lost: status=%s phase=%s", fresh.Status, fresh.RecoveryPhase)
		}
		if session := f.row(t); session.Status != models.SoloStatusStopping || session.Error == "" {
			t.Fatalf("pending release announced completion: status=%s error=%q", session.Status, session.Error)
		}
		if capacity, err := f.sbx.Capacity(); err != nil || capacity.Used != 0 {
			t.Fatalf("paused guest consumed capacity: %+v %v", capacity, err)
		}
		f.sbx.RetryRecoveryCleanup(context.Background())
	}
	f.fg.FailStop = false
	f.sbx.RetryRecoveryCleanup(context.Background())
	if fresh := f.recovery(t); fresh.Status != "stopped" || fresh.RecoveryPhase != "" {
		t.Fatalf("retry did not complete release: status=%s phase=%s", fresh.Status, fresh.RecoveryPhase)
	}
	if session := f.row(t); session.Status != models.SoloStatusStopped || session.Error != "" {
		t.Fatalf("completed release did not finish manual stop: status=%s error=%q", session.Status, session.Error)
	}
}

func TestStoppedReleaseIntentStillRetriesExplicitReclaim(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.fg.Driver, f.fg.FailStop = "e2b", true
	f.fg.OnStop = func() { f.fg.SetStatus(row.Name, "stopped") }
	if err := f.sbx.Reclaim(context.Background(), row.ID); err == nil {
		t.Fatal("incomplete release reported success")
	}
	if err := f.sbx.Reclaim(context.Background(), row.ID); err == nil {
		t.Fatal("stopped allocation hid its unfinished release")
	}
	f.fg.FailStop = false
	if err := f.sbx.Reclaim(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	if fresh := f.recovery(t); fresh.RecoveryPhase != "" || fresh.Status != "stopped" {
		t.Fatalf("explicit retry did not finish release: %+v", fresh)
	}
}
