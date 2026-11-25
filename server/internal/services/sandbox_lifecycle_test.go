package services

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func (w *workspaceExec) trimCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.trims)
}

func (w *workspaceExec) setGuestBusy(busy bool) {
	w.mu.Lock()
	w.guestBusy = busy
	w.mu.Unlock()
}

func TestAccessDoesNotExtendModelIdleInterval(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	row := f.recovery(t)
	expireModelIdle(t, f.sbx, row.ID)
	f.sbx.TouchActivity(row.ID) // IDE, preview or page traffic
	f.sbx.sweepOnce(context.Background())
	if got := f.recovery(t); got.Status != "stopped" {
		t.Fatalf("access traffic kept an idle sandbox: %s", got.Status)
	}
}

func TestGuestTurnBlocksReclaimAndRestartsInterval(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	row := f.recovery(t)
	old := expireModelIdle(t, f.sbx, row.ID)
	f.exec.setGuestBusy(true)
	f.sbx.sweepOnce(context.Background())
	got := f.recovery(t)
	if got.Status != "running" || got.ModelActiveAt == nil || !got.ModelActiveAt.After(old) {
		t.Fatalf("guest turn must keep the sandbox and restart its interval: %+v", got)
	}
}

func TestControlPlaneTurnRefreshesPersistedInterval(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	row := f.recovery(t)
	old := expireModelIdle(t, f.sbx, row.ID)
	f.sbx.soloBusy = func(uint) bool { return true }
	f.sbx.sweepOnce(context.Background())
	got := f.recovery(t)
	if got.Status != "running" || got.ModelActiveAt == nil || !got.ModelActiveAt.After(old) {
		t.Fatalf("a running turn must be persisted as model activity: %+v", got)
	}
}

func TestSoloKeepAliveSkipsAutomaticReclaimOnly(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	row := f.recovery(t)
	if _, err := f.solo.SetKeepAlive(context.Background(), f.id, "bob", true); err == nil {
		t.Fatal("another user changed the reclaim policy")
	}
	v, err := f.solo.SetKeepAlive(context.Background(), f.id, "alice", true)
	if err != nil || !v.KeepAlive {
		t.Fatalf("keep alive: %+v %v", v, err)
	}
	expireModelIdle(t, f.sbx, row.ID)
	f.sbx.sweepOnce(context.Background())
	got := f.recovery(t)
	if got.Status != "running" {
		t.Fatalf("keep-alive sandbox was reclaimed: %s", got.Status)
	}
	if f.sbx.view(context.Background(), got).DestroyAt != nil {
		t.Fatal("keep-alive sandbox must not show a reclaim deadline")
	}

	// Turning it back on grants a full interval instead of reclaiming at once.
	if _, err := f.solo.SetKeepAlive(context.Background(), f.id, "alice", false); err != nil {
		t.Fatal(err)
	}
	f.sbx.sweepOnce(context.Background())
	if got := f.recovery(t); got.Status != "running" {
		t.Fatalf("disabling keep-alive reclaimed immediately: %s", got.Status)
	}
	expireModelIdle(t, f.sbx, row.ID)
	f.sbx.sweepOnce(context.Background())
	if got := f.recovery(t); got.Status != "stopped" {
		t.Fatalf("idle sandbox not reclaimed after keep-alive was removed: %s", got.Status)
	}
}

func TestManualStopIgnoresKeepAlive(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	if _, err := f.solo.SetKeepAlive(context.Background(), f.id, "alice", true); err != nil {
		t.Fatal(err)
	}
	if err := f.sbx.Reclaim(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.recovery(t); got.Status != "stopped" {
		t.Fatalf("manual stop must still work: %s", got.Status)
	}
}

func TestRunKeepAlive(t *testing.T) {
	f := newStopFixture(t)
	f.fg.Driver = "e2b"
	row := f.recovery(t)
	if err := f.db.Create(&models.Run{ID: "run-keep", Status: "waiting_human"}).Error; err != nil {
		t.Fatal(err)
	}
	f.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"purpose": "run", "run_id": "run-keep"})
	if err := f.sbx.SetRunKeepAlive("run-keep", true); err != nil {
		t.Fatal(err)
	}
	expireModelIdle(t, f.sbx, row.ID)
	f.sbx.sweepOnce(context.Background())
	if got := f.recovery(t); got.Status != "running" {
		t.Fatalf("keep-alive run sandbox was reclaimed: %s", got.Status)
	}
	old := expireModelIdle(t, f.sbx, row.ID)
	if err := f.sbx.SetRunKeepAlive("run-keep", false); err != nil {
		t.Fatal(err)
	}
	if got := f.recovery(t); got.ModelActiveAt == nil || !got.ModelActiveAt.After(old) {
		t.Fatal("disabling keep-alive must restart the run sandbox interval")
	}
}

