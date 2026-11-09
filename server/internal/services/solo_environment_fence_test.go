package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func soloStopFenceFixture(t *testing.T) (*SandboxService, *gorm.DB, models.Sandbox, *httptest.Server) {
	t.Helper()
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	row := models.Sandbox{Name: "stop-fence", Purpose: SandboxPurposeSolo, Status: "running", DesiredState: "running", ThreadID: "stop-chat"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: row.ThreadID, UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, SandboxID: row.ID}).Error; err != nil {
		t.Fatal(err)
	}
	ds.fg.Seed(row.Name)
	return s, db, row, ds.fg.Server
}

func interceptStopGateway(t *testing.T, s *SandboxService, upstream *httptest.Server, intercept func(*http.Request)) {
	t.Helper()
	endpoint, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(endpoint)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		intercept(r)
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	s.mgr = sandbox.NewManager(sandbox.NewGatewayClient(server.URL, ""), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
}

func TestSoloStopLostFenceNeverCallsProviderOrConfirmsIntent(t *testing.T) {
	for _, fence := range []string{"owner", "generation", "expiry", "zero_rows"} {
		t.Run(fence, func(t *testing.T) {
			s, db, row, upstream := soloStopFenceFixture(t)
			var stopCalls atomic.Int32
			interceptStopGateway(t, s, upstream, func(r *http.Request) {
				if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
					stopCalls.Add(1)
				}
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/info") {
					return
				}
				updates := map[string]any{}
				switch fence {
				case "owner":
					updates["operation_owner"] = "successor"
				case "generation":
					updates["generation"] = gorm.Expr("generation + 1")
				case "expiry":
					updates["operation_expires_at"] = time.Now().Add(-time.Second)
				default:
					return
				}
				if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
					t.Error(err)
				}
			})
			if fence == "zero_rows" {
				if err := db.Callback().Update().Before("gorm:update").Register("test:reject_stop_intent", func(tx *gorm.DB) {
					values, ok := tx.Statement.Dest.(map[string]any)
					if tx.Statement.Table == "sandboxes" && ok && values["desired_state"] == "stopped" {
						tx.Where("1 = 0")
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Update().Remove("test:reject_stop_intent") })
			}
			err := s.stopSoloEnvironment(context.Background(), row.ID)
			if !errors.Is(err, errSandboxOperationLost) || stopCalls.Load() != 0 {
				t.Fatalf("lost stop operation reached provider: calls=%d err=%v", stopCalls.Load(), err)
			}
			if err := db.First(&row, row.ID).Error; err != nil || row.DesiredState != "running" || row.Status != "running" {
				t.Fatalf("uncommitted stop was acknowledged: %+v %v", row, err)
			}
		})
	}
}

func TestSoloStopLostFinalFencePreservesSuccessorAndLiveConnection(t *testing.T) {
	s, db, row, upstream := soloStopFenceFixture(t)
	s.live[row.ID] = &liveSandbox{}
	interceptStopGateway(t, s, upstream, func(r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{
				"operation_owner": "successor", "generation": gorm.Expr("generation + 1"),
				"status": "successor-state", "error": "keep successor state", "desired_state": "running",
			}).Error; err != nil {
				t.Error(err)
			}
		}
	})
	if err := s.stopSoloEnvironment(context.Background(), row.ID); !errors.Is(err, errSandboxOperationLost) {
		t.Fatalf("stale completion was acknowledged: %v", err)
	}
	if err := db.First(&row, row.ID).Error; err != nil || row.OperationOwner != "successor" || row.Status != "successor-state" || row.Error != "keep successor state" {
		t.Fatalf("stale worker replaced successor state: %+v %v", row, err)
	}
	if s.live[row.ID] == nil {
		t.Fatal("stale stop tore down a connection after losing its final fence")
	}
	var session models.SoloSession
	if err := db.First(&session, "id = ?", row.ThreadID).Error; err != nil || session.Status != models.SoloStatusRunning {
		t.Fatalf("stale completion changed the chat: %+v %v", session, err)
	}
}

func TestSoloStopFinishingAfterCloseKeepsClosedSession(t *testing.T) {
	s, db, row, upstream := soloStopFenceFixture(t)
	interceptStopGateway(t, s, upstream, func(r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			if err := db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Update("status", models.SoloStatusClosed).Error; err != nil {
				t.Error(err)
			}
		}
	})
	if err := s.stopSoloEnvironment(context.Background(), row.ID); err == nil {
		t.Fatal("a concurrent close was reported as a stopped open chat")
	}
	var session models.SoloSession
	if err := db.First(&session, "id = ?", row.ThreadID).Error; err != nil || session.Status != models.SoloStatusClosed {
		t.Fatalf("stop overwrote a concurrent close: %+v %v", session, err)
	}
}

func TestSoloStopFailureAfterLosingFenceCannotOverwriteSuccessor(t *testing.T) {
	s, db, row, _ := soloStopFenceFixture(t)
	ctx, release, err := s.acquireSandboxOperation(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{
		"operation_owner": "successor", "generation": gorm.Expr("generation + 1"), "error": "successor error", "status": "running",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.recordSoloStopFailure(ctx, row.ID, errors.New("late provider failure")); !errors.Is(err, errSandboxOperationLost) {
		t.Fatalf("late failure was not fenced: %v", err)
	}
	if err := db.First(&row, row.ID).Error; err != nil || row.Error != "successor error" || row.Status != "running" {
		t.Fatalf("late failure replaced successor state: %+v %v", row, err)
	}
}

func TestSoloResumeWaitingForOperationCannotReopenConcurrentClose(t *testing.T) {
	s, db, row, upstream := soloStopFenceFixture(t)
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"status": "stopped", "desired_state": "stopped"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Update("status", models.SoloStatusStopped).Error; err != nil {
		t.Fatal(err)
	}
	var startCalls atomic.Int32
	interceptStopGateway(t, s, upstream, func(r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start") {
			startCalls.Add(1)
		}
	})
	_, release, err := s.acquireSandboxOperation(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	solo := NewSoloService(db, s, s.skills, NewProjectService(db))
	readSession := make(chan struct{}, 1)
	if err := db.Callback().Query().After("gorm:query").Register("test:resume_read_session", func(tx *gorm.DB) {
		if tx.Statement.Table == "solo_sessions" {
			select {
			case readSession <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:resume_read_session") })
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _, err := solo.ResumeEnvironment(ctx, row.ThreadID, "alice"); done <- err }()
	select {
	case <-readSession:
	case err := <-done:
		t.Fatalf("resume failed before the intended race: %v", err)
	case <-ctx.Done():
		t.Fatal("resume never read its starting chat state")
	}
	if err := db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Update("status", models.SoloStatusClosed).Error; err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stale resume reopened a concurrently closed chat")
		}
	case <-ctx.Done():
		t.Fatal("resume did not finish after the operation lock was released")
	}
	var session models.SoloSession
	if err := db.First(&session, "id = ?", row.ThreadID).Error; err != nil || session.Status != models.SoloStatusClosed || startCalls.Load() != 0 {
		t.Fatalf("resume overrode close: session=%+v starts=%d err=%v", session, startCalls.Load(), err)
	}
	if err := db.First(&row, row.ID).Error; err != nil || row.DesiredState != "stopped" {
		t.Fatalf("rejected resume committed a running intent: %+v %v", row, err)
	}
}
