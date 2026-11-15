package services

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

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
		case strings.Contains(command, "'python3' '-'"):
			return []byte(`{"busy":false}`), nil
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

func (f *stopFixture) stop(t *testing.T) models.SoloSession {
	t.Helper()
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	return f.waitStatus(t, models.SoloStatusStopped)
}

func wantSoloCode(t *testing.T, err error, code string) {
	t.Helper()
	var se *SoloError
	if !errors.As(err, &se) || se.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func (f *stopFixture) recovery(t *testing.T) *models.Sandbox {
	t.Helper()
	row, err := f.sbx.Get(f.row(t).SandboxID)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSoloStopSavesBeforeReleaseAndRestoresLatest(t *testing.T) {
	f := newStopFixture(t)
	f.db.Create(&models.SoloMessage{ID: "m1", SessionID: f.id, Role: "user", Content: "hi"})
	f.stop(t)
	first := f.recovery(t)
	if first.Status != "stopped" || first.RecoveryKind != "files" || len(first.RecoveryPoints) != 1 {
		t.Fatalf("recovery = %+v", first)
	}
	oldKey := first.RecoveryPoints[0].Key
	if _, err := f.solo.Resume(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(t, models.SoloStatusRunning)
	if got := f.exec.restoredArchives(); len(got) != 1 || got[0] != "workspace-archive-v1" {
		t.Fatalf("restored = %q", got)
	}
	if !f.row(t).ResumeContextPending {
		t.Fatal("cold restore must replay conversation context")
	}
	f.exec.mu.Lock()
	f.exec.archive = "workspace-archive-v2"
	f.exec.mu.Unlock()
	f.stop(t)
	latest := f.recovery(t)
	if len(latest.RecoveryPoints) != 1 || latest.RecoveryPoints[0].Key == oldKey {
		t.Fatalf("retention = %+v", latest.RecoveryPoints)
	}
	if rc, err := f.store.Open(context.Background(), oldKey); err == nil {
		_ = rc.Close()
		t.Fatal("old stored object was not physically deleted")
	}
	if _, err := f.solo.Close(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sbx.Get(latest.ID); err == nil {
		t.Fatal("cancel kept sandbox row")
	}
	if rc, err := f.store.Open(context.Background(), latest.RecoveryPoints[0].Key); err == nil {
		_ = rc.Close()
		t.Fatal("cancel kept backup object")
	}
}

func TestE2BStopUsesSnapshotWithoutFileStorage(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	f.solo.SetBackupStore(nil)
	f.stop(t)
	row := f.recovery(t)
	if row.RecoveryKind != "snapshot" || len(row.RecoveryPoints) != 0 {
		t.Fatalf("snapshot recovery = %+v", row)
	}
	f.exec.mu.Lock()
	count := f.exec.archives
	f.exec.mu.Unlock()
	if count != 0 {
		t.Fatal("E2B duplicated its snapshot with a file archive")
	}
	if _, err := f.solo.Resume(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	if f.waitStatus(t, models.SoloStatusRunning).ResumeContextPending {
		t.Fatal("warm restore replayed conversation")
	}
}

func TestSoloFailedSaveDoesNotReleaseSandbox(t *testing.T) {
	f := newStopFixture(t)
	f.exec.mu.Lock()
	f.exec.failTar = true
	f.exec.mu.Unlock()
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "save failure", func() bool { r := f.row(t); return r.Status == models.SoloStatusRunning && r.Error != "" })
	if row := f.recovery(t); row.Status != "running" || len(row.RecoveryPoints) != 1 || !row.RecoveryPoints[0].Deleting {
		t.Fatalf("failed save released data: %+v", row)
	}
	if status, _ := f.sbx.mgr.ProbeStatus(context.Background(), f.recovery(t).Name); status != "running" {
		t.Fatal("provider stopped after save failure")
	}
}

func TestSoloRestoreCorruptionKeepsRecoveryAndDoesNotStart(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	row.RecoveryPoints[0].SHA256 = strings.Repeat("0", 64)
	if err := f.sbx.persistRecoveryPoints(row); err != nil {
		t.Fatal(err)
	}
	if _, err := f.solo.Resume(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "checksum failure", func() bool {
		r := f.row(t)
		return r.Status == models.SoloStatusStopped && strings.Contains(r.Error, "checksum")
	})
	if status, _ := f.sbx.mgr.ProbeStatus(context.Background(), row.Name); status != "exited" {
		t.Fatalf("corrupt archive started a workload: %s", status)
	}
	if len(f.recovery(t).RecoveryPoints) != 1 {
		t.Fatal("failed restore deleted recovery point")
	}
}

func TestSoloFailedRestoreRetainsCapacityAndRecovery(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	f.exec.mu.Lock()
	f.exec.failUntar = true
	f.exec.mu.Unlock()
	if _, err := f.solo.Resume(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "extract failure", func() bool {
		r := f.row(t)
		return r.Status == models.SoloStatusStopped && strings.Contains(r.Error, "corrupt archive")
	})
	row := f.recovery(t)
	if row.Status != "error" || len(row.RecoveryPoints) != 1 {
		t.Fatalf("partial restore did not remain allocated: %+v", row)
	}
	cap, err := f.sbx.Capacity()
	if err != nil || cap.Used != 1 {
		t.Fatalf("capacity = %+v %v", cap, err)
	}
}

func TestSandboxUnifiedRetentionKeepsSuccessfulPoints(t *testing.T) {
	f := newStopFixture(t)
	f.sbx.SetBackupKeep(2)
	keys := []string{}
	for i := 0; i < 3; i++ {
		f.stop(t)
		keys = append(keys, f.recovery(t).RecoveryPoints[0].Key)
		if i < 2 {
			if _, err := f.solo.Resume(context.Background(), f.id, "alice"); err != nil {
				t.Fatal(err)
			}
			f.waitStatus(t, models.SoloStatusRunning)
		}
	}
	row := f.recovery(t)
	if len(row.RecoveryPoints) != 2 || row.RecoveryPoints[0].Key != keys[2] || row.RecoveryPoints[1].Key != keys[1] {
		t.Fatalf("retention = %+v", row.RecoveryPoints)
	}
	if rc, err := f.store.Open(context.Background(), keys[0]); err == nil {
		_ = rc.Close()
		t.Fatal("oldest archive remains")
	}
}

func TestSoloManualStopProtectsQueuedTurn(t *testing.T) {
	f := newStopFixture(t)
	busy := newRestoreChat(true)
	f.solo.SetChatterForTest(busy)
	if _, err := f.solo.Send(f.id, "alice", "work", nil); err != nil {
		t.Fatal(err)
	}
	<-busy.started
	if _, err := f.solo.Send(f.id, "alice", "next", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.solo.Stop(context.Background(), f.id, "alice"); err == nil {
		t.Fatal("active work was stopped")
	} else {
		wantSoloCode(t, err, "session_busy")
	}
	close(busy.release)
}

func TestUnifiedIdleReclaimCoversEveryPurpose(t *testing.T) {
	for _, purpose := range []string{"test", "solo", "run"} {
		t.Run(purpose, func(t *testing.T) {
			f := newStopFixture(t)
			row := f.recovery(t)
			old := time.Now().Add(-2 * time.Hour)
			f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"purpose": purpose, "last_active_at": old})
			f.sbx.SetIdleTTL(time.Hour)
			f.sbx.sweepOnce(context.Background())
			if got, _ := f.sbx.Get(row.ID); got.Status != "stopped" || len(got.RecoveryPoints) != 1 {
				t.Fatalf("idle %s not saved: %+v", purpose, got)
			}
		})
	}
}