func TestMemoryTrimRunsOnIdleE2BGuests(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.fg.Driver = "e2b"
	f.sbx.memoryTrim = MemoryTrimPolicy{IdleAfter: 10 * time.Minute, Interval: 30 * time.Minute, MinCacheMiB: 1024}
	now := time.Now()

	f.sbx.maybeTrimMemory(context.Background(), *row, now)
	if f.exec.trimCount() != 0 {
		t.Fatal("trimmed before the idle delay")
	}
	idle := f.sbx.modelIdleSince(row).Add(11 * time.Minute)
	f.sbx.maybeTrimMemory(context.Background(), *row, idle)
	waitFor(t, "first trim", func() bool { return f.exec.trimCount() == 1 })
	waitFor(t, "trim finished", func() bool {
		f.sbx.mu.Lock()
		defer f.sbx.mu.Unlock()
		return !f.sbx.memoryTrims[row.ID].running
	})
	f.sbx.maybeTrimMemory(context.Background(), *row, idle.Add(5*time.Minute))
	time.Sleep(20 * time.Millisecond)
	if f.exec.trimCount() != 1 {
		t.Fatal("trimmed again inside the interval")
	}
	f.sbx.maybeTrimMemory(context.Background(), *row, idle.Add(31*time.Minute))
	waitFor(t, "second trim", func() bool { return f.exec.trimCount() == 2 })
	f.exec.mu.Lock()
	got := f.exec.trims[0]
	f.exec.mu.Unlock()
	if got != "'sh' '-s' '--' '1024'" {
		t.Fatalf("trim command = %q", got)
	}
}

func TestMemoryTrimSkipsContainersAndDisabledPolicy(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	idle := f.sbx.modelIdleSince(row).Add(time.Hour)

	f.sbx.memoryTrim = MemoryTrimPolicy{}
	f.sbx.maybeTrimMemory(context.Background(), *row, idle)

	f.sbx.memoryTrim = MemoryTrimPolicy{IdleAfter: time.Minute, Interval: time.Minute, MinCacheMiB: 1}
	f.sbx.maybeTrimMemory(context.Background(), *row, idle) // docker driver
	waitFor(t, "driver resolved", func() bool {
		f.sbx.mu.Lock()
		defer f.sbx.mu.Unlock()
		st := f.sbx.memoryTrims[row.ID]
		return st != nil && !st.running && st.kind == "files"
	})
	f.sbx.maybeTrimMemory(context.Background(), *row, idle.Add(time.Hour))
	time.Sleep(20 * time.Millisecond)
	if f.exec.trimCount() != 0 {
		t.Fatal("container guests share the host kernel and must never be trimmed")
	}
}

func TestSweepTrimsIdleKeepAliveSandbox(t *testing.T) {
	f := newStopFixture(t)
	row := f.recovery(t)
	f.fg.Driver = "e2b"
	f.sbx.memoryTrim = MemoryTrimPolicy{IdleAfter: time.Minute, Interval: time.Hour, MinCacheMiB: 1024}
	if _, err := f.solo.SetKeepAlive(context.Background(), f.id, "alice", true); err != nil {
		t.Fatal(err)
	}
	expireModelIdle(t, f.sbx, row.ID)
	f.sbx.sweepOnce(context.Background())
	waitFor(t, "trim", func() bool { return f.exec.trimCount() == 1 })
	if got := f.recovery(t); got.Status != "running" {
		t.Fatalf("keep-alive sandbox reclaimed: %s", got.Status)
	}
}
