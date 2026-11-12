package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSoloReconcileRetainsResidentData(t *testing.T) {
	for _, status := range []string{"running", "stopped", "pending", "error", "probe_failure", "missing_record"} {
		t.Run(status, func(t *testing.T) {
			db := newTestDB(t)
			ds := &dockerState{}
			svc := newSandboxService(t, db, ds)
			solo := NewSoloService(db, svc, svc.skills, NewProjectService(db))
			past := time.Now().Add(-time.Hour)
			home := t.TempDir()
			marker := filepath.Join(home, "uncommitted-output.mp4")
			if err := os.WriteFile(marker, []byte("user data"), 0o600); err != nil {
				t.Fatal(err)
			}
			row := models.Sandbox{Name: "resident", Purpose: SandboxPurposeSolo, Status: "running", ThreadID: "session", RunID: "solo-artifacts", Token: "original-token", HomeDir: home, DestroyAt: &past}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			session := models.SoloSession{ID: "session", UserID: "alice", Status: models.SoloStatusRunning, SandboxID: row.ID}
			if err := db.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			if status != "missing_record" {
				ds.setStatus(row.Name, status)
			}
			if status == "probe_failure" {
				ds.setStatus(row.Name, "running")
				ds.fg.FailStatus = true
			}
			svc.ReconcileOnStartup(context.Background(), nil)
			solo.ReconcileSandboxReferences()
			svc.sweepOnce(context.Background())
			kept, err := svc.Get(row.ID)
			if err != nil || kept.Name != row.Name || kept.Token != row.Token || kept.DestroyAt != nil {
				t.Fatalf("resident binding was not retained: %+v %v", kept, err)
			}
			if status != "missing_record" && !ds.fg.Has(row.Name) {
				t.Fatal("gateway sandbox was destroyed")
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "user data" {
				t.Fatalf("data lost: %q %v", data, err)
			}
			if err := db.First(&session, "id = ?", session.ID).Error; err != nil {
				t.Fatal(err)
			}
			if session.Status != models.SoloStatusRunning || session.SandboxID != row.ID {
				t.Fatalf("session binding changed: %+v", session)
			}
		})
	}
}

func TestSoloRecoveryRetriesSameSandbox(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	solo := NewSoloService(db, svc, svc.skills, NewProjectService(db))
	row := models.Sandbox{Name: "stopped-resident", Purpose: SandboxPurposeSolo, Profile: "agentA", Status: "running", ThreadID: "session", Token: "token", RunID: "original-artifacts"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	session := models.SoloSession{ID: "session", AgentName: "agentA", UserID: "alice", Status: models.SoloStatusRunning, SandboxID: row.ID}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "stopped")
	if err := svc.Stop(context.Background(), row.ID); err == nil {
		t.Fatal("generic Stop must not remove a resident's writable layer")
	}
	svc.ReconcileOnStartup(context.Background(), nil)
	ds.fg.FailStart = true
	if err := svc.EnsureSoloRunning(context.Background(), row.ID); err == nil {
		t.Fatal("expected resume failure")
	}
	if !ds.fg.Has(row.Name) {
		t.Fatal("failed resume destroyed container")
	}
	if svc.activeSoloSandboxCount() != 1 {
		t.Fatal("retained recovery failure must still count toward the resident limit")
	}
	ds.fg.FailStart = false
	svc.RecoverSoloSandboxes(context.Background())
	got, err := svc.Get(row.ID)
	if err != nil || got.Name != row.Name || got.Status != "running" || got.Error != "" || got.ACPPort == 0 || got.RunID != row.RunID || got.Token != row.Token || got.DestroyAt != nil {
		t.Fatalf("resume changed identity or failed: %+v %v", got, err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	if session.SandboxID != row.ID || session.Restarts != 0 || !session.ResumeContextPending || session.NeedsPreamble {
		t.Fatalf("resume must retain workspace and request accurate dialogue context: %+v", session)
	}
	if _, err := solo.Close(context.Background(), session.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if ds.fg.Has(row.Name) {
		t.Fatal("explicit Close must release the container")
	}
}

func TestSoloMissingResourceAndOldDanglingReferences(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	solo := NewSoloService(db, svc, svc.skills, NewProjectService(db))
	row := models.Sandbox{Name: "resource-gone", Purpose: SandboxPurposeSolo, Status: "running"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, session := range []models.SoloSession{
		{ID: "resource-gone", SandboxID: row.ID, Status: models.SoloStatusRunning},
		{ID: "old-dangling", SandboxID: 999, Status: models.SoloStatusRunning},
		{ID: "closed-history", SandboxID: 1000, Status: models.SoloStatusClosed},
	} {
		if err := db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
	}
	ds.setStatus(row.Name, "not_found")
	svc.ReconcileOnStartup(context.Background(), nil)
	solo.ReconcileSandboxReferences()
	for _, id := range []string{"resource-gone", "old-dangling"} {
		var session models.SoloSession
		if err := db.First(&session, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if session.Status != models.SoloStatusError || session.SandboxID != 0 || !strings.Contains(session.Error, "重新启动") {
			t.Fatalf("missing sandbox left dangling: %+v", session)
		}
	}
	var closed models.SoloSession
	db.First(&closed, "id = ?", "closed-history")
	if closed.Status != models.SoloStatusClosed {
		t.Fatal("closed history was changed")
	}
}

func TestSoloCorrelatedCreatingSandboxIsProtected(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	row := models.Sandbox{Name: "placeholder", Purpose: SandboxPurposeSolo, Status: "creating", CreatedAt: time.Now().Add(-time.Hour)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus("actual-gateway-id", "stopped")
	ds.fg.SetLabel("actual-gateway-id", sandbox.CorrelationNameKey(), row.Name)
	svc.ReconcileOnStartup(context.Background(), nil)
	if !ds.fg.Has("actual-gateway-id") {
		t.Fatal("orphan sweep destroyed the resident's correlated resource")
	}
	svc.RecoverSoloSandboxes(context.Background())
	adopted, err := svc.Get(row.ID)
	if err != nil || adopted.Name != "actual-gateway-id" || adopted.Status != "running" {
		t.Fatalf("interrupted create did not adopt and resume its original resource: %+v %v", adopted, err)
	}
}

func TestSoloNeverExpiresFromStaleTTL(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	past := time.Now().Add(-time.Hour)
	row := models.Sandbox{Name: "legacy-expiry", Purpose: SandboxPurposeSolo, Status: "running", DestroyAt: &past}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "running")
	svc.sweepOnce(context.Background())
	if _, err := svc.Get(row.ID); err != nil || !ds.fg.Has(row.Name) {
		t.Fatal("stale TTL destroyed resident data")
	}
}

func TestSoloStartingSessionRecoversAndClosedSessionDoesNotRestart(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	NewSoloService(db, svc, svc.skills, NewProjectService(db))
	for _, status := range []string{models.SoloStatusStarting, models.SoloStatusClosed} {
		row := models.Sandbox{Name: status, Purpose: SandboxPurposeSolo, Status: "running", ThreadID: status}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.SoloSession{ID: status, Status: status, SandboxID: row.ID}).Error; err != nil {
			t.Fatal(err)
		}
		ds.setStatus(row.Name, "stopped")
	}
	svc.ReconcileOnStartup(context.Background(), nil)
	svc.RecoverSoloSandboxes(context.Background())
	var starting models.SoloSession
	db.First(&starting, "id = ?", models.SoloStatusStarting)
	if starting.Status != models.SoloStatusRunning || !ds.fg.Has(models.SoloStatusStarting) {
		t.Fatal("interrupted creation did not recover")
	}
	if ds.fg.Has(models.SoloStatusClosed) {
		t.Fatal("closed session was restarted")
	}
}

func TestSoloCloseDuringRecoveryDoesNotResurrectSession(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	solo := NewSoloService(db, svc, svc.skills, NewProjectService(db))
	row := models.Sandbox{Name: "closing-resident", Purpose: SandboxPurposeSolo, Status: "error", ThreadID: "closing"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "closing", UserID: "alice", Status: models.SoloStatusRunning, SandboxID: row.ID}).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "stopped")
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start") {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		ds.fg.Server.Config.Handler.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	defer unblock()
	svc.mgr = sandbox.NewManager(sandbox.NewGatewayClient(proxy.URL, ""), sandbox.ManagerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recovered, closed := make(chan error, 1), make(chan error, 1)
	go func() { recovered <- svc.EnsureSoloRunning(ctx, row.ID) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("recovery did not start")
	}
	go func() { _, err := solo.Close(ctx, "closing", "alice"); closed <- err }()
	waitFor(t, "session closed while resume is blocked", func() bool {
		row, err := solo.Require("closing", "alice")
		return err == nil && row.Status == models.SoloStatusClosed
	})
	// Finishing recovery releases its lock; Close must then remove the original
	// container without ever publishing the session as running again.
	unblock()
	select {
	case err := <-recovered:
		if err == nil {
			t.Fatal("closed recovery succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not stop")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("close did not finish")
	}
	final, err := solo.Require("closing", "alice")
	if err != nil || final.Status != models.SoloStatusClosed || ds.fg.Has(row.Name) {
		t.Fatalf("closed session resurrected: %+v %v", final, err)
	}
}

func TestSoloRunningRowsReleasedOnlyWhenProviderConfirmsAbsence(t *testing.T) {
	for _, status := range []string{"not_found", "stopped", "probe_failure", "missing_record"} {
		t.Run(status, func(t *testing.T) {
			db := newTestDB(t)
			ds := &dockerState{}
			svc := newSandboxService(t, db, ds)
			NewSoloService(db, svc, svc.skills, NewProjectService(db))
			row := models.Sandbox{Name: "resident", Purpose: SandboxPurposeSolo, Status: "running", ThreadID: "session"}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&models.SoloSession{ID: "session", UserID: "alice", Status: models.SoloStatusRunning, SandboxID: row.ID}).Error; err != nil {
				t.Fatal(err)
			}
			switch status {
			case "probe_failure":
				ds.setStatus(row.Name, "running")
				ds.fg.FailStatus = true
			case "missing_record":
			default:
				ds.setStatus(row.Name, status)
			}
			svc.ReleaseVanishedSoloSandboxes(context.Background())
			ds.fg.FailStatus = false
			_, err := svc.Get(row.ID)
			if status == "not_found" {
				if err == nil {
					t.Fatal("confirmed absence kept the sandbox row")
				}
				var session models.SoloSession
				db.First(&session, "id = ?", "session")
				if session.Status != models.SoloStatusError || session.SandboxID != 0 {
					t.Fatalf("session still bound to a vanished sandbox: %+v", session)
				}
				return
			}
			if err != nil {
				t.Fatalf("unconfirmed absence released the sandbox: %v", err)
			}
			if status == "stopped" && svc.mgr.Status(context.Background(), row.Name) != "exited" {
				t.Fatal("paused sandbox was resumed in the background")
			}
		})
	}
}

func TestSoloPausedSandboxResumesWhenTerminalOpens(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	svc := newSandboxService(t, db, ds)
	NewSoloService(db, svc, svc.skills, NewProjectService(db))
	row := models.Sandbox{Name: "paused-resident", Purpose: SandboxPurposeSolo, Profile: "agentA", Status: "running", ThreadID: "session", Token: "token"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "session", AgentName: "agentA", UserID: "alice", Status: models.SoloStatusRunning, SandboxID: row.ID}).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "stopped")
	_, _ = svc.OpenTerminal(context.Background(), row.ID)
	if st := svc.mgr.Status(context.Background(), row.Name); st != "running" {
		t.Fatalf("paused Solo sandbox not resumed on terminal open: %s", st)
	}
}
