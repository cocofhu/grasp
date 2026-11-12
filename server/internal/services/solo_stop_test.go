package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
	"github.com/cocofhu/grasp/internal/solobackup"
	"gorm.io/gorm"
)

// workspaceExec stands in for tar inside the guest.
type workspaceExec struct {
	mu        sync.Mutex
	archive   string
	failTar   bool
	restored  []string
	archives  int
	failUntar bool
}

func (w *workspaceExec) install(t *testing.T) {
	t.Helper()
	t.Cleanup(sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		switch {
		case strings.Contains(command, "-czf"):
			w.archives++
			if !strings.Contains(command, "--exclude=node_modules") {
				return nil, errors.New("dependency directories were archived")
			}
			if w.failTar {
				return nil, errors.New("tar: read error")
			}
			return []byte(w.archive), nil
		case strings.Contains(command, "-xzf"):
			b, _ := io.ReadAll(stdin)
			if w.failUntar {
				return []byte("tar: corrupt archive"), errors.New("exit 2")
			}
			w.restored = append(w.restored, string(b))
		}
		return nil, nil
	}))
}

func (w *workspaceExec) restoredArchives() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.restored...)
}

type stopFixture struct {
	solo  *SoloService
	sbx   *SandboxService
	fg    *sandboxtest.FakeGateway
	db    *gorm.DB
	exec  *workspaceExec
	store solobackup.Store
	id    string
}

