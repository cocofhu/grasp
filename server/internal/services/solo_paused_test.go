package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

type pausedFixture struct {
	ds   *dockerState
	svc  *SandboxService
	solo *SoloService
	row  models.Sandbox
}

func newPausedFixture(t *testing.T, sessionStatus string) pausedFixture {
	t.Helper()
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	solo := NewSoloService(db, svc, svc.skills, NewProjectService(db))
	row := models.Sandbox{Name: "paused-resident", Purpose: SandboxPurposeSolo, Profile: "agentA", Status: "running", ThreadID: "session", Token: "token"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "session", AgentName: "agentA", UserID: "alice", Status: sessionStatus, SandboxID: row.ID}).Error; err != nil {
		t.Fatal(err)
	}
	return pausedFixture{ds: ds, svc: svc, solo: solo, row: row}
}

func statusFrames(ch <-chan chatsession.Event) int {
	n := 0
	for {
		select {
		case ev := <-ch:
			if ev.Event == SoloEventStatus {
				n++
			}
		default:
			return n
		}
	}
}

func TestSoloViewReportsProviderPause(t *testing.T) {
	cases := []struct {
		name, session, container string
		failProbe, want          bool
	}{
		{"stopped", models.SoloStatusRunning, "stopped", false, true},
		{"running", models.SoloStatusRunning, "running", false, false},
		{"probe failure", models.SoloStatusRunning, "stopped", true, false},
		{"user stopped session", models.SoloStatusStopped, "stopped", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPausedFixture(t, tc.session)
			f.ds.setStatus(f.row.Name, tc.container)
			f.ds.fg.FailStatus = tc.failProbe
			v, err := f.solo.Get(context.Background(), "session", "alice")
			if err != nil || v.Sandbox == nil {
				t.Fatalf("view: %+v %v", v, err)
			}
			if v.Sandbox.Paused != tc.want {
				t.Fatalf("paused = %v, want %v (container %q)", v.Sandbox.Paused, tc.want, v.Sandbox.ContainerStatus)
			}
			listed, err := f.solo.List(context.Background(), "alice", "")
			if err != nil || len(listed) != 1 || listed[0].Sandbox == nil || listed[0].Sandbox.Paused {
				t.Fatalf("list views must not probe pause: %+v %v", listed, err)
			}
		})
	}
}

func TestSoloSweepPublishesPauseOnceAndRecoveryClearsIt(t *testing.T) {
	f := newPausedFixture(t, models.SoloStatusRunning)
	viewer, stop, err := f.solo.Subscribe("session", "alice", -1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	<-viewer

	f.ds.setStatus(f.row.Name, "stopped")
	f.svc.ReleaseVanishedSoloSandboxes(context.Background())
	f.svc.ReleaseVanishedSoloSandboxes(context.Background())
	if n := statusFrames(viewer); n != 1 {
		t.Fatalf("paused sweeps published %d status frames, want 1", n)
	}
	if _, err := f.svc.Get(f.row.ID); err != nil {
		t.Fatalf("paused sandbox was released: %v", err)
	}
	if st := f.svc.mgr.Status(context.Background(), f.row.Name); st != "exited" {
		t.Fatalf("sweep resumed the paused sandbox: %s", st)
	}

	// A turn wakes the resident through the same path as waitSandbox.
	if err := f.svc.EnsureSoloRunning(context.Background(), f.row.ID); err != nil {
		t.Fatal(err)
	}
	if n := statusFrames(viewer); n != 1 {
		t.Fatalf("recovery published %d status frames, want 1", n)
	}
	if err := f.svc.EnsureSoloRunning(context.Background(), f.row.ID); err != nil {
		t.Fatal(err)
	}
	if n := statusFrames(viewer); n != 0 {
		t.Fatalf("a running resident published %d status frames", n)
	}
}

func TestSoloUndetectedPauseStillPublishesOnWake(t *testing.T) {
	f := newPausedFixture(t, models.SoloStatusRunning)
	viewer, stop, err := f.solo.Subscribe("session", "alice", -1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	<-viewer
	f.ds.setStatus(f.row.Name, "stopped")
	if err := f.svc.EnsureSoloRunning(context.Background(), f.row.ID); err != nil {
		t.Fatal(err)
	}
	if n := statusFrames(viewer); n != 2 {
		t.Fatalf("wake published %d status frames, want paused then recovered", n)
	}
}

func TestSoloTurnRetriesFailedWake(t *testing.T) {
	prev := soloWakeRetryDelay
	soloWakeRetryDelay = time.Millisecond
	t.Cleanup(func() { soloWakeRetryDelay = prev })

	t.Run("a transient wake failure does not fail the turn", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, "stopped")
		f.ds.fg.FailStartTimes = 1
		id, _, err := f.solo.acquire("session").waitSandbox(context.Background())
		if err != nil {
			t.Fatalf("first message failed on a transient wake error: %v", err)
		}
		if id != f.row.ID {
			t.Fatalf("sandbox = %d, want %d", id, f.row.ID)
		}
		if st := f.svc.mgr.Status(context.Background(), f.row.Name); st != "running" {
			t.Fatalf("sandbox not resumed: %s", st)
		}
	})
	t.Run("a persistent wake failure still fails the turn", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, "stopped")
		f.ds.fg.FailStart = true
		if _, _, err := f.solo.acquire("session").waitSandbox(context.Background()); err == nil {
			t.Fatal("turn waited forever on a sandbox that cannot resume")
		}
		if !f.ds.fg.Has(f.row.Name) {
			t.Fatal("failed wake destroyed the sandbox")
		}
	})
}

