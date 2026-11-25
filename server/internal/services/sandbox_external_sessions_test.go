package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestExternalSessionCountAndIdempotentRelease(t *testing.T) {
	s := &SandboxService{}
	if s.ExternalSessions(1) != 0 {
		t.Fatal("fresh service reports a session")
	}
	a := s.AcquireExternalSession(1)
	b := s.AcquireExternalSession(1)
	if got := s.ExternalSessions(1); got != 2 {
		t.Fatalf("sessions = %d, want 2", got)
	}
	a()
	a()
	if got := s.ExternalSessions(1); got != 1 {
		t.Fatalf("double release dropped another session: %d", got)
	}
	if _, ended := s.externalEnded[1]; ended {
		t.Fatal("idle interval restarted while a session is still open")
	}
	b()
	if s.ExternalSessions(1) != 0 || s.externalBusy(1) {
		t.Fatal("last release left the sandbox busy")
	}
	if s.externalEnded[1].IsZero() {
		t.Fatal("last release must record when the idle interval restarts")
	}
	var nilSvc *SandboxService
	nilSvc.AcquireExternalSession(1)()
	s.AcquireExternalSession(0)()
	if nilSvc.ExternalSessions(1) != 0 || s.ExternalSessions(0) != 0 {
		t.Fatal("nil service or zero id must be a no-op")
	}
}

func TestExternalSessionBlocksIdleReclaimAndTrim(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	f.sbx.memoryTrim = MemoryTrimPolicy{IdleAfter: time.Minute, Interval: time.Minute, MinCacheMiB: 1}
	row := f.recovery(t)
	old := expireModelIdle(t, f.sbx, row.ID)
	release := f.sbx.AcquireExternalSession(row.ID)

	f.sbx.sweepOnce(context.Background())
	time.Sleep(20 * time.Millisecond)
	if got := f.recovery(t); got.Status != "running" {
		t.Fatalf("an open IDE connection did not keep the sandbox: %s", got.Status)
	}
	if f.exec.trimCount() != 0 {
		t.Fatal("memory was trimmed under an open IDE connection")
	}
	if err := f.sbx.reclaimLocked(context.Background(), row.ID, true); err == nil {
		t.Fatal("automatic reclaim ignored the open connection")
	}

	f.sbx.recoveryKinds[row.ID] = "snapshot"
	view, err := f.sbx.GetView(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.DestroyAt != nil {
		t.Fatal("no reclaim countdown while a connection is open")
	}

	release()
	if since := f.sbx.modelIdleSince(f.recovery(t)); !since.After(old) {
		t.Fatal("closing the last connection must restart the idle interval")
	}
	view, err = f.sbx.GetView(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.DestroyAt == nil || !view.DestroyAt.After(time.Now()) {
		t.Fatalf("countdown should resume from the disconnect: %v", view.DestroyAt)
	}
	f.sbx.sweepOnce(context.Background())
	if got := f.recovery(t); got.Status != "running" {
		t.Fatalf("sandbox reclaimed right after the IDE disconnected: %s", got.Status)
	}
}

func TestSoloViewReportsLifecycleAndConnections(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.sbx.MarkModelActivity(row.ID)
	f.sbx.recoveryKinds[row.ID] = "snapshot"

	view, err := f.solo.Get(context.Background(), f.id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	info := view.Sandbox
	if info == nil || info.RecoveryKind != "snapshot" || info.ModelActiveAt == nil || info.ExternalSessions != 0 {
		t.Fatalf("lifecycle fields missing: %+v", info)
	}
	if info.DestroyAt == nil || !info.DestroyAt.After(*info.ModelActiveAt) {
		t.Fatalf("live view must carry the reclaim deadline: %+v", info)
	}

	release := f.sbx.AcquireExternalSession(row.ID)
	defer release()
	view, err = f.solo.Get(context.Background(), f.id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Sandbox.ExternalSessions != 1 || view.Sandbox.DestroyAt != nil {
		t.Fatalf("an open connection must show and suspend the countdown: %+v", view.Sandbox)
	}
}

func TestDialSSHRefusesMissingAndUserStoppedSandboxes(t *testing.T) {
	f := newStopFixture(t)
	if _, err := f.sbx.DialSSH(context.Background(), 99999); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("missing sandbox = %v", err)
	}
	row := f.recovery(t)
	for status, want := range map[string]string{
		models.SoloStatusStopped: "手动停止",
		models.SoloStatusClosed:  "已关闭",
	} {
		if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.sbx.DialSSH(context.Background(), row.ID); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s session = %v", status, err)
		}
	}
	if err := f.sbx.resumeForAccess(context.Background(), &models.Sandbox{ID: row.ID, Name: row.Name, Purpose: SandboxPurposeSolo}); err != nil &&
		strings.Contains(err.Error(), "已关闭") {
		t.Fatal("the web IDE path must keep its previous closed-session behavior")
	}
	noMgr := &SandboxService{db: f.db}
	if _, err := noMgr.DialSSH(context.Background(), row.ID); err == nil {
		t.Fatal("a service without a manager cannot dial")
	}
}

func TestExternalSessionOpenedDuringSaveAbortsAutomaticRelease(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	row := f.recovery(t)
	idleSince := f.sbx.modelIdleSince(row)
	sb, err := f.sbx.soloHandle(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.sbx.recheckBeforeRelease(context.Background(), row, sb, idleSince, true); err != nil {
		t.Fatalf("idle recheck failed without a connection: %v", err)
	}
	release := f.sbx.AcquireExternalSession(row.ID)
	defer release()
	if err := f.sbx.recheckBeforeRelease(context.Background(), row, sb, idleSince, true); err == nil {
		t.Fatal("a connection opened while saving must abort the automatic release")
	}
	if err := f.sbx.recheckBeforeRelease(context.Background(), row, sb, idleSince, false); err != nil {
		t.Fatalf("manual stop must not be blocked by a connection: %v", err)
	}
}