func newStopFixture(t *testing.T) *stopFixture {
	t.Helper()
	acp, port := fakeACPServer(t)
	t.Cleanup(acp.Close)
	solo, sbx, fg, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	store, err := solobackup.NewLocal(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	solo.SetBackupStore(store)
	solo.SetChatterForTest(newRestoreChat(false))
	exec := &workspaceExec{archive: "workspace-archive-v1"}
	exec.install(t)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	f := &stopFixture{solo: solo, sbx: sbx, fg: fg, db: db, exec: exec, store: store, id: view.ID}
	f.waitStatus(t, models.SoloStatusRunning)
	return f
}

func (f *stopFixture) row(t *testing.T) models.SoloSession {
	t.Helper()
	var row models.SoloSession
	if err := f.db.First(&row, "id = ?", f.id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func (f *stopFixture) waitStatus(t *testing.T, status string) models.SoloSession {
	t.Helper()
	waitFor(t, "session "+status, func() bool { return f.row(t).Status == status })
	return f.row(t)
}

func (f *stopFixture) sandboxName(t *testing.T, id uint) string {
	t.Helper()
	sb, err := f.sbx.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return sb.Name
}

func (f *stopFixture) stop(t *testing.T) models.SoloSession {
	t.Helper()
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	return f.waitStatus(t, models.SoloStatusStopped)
}

func (f *stopFixture) hasSystem(t *testing.T, text string) bool {
	t.Helper()
	msgs, err := f.solo.Messages(f.id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}

func wantSoloCode(t *testing.T, err error, code string) {
	t.Helper()
	var se *SoloError
	if !errors.As(err, &se) || se.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestSoloStopBacksUpThenResumesSameSandbox(t *testing.T) {
	f := newStopFixture(t)
	running := f.row(t)
	name := f.sandboxName(t, running.SandboxID)
	f.db.Create(&models.SoloMessage{ID: "m1", SessionID: f.id, Role: "user", Content: "hi"})

	stopped := f.stop(t)
	if stopped.BackupKey == "" || stopped.BackupStore != solobackup.KindLocal || stopped.BackupSize != int64(len("workspace-archive-v1")) || stopped.BackupAt == nil || stopped.SandboxID != running.SandboxID {
		t.Fatalf("backup not recorded: %+v", stopped)
	}
	rc, err := f.store.Open(context.Background(), stopped.BackupKey)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "workspace-archive-v1" {
		t.Fatalf("archive content: %q", got)
	}
	if st, _ := f.sbx.mgr.ProbeStatus(context.Background(), name); st != "exited" {
		t.Fatalf("sandbox not stopped: %s", st)
	}
	if !f.hasSystem(t, "工作区已备份") {
		t.Fatal("stop notice missing")
	}
	if _, err := f.solo.Send(f.id, "alice", "more", nil); err == nil {
		t.Fatal("message accepted while stopped")
	} else {
		wantSoloCode(t, err, "session_stopped")
	}
	if err := f.sbx.EnsureSoloRunning(context.Background(), running.SandboxID); err == nil {
		t.Fatal("background recovery started a stopped session")
	}
	if active, _ := f.solo.List(context.Background(), "alice", "active"); len(active) != 1 {
		t.Fatalf("stopped session missing from the active list: %d", len(active))
	}
	if n := f.sbx.activeSoloSandboxCount(); n != 0 {
		t.Fatalf("stopped session still counts against the quota: %d", n)
	}

	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err != nil {
		t.Fatal(err)
	}
	resumed := f.waitStatus(t, models.SoloStatusRunning)
	if resumed.SandboxID != running.SandboxID || !resumed.ResumeContextPending || resumed.NeedsPreamble {
		t.Fatalf("cold resume should keep the sandbox and restore dialogue: %+v", resumed)
	}
	if len(f.exec.restoredArchives()) != 0 {
		t.Fatal("existing sandbox was restored from backup")
	}
	waitFor(t, "cold resume notice", func() bool {
		return f.hasSystem(t, "进程已重新启动")
	})

	// A second stop adds an archive and keeps the earlier one.
	f.exec.mu.Lock()
	f.exec.archive = "workspace-archive-v2"
	f.exec.mu.Unlock()
	f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("resume_context_pending", false)
	again := f.stop(t)
	if again.BackupKey == stopped.BackupKey {
		t.Fatal("archive key reused")
	}
	if rc, err := f.store.Open(context.Background(), stopped.BackupKey); err != nil {
		t.Fatalf("previous archive deleted: %v", err)
	} else {
		_ = rc.Close()
	}
	if list, err := f.solo.ListBackups(f.id, "alice"); err != nil || len(list) != 2 || list[0].ObjectKey != again.BackupKey || list[1].Reason != models.SoloBackupReasonStop {
		t.Fatalf("backups not listed newest first: %+v %v", list, err)
	}

	// E2B pauses: the process and its CLI session survive.
	f.fg.Driver = "e2b"
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err != nil {
		t.Fatal(err)
	}
	warm := f.waitStatus(t, models.SoloStatusRunning)
	if warm.ResumeContextPending || warm.NeedsPreamble {
		t.Fatalf("warm resume should not replay dialogue: %+v", warm)
	}
	if !f.hasSystem(t, "进程和工作区保持停止前的状态") {
		t.Fatal("warm resume notice missing")
	}

	if _, err := f.solo.Close(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{stopped.BackupKey, again.BackupKey} {
		if rc, err := f.store.Open(context.Background(), key); err == nil {
			_ = rc.Close()
			t.Fatal("close kept an archive")
		}
	}
	var left int64
	f.db.Model(&models.SoloBackup{}).Where("session_id = ?", f.id).Count(&left)
	if left != 0 {
		t.Fatalf("close kept %d backup records", left)
	}
	if closed := f.row(t); closed.BackupKey != "" || closed.BackupAt != nil {
		t.Fatalf("close kept backup fields: %+v", closed)
	}
}

func TestSoloResumeRestoresBackupIntoNewSandbox(t *testing.T) {
	f := newStopFixture(t)
	f.db.Create(&models.SoloMessage{ID: "m1", SessionID: f.id, Role: "user", Content: "hi"})
	stopped := f.stop(t)
	f.fg.SetStatus(f.sandboxName(t, stopped.SandboxID), "")

	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err != nil {
		t.Fatal(err)
	}
	restored := f.waitStatus(t, models.SoloStatusRunning)
	if restored.SandboxID == 0 || restored.SandboxID == stopped.SandboxID || !restored.NeedsPreamble || restored.Restarts != 1 {
		t.Fatalf("restore into a new sandbox: %+v", restored)
	}
	if got := f.exec.restoredArchives(); len(got) != 1 || got[0] != "workspace-archive-v1" {
		t.Fatalf("restored archives: %q", got)
	}
	if _, err := f.sbx.Get(stopped.SandboxID); err == nil {
		t.Fatal("stale sandbox row kept")
	}
	if env := f.fg.LastCreateEnv(); env["GIT_REPOS"] != "" && env["GIT_REPOS"] != "[]" {
		t.Fatalf("restored workspace was cloned again: %q", env["GIT_REPOS"])
	}
	if !f.hasSystem(t, "从备份恢复工作区") {
		t.Fatal("restore notice missing")
	}
}

func TestSoloRestoreFailureKeepsBackup(t *testing.T) {
	f := newStopFixture(t)
	stopped := f.stop(t)
	f.fg.SetStatus(f.sandboxName(t, stopped.SandboxID), "")
	f.db.Model(&models.SoloBackup{}).Where("object_key = ?", stopped.BackupKey).Update("sha256", strings.Repeat("0", 64))

	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restore failure", func() bool {
		row := f.row(t)
		return row.Status == models.SoloStatusStopped && strings.Contains(row.Error, "checksum")
	})
	failed := f.row(t)
	if failed.SandboxID != 0 || failed.BackupKey != stopped.BackupKey {
		t.Fatalf("failed restore must keep the archive and unbind: %+v", failed)
	}

	f.db.Model(&models.SoloBackup{}).Where("object_key = ?", stopped.BackupKey).Update("sha256", stopped.BackupSHA256)
	f.exec.mu.Lock()
	f.exec.failUntar = true
	f.exec.mu.Unlock()
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "untar failure", func() bool {
		row := f.row(t)
		return row.Status == models.SoloStatusStopped && strings.Contains(row.Error, "corrupt archive")
	})
}

func TestSoloResumeWithoutBackupOrStore(t *testing.T) {
	f := newStopFixture(t)
	stopped := f.stop(t)
	f.fg.SetStatus(f.sandboxName(t, stopped.SandboxID), "")
	f.db.Model(&models.SoloBackup{}).Where("session_id = ?", f.id).Update("store", "s3")
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "store mismatch", func() bool {
		row := f.row(t)
		return row.Status == models.SoloStatusStopped && strings.Contains(row.Error, "s3")
	})
	f.db.Where("session_id = ?", f.id).Delete(&models.SoloBackup{})
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "missing backup", func() bool {
		row := f.row(t)
		return row.Status == models.SoloStatusStopped && strings.Contains(row.Error, "没有可用的工作区备份")
	})
}

func TestSoloStopFailuresKeepSandboxRunning(t *testing.T) {
	f := newStopFixture(t)
	f.exec.mu.Lock()
	f.exec.failTar = true
	f.exec.mu.Unlock()
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "archive failure", func() bool {
		row := f.row(t)
		return row.Status == models.SoloStatusRunning && strings.Contains(row.Error, "停止失败")
	})
	if row := f.row(t); row.BackupKey != "" {
		t.Fatalf("failed archive recorded: %+v", row)
	}

	f.exec.mu.Lock()
	f.exec.failTar = false
	f.exec.archive = strings.Repeat("x", 2<<20)
	f.exec.mu.Unlock()
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "oversized archive", func() bool {
		row := f.row(t)
		return row.Status == models.SoloStatusRunning && strings.Contains(row.Error, "超过备份上限")
	})

	f.exec.mu.Lock()
	f.exec.archive = "small"
	f.exec.mu.Unlock()
	f.fg.FailStop = true
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "gateway stop failure", func() bool {
		row := f.row(t)
		return row.Status == models.SoloStatusRunning && strings.Contains(row.Error, "停止失败")
	})
	if row := f.row(t); row.BackupKey != "" {
		t.Fatalf("archive of a failed stop recorded: %+v", row)
	}
}

