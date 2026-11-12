package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/solobackup"
)

func (f *stopFixture) setArchive(content string) {
	f.exec.mu.Lock()
	f.exec.archive = content
	f.exec.mu.Unlock()
}

func (f *stopFixture) resume(t *testing.T, backupID uint) models.SoloSession {
	t.Helper()
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", backupID); err != nil {
		t.Fatal(err)
	}
	return f.waitStatus(t, models.SoloStatusRunning)
}

func (f *stopFixture) backups(t *testing.T) []models.SoloBackup {
	t.Helper()
	list, err := f.solo.ListBackups(f.id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func (f *stopFixture) stored(key string) bool {
	rc, err := f.store.Open(context.Background(), key)
	if err != nil {
		return false
	}
	_ = rc.Close()
	return true
}

// makeDue backdates the last archive and records later activity, so the
// schedule considers the session.
func (f *stopFixture) makeDue(t *testing.T) {
	t.Helper()
	old, now := time.Now().Add(-2*time.Hour), time.Now()
	f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Updates(map[string]any{"created_at": old, "backup_at": &old, "last_active_at": &now})
}

func TestSoloBackupsKeepNewestAndPruneObjects(t *testing.T) {
	f := newStopFixture(t)
	f.solo.SetBackupPolicy(2, 0)
	var keys []string
	for i, content := range []string{"v1", "v2", "v3"} {
		if i > 0 {
			f.resume(t, 0)
		}
		f.setArchive(content)
		keys = append(keys, f.stop(t).BackupKey)
	}
	list := f.backups(t)
	if len(list) != 2 || list[0].ObjectKey != keys[2] || list[1].ObjectKey != keys[1] {
		t.Fatalf("retained backups: %+v", list)
	}
	if f.stored(keys[0]) || !f.stored(keys[1]) || !f.stored(keys[2]) {
		t.Fatal("pruned archive object not deleted, or a retained one was")
	}
}

func TestSoloResumeFromChosenBackupReplacesSandbox(t *testing.T) {
	f := newStopFixture(t)
	f.db.Create(&models.SoloMessage{ID: "m1", SessionID: f.id, Role: "user", Content: "hi"})
	f.setArchive("v1")
	f.stop(t)
	f.resume(t, 0)
	f.setArchive("v2")
	stopped := f.stop(t)
	list := f.backups(t)
	if len(list) != 2 {
		t.Fatalf("backups: %+v", list)
	}

	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 9999); err == nil {
		t.Fatal("resumed from a backup of no session")
	} else {
		wantSoloCode(t, err, "backup_not_found")
	}

	restored := f.resume(t, list[1].ID)
	if restored.SandboxID == stopped.SandboxID || !restored.NeedsPreamble {
		t.Fatalf("chosen backup must restore into a new sandbox: %+v", restored)
	}
	if got := f.exec.restoredArchives(); len(got) != 1 || got[0] != "v1" {
		t.Fatalf("restored archives: %q", got)
	}
	if _, err := f.sbx.Get(stopped.SandboxID); err == nil {
		t.Fatal("replaced sandbox kept")
	}
	if !f.hasSystem(t, "从所选备份恢复") {
		t.Fatal("restore notice missing")
	}
}

func TestSoloResumeFromUnreachableBackupKeepsSandbox(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	f.resume(t, 0)
	stopped := f.stop(t)
	older := f.backups(t)[1]
	f.db.Model(&models.SoloBackup{}).Where("id = ?", older.ID).Update("store", solobackup.KindS3)

	if _, err := f.solo.Resume(context.Background(), f.id, "alice", older.ID); err != nil {
		t.Fatal(err)
	}
	failed := f.waitStatus(t, models.SoloStatusStopped)
	if failed.SandboxID != stopped.SandboxID || !strings.Contains(failed.Error, solobackup.KindS3) {
		t.Fatalf("an unrestorable backup must not replace the sandbox: %+v", failed)
	}
	if _, err := f.sbx.Get(stopped.SandboxID); err != nil {
		t.Fatalf("kept sandbox destroyed: %v", err)
	}
}

func TestSoloResumeFromNewestBackupKeepsSandbox(t *testing.T) {
	f := newStopFixture(t)
	stopped := f.stop(t)
	resumed := f.resume(t, f.backups(t)[0].ID)
	if resumed.SandboxID != stopped.SandboxID || len(f.exec.restoredArchives()) != 0 {
		t.Fatalf("choosing the newest backup must resume the existing sandbox: %+v", resumed)
	}
}

func TestSoloScheduledBackup(t *testing.T) {
	f := newStopFixture(t)
	f.solo.SetBackupPolicy(5, time.Hour)
	f.setArchive("scheduled")

	f.solo.runScheduledBackups(context.Background())
	if len(f.backups(t)) != 0 {
		t.Fatal("backed up a session with no activity since it started")
	}

	f.makeDue(t)
	f.fg.SetStatus(f.sandboxName(t, f.row(t).SandboxID), "stopped")
	f.solo.runScheduledBackups(context.Background())
	if len(f.backups(t)) != 0 {
		t.Fatal("backed up a paused sandbox")
	}
	f.fg.SetStatus(f.sandboxName(t, f.row(t).SandboxID), "running")

	f.solo.runScheduledBackups(context.Background())
	list := f.backups(t)
	if len(list) != 1 || list[0].Reason != models.SoloBackupReasonScheduled || list[0].Size != int64(len("scheduled")) {
		t.Fatalf("scheduled backup: %+v", list)
	}
	if row := f.row(t); row.Status != models.SoloStatusRunning || row.BackupKey != list[0].ObjectKey {
		t.Fatalf("session after scheduled backup: %+v", row)
	}

	f.makeDue(t)
	f.solo.runScheduledBackups(context.Background())
	if len(f.backups(t)) != 1 {
		t.Fatal("retried within the backup interval")
	}
}

func TestSoloScheduledBackupWaitsForTurn(t *testing.T) {
	f := newStopFixture(t)
	f.solo.SetBackupPolicy(5, time.Hour)
	busy := newRestoreChat(true)
	f.solo.SetChatterForTest(busy)
	if _, err := f.solo.Send(f.id, "alice", "work", nil); err != nil {
		t.Fatal(err)
	}
	<-busy.started
	f.makeDue(t)
	f.solo.runScheduledBackups(context.Background())
	close(busy.release)
	if len(f.backups(t)) != 0 {
		t.Fatal("backed up during a turn")
	}
}

func TestSoloArchiveFinishedAfterStopIsDiscarded(t *testing.T) {
	f := newStopFixture(t)
	f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("status", models.SoloStatusStopping)
	key := newBackupKey(f.id)
	obj, err := f.store.Put(context.Background(), key, strings.NewReader("late"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.solo.recordBackup(context.Background(), f.id, key, obj, models.SoloBackupReasonScheduled, true); err == nil {
		t.Fatal("recorded an archive of a session that left running")
	}
	if f.stored(key) || len(f.backups(t)) != 0 {
		t.Fatal("superseded archive kept")
	}
}

func TestSoloVanishedSandboxWithBackupIsResumable(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	running := f.resume(t, 0)
	f.solo.onSandboxGone(running.SandboxID)
	row := f.row(t)
	if row.Status != models.SoloStatusStopped || row.SandboxID != 0 || !strings.Contains(row.Error, "备份") {
		t.Fatalf("vanished sandbox with a backup: %+v", row)
	}
	f.resume(t, 0)
	if got := f.exec.restoredArchives(); len(got) != 1 {
		t.Fatalf("restored archives: %q", got)
	}
}

func TestSoloAdoptsLegacyBackup(t *testing.T) {
	db := newTestDB(t)
	at := time.Now().Add(-time.Hour)
	db.Create(&models.SoloSession{ID: "legacy", UserID: "alice", Status: models.SoloStatusStopped,
		BackupStore: solobackup.KindLocal, BackupKey: "solo/legacy/a.tar.gz", BackupSHA256: "abc", BackupSize: 3, BackupAt: &at})
	for range 2 {
		NewSoloService(db, nil, nil, nil)
	}
	var rows []models.SoloBackup
	db.Where("session_id = ?", "legacy").Find(&rows)
	if len(rows) != 1 || rows[0].ObjectKey != "solo/legacy/a.tar.gz" || rows[0].SHA256 != "abc" || !rows[0].CreatedAt.Equal(at) {
		t.Fatalf("legacy backup not adopted once: %+v", rows)
	}
}
