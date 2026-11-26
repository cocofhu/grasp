package services

import (
	"context"
	"fmt"
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
	if since := f.sbx.reclaimIdleSince(f.recovery(t)); !since.After(old) {
		t.Fatal("closing the last connection must restart the reclaim countdown")
	}
	if since := f.sbx.modelIdleSince(f.recovery(t)); !since.Equal(old) {
		t.Fatal("an IDE disconnect is not model activity")
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
	if _, _, err := f.sbx.DialSSH(context.Background(), 99999); err == nil || !strings.Contains(err.Error(), "不存在") {
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
		if _, _, err := f.sbx.DialSSH(context.Background(), row.ID); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s session = %v", status, err)
		}
	}
	if err := f.sbx.resumeForAccess(context.Background(), &models.Sandbox{ID: row.ID, Name: row.Name, Purpose: SandboxPurposeSolo}); err != nil &&
		strings.Contains(err.Error(), "已关闭") {
		t.Fatal("the web IDE path must keep its previous closed-session behavior")
	}
	noMgr := &SandboxService{db: f.db}
	if _, _, err := noMgr.DialSSH(context.Background(), row.ID); err == nil {
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

func TestManualStopSucceedsWhenTheLastIDEDisconnectsDuringSave(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	row := f.recovery(t)
	release := f.sbx.AcquireExternalSession(row.ID)
	idleSince := f.sbx.modelIdleSince(row)
	sb, err := f.sbx.soloHandle(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	release() // the user closes the IDE while the archive is written
	if err := f.sbx.recheckBeforeRelease(context.Background(), row, sb, idleSince, false); err != nil {
		t.Fatalf("closing an IDE failed the manual stop: %v", err)
	}
}

func TestAuthorizeSSHByOwnerAndAdmin(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	if err := f.sbx.AuthorizeSSH("alice", row.ID); err != nil {
		t.Fatalf("the session owner was refused: %v", err)
	}
	denied := f.sbx.AuthorizeSSH("bob", row.ID)
	missing := f.sbx.AuthorizeSSH("bob", 99999)
	if denied == nil || missing == nil {
		t.Fatal("another user entered a Solo sandbox")
	}
	if denied.Error() != fmt.Sprintf("沙箱 %d 不存在或无权访问", row.ID) || !strings.Contains(missing.Error(), "不存在或无权访问") {
		t.Fatalf("forbidden and missing sandboxes must look the same: %q / %q", denied, missing)
	}
	if f.sbx.AuthorizeSSH("", row.ID) == nil {
		t.Fatal("an empty user name was authorized")
	}

	run := models.Sandbox{Name: "wf-run-box", Status: "running", Purpose: "run", RunID: "run-1"}
	if err := f.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if f.sbx.AuthorizeSSH("alice", run.ID) == nil {
		t.Fatal("workflow sandboxes have no owner and must be admin-only")
	}
	f.sbx.SetSSHAdminLookup(func(u string) bool { return u == "root-admin" })
	if err := f.sbx.AuthorizeSSH("root-admin", run.ID); err != nil {
		t.Fatalf("admin refused: %v", err)
	}
	if err := f.sbx.AuthorizeSSH("root-admin", row.ID); err != nil {
		t.Fatalf("admin refused a Solo sandbox: %v", err)
	}
	if f.sbx.AuthorizeSSH("bob", row.ID) == nil {
		t.Fatal("the admin lookup let a regular user in")
	}

	if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("user_id", "carol").Error; err != nil {
		t.Fatal(err)
	}
	if f.sbx.AuthorizeSSH("alice", row.ID) == nil {
		t.Fatal("a reassigned session kept the previous owner's access")
	}
}

func TestDialSSHWaitsForAnIdleSaveHoldingTheRecoveryLock(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	unlock, err := f.sbx.lockSoloRecovery(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := f.sbx.DialSSH(ctx, row.ID); err == nil {
		t.Fatal("dialing went ahead while a save held the recovery lock")
	}
	if f.sbx.ExternalSessions(row.ID) != 0 {
		t.Fatal("a hold was taken without a connection")
	}
	unlock()
}

func TestDialSSHRechecksManualStopAfterTakingTheLock(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	unlock, err := f.sbx.lockSoloRecovery(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, err := f.sbx.DialSSH(context.Background(), row.ID)
		result <- err
	}()
	time.Sleep(20 * time.Millisecond) // DialSSH is now waiting for the lock
	// A manual stop finishes while the dial waits.
	if err := f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Update("status", models.SoloStatusStopped).Error; err != nil {
		t.Fatal(err)
	}
	unlock()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "手动停止") {
			t.Fatalf("a dial waiting on the lock woke a manually stopped sandbox: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dial did not finish")
	}
	if f.sbx.ExternalSessions(row.ID) != 0 {
		t.Fatal("a refused dial took a hold")
	}
}