func TestSoloStopResumeGuards(t *testing.T) {
	f := newStopFixture(t)
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err == nil {
		t.Fatal("resumed a running session")
	} else {
		wantSoloCode(t, err, "session_not_stopped")
	}
	if _, err := f.solo.Stop(context.Background(), f.id, "bob"); err == nil {
		t.Fatal("stopped another user's session")
	}

	busy := newRestoreChat(true)
	f.solo.SetChatterForTest(busy)
	if _, err := f.solo.Send(f.id, "alice", "work", nil); err != nil {
		t.Fatal(err)
	}
	<-busy.started
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err == nil {
		t.Fatal("stopped during a reply")
	} else {
		wantSoloCode(t, err, "session_busy")
	}
	close(busy.release)
	waitFor(t, "idle", func() bool { return !f.solo.lookup(f.id).sess.Snapshot().Busy })

	store := f.solo.backups
	f.solo.SetBackupStore(nil)
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err == nil {
		t.Fatal("stopped without backup storage")
	} else {
		wantSoloCode(t, err, "backup_unavailable")
	}
	f.solo.SetBackupStore(store)

	f.stop(t)
	if v, err := f.solo.Stop(context.Background(), f.id, "alice"); err != nil || v.Status != models.SoloStatusStopped {
		t.Fatalf("repeated stop: %+v %v", v, err)
	}
	if err := f.solo.Delete(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err == nil {
		t.Fatal("stopped a deleted session")
	}
}

func TestSoloStopRejectsClosedSessionAndQuota(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	f.sbx.SetMaxSoloSandboxes(1)
	other := models.Sandbox{Name: "busy", Purpose: SandboxPurposeSolo, Status: "running", ThreadID: "other"}
	f.db.Create(&other)
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); err == nil {
		t.Fatal("resume ignored the Solo quota")
	} else {
		wantSoloCode(t, err, "solo_limit")
	}
	f.db.Delete(&other)
	if _, err := f.solo.Close(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err == nil {
		t.Fatal("stopped a closed session")
	} else {
		wantSoloCode(t, err, "session_not_running")
	}
	var se *SoloError
	if _, err := f.solo.Resume(context.Background(), f.id, "alice", 0); !errors.As(err, &se) || se.Status != http.StatusConflict {
		t.Fatalf("resume closed: %v", err)
	}
}

func TestSoloInterruptedStopReturnsToRunning(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.SoloSession{ID: "s", UserID: "alice", Status: models.SoloStatusStopping})
	NewSoloService(db, nil, nil, nil)
	var row models.SoloSession
	db.First(&row, "id = ?", "s")
	if row.Status != models.SoloStatusRunning || row.Error == "" {
		t.Fatalf("interrupted stop: %+v", row)
	}
}
