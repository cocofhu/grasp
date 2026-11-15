package services

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
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

func TestSoloWake(t *testing.T) {
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