func TestSoloWakeWaitsForBridge(t *testing.T) {
	t.Run("a resumed resident waits for its bridge", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, "stopped")
		probed := false
		f.svc.soloBridgeReady = func(ctx context.Context, sb *sandbox.Sandbox) error {
			if sb.Password != f.row.Token {
				t.Errorf("bridge probe password = %q, want the sandbox token", sb.Password)
			}
			probed = true
			return nil
		}
		if err := f.svc.EnsureSoloRunning(context.Background(), f.row.ID); err != nil {
			t.Fatal(err)
		}
		if !probed {
			t.Fatal("wake did not wait for the bridge")
		}
		row, _ := f.svc.Get(f.row.ID)
		if row.Status != "running" {
			t.Fatalf("status after ready bridge = %q", row.Status)
		}
	})
	t.Run("a bridge that never serves fails the wake", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, "stopped")
		f.svc.soloBridgeReady = func(context.Context, *sandbox.Sandbox) error { return errors.New("ACP not ready") }
		if err := f.svc.EnsureSoloRunning(context.Background(), f.row.ID); err == nil {
			t.Fatal("wake reported success before the bridge served")
		}
		if !f.ds.fg.Has(f.row.Name) {
			t.Fatal("failed wake destroyed the sandbox")
		}
	})
	t.Run("an already running resident verifies its bridge", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, "running")
		f.svc.soloBridgeReady = func(context.Context, *sandbox.Sandbox) error {
			return nil
		}
		if err := f.svc.EnsureSoloRunning(context.Background(), f.row.ID); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSoloWake(t *testing.T) {
	t.Run("waits for a pending provider pause without releasing the original sandbox", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, sandbox.StatusPausePending)
		v, err := f.solo.Get(t.Context(), "session", "alice")
		if err != nil || v.Sandbox == nil || v.Sandbox.ContainerStatus != sandbox.StatusPausePending || v.Sandbox.Paused {
			t.Fatalf("pending view = %+v, %v", v, err)
		}
		_, err = f.solo.Wake(t.Context(), "session", "alice")
		var se *SoloError
		if !errors.As(err, &se) || se.Status != http.StatusConflict || se.Code != sandbox.StatusPausePending {
			t.Fatalf("pending wake = %v", err)
		}
		f.svc.ReleaseVanishedSoloSandboxes(t.Context())
		f.svc.ReconcileOnStartup(t.Context(), nil)
		if row, err := f.svc.Get(f.row.ID); err != nil || row.Status != "running" || !f.ds.fg.Has(f.row.Name) {
			t.Fatalf("pending sandbox after reconcile = %+v, %v", row, err)
		}
		f.ds.setStatus(f.row.Name, "stopped")
		v, err = f.solo.Wake(t.Context(), "session", "alice")
		if err != nil || v.Sandbox == nil || v.Sandbox.ID != f.row.ID || v.Sandbox.ContainerStatus != "running" {
			t.Fatalf("wake after durable pause = %+v, %v", v, err)
		}
	})
	t.Run("resumes the same sandbox", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, "stopped")
		v, err := f.solo.Wake(context.Background(), "session", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if v.Sandbox == nil || v.Sandbox.ID != f.row.ID || v.Sandbox.Paused {
			t.Fatalf("wake view: %+v", v.Sandbox)
		}
		if st := f.svc.mgr.Status(context.Background(), f.row.Name); st != "running" {
			t.Fatalf("sandbox not resumed: %s", st)
		}
	})
	t.Run("rejects sessions that are not running", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusStopped)
		_, err := f.solo.Wake(context.Background(), "session", "alice")
		var se *SoloError
		if !errors.As(err, &se) || se.Status != http.StatusConflict || se.Code != "session_not_running" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("hides other users' sessions", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		_, err := f.solo.Wake(context.Background(), "session", "bob")
		var se *SoloError
		if !errors.As(err, &se) || se.Status != http.StatusNotFound {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("reports resume failures", func(t *testing.T) {
		f := newPausedFixture(t, models.SoloStatusRunning)
		f.ds.setStatus(f.row.Name, "stopped")
		f.ds.fg.FailStart = true
		_, err := f.solo.Wake(context.Background(), "session", "alice")
		var se *SoloError
		if !errors.As(err, &se) || se.Status != http.StatusBadGateway || se.Code != "wake_failed" || se.Msg == "" {
			t.Fatalf("err = %v", err)
		}
		if !f.ds.fg.Has(f.row.Name) {
			t.Fatal("failed wake destroyed the sandbox")
		}
	})
}
