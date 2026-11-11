package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func startupRetentionRow(t *testing.T, s *SandboxService, purpose string) models.Sandbox {
	t.Helper()
	row := models.Sandbox{Name: "original-correlation", Profile: "agentA", Purpose: purpose, Status: "creating", RunID: "original-run", ThreadID: "original-thread"}
	if err := s.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSandboxStartupCancelledRetentionAndCloseRetryKeepOriginalFiles(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	row := startupRetentionRow(t, s, "test")
	source, cancel := context.WithCancel(context.Background())
	ctx, identity, release, err := s.beginSandboxStartup(source, row.ID, row.Name)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	marker := filepath.Join(home, "original-file")
	if err = os.WriteFile(marker, []byte("uncommitted original bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	handle := &sandbox.Sandbox{Name: "provider-original", Host: "127.0.0.1", Port: 8765}
	ds.setStatus(handle.Name, "running")
	cancel()
	if err = s.adoptSandboxStartupHandle(ctx, &identity, handle, home); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup adopted success: %v", err)
	}
	if err = s.retainSandboxStartupFailure(ctx, identity, handle, home, context.Canceled); err != nil {
		t.Fatal(err)
	}
	if err = s.publishSandboxStartup(ctx, identity, map[string]any{"status": "running"}); err == nil {
		t.Fatal("cancelled startup published running")
	}
	release()
	var kept models.Sandbox
	if err = db.First(&kept, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if kept.Name != handle.Name || kept.Status != "error" || kept.DestroyAt != nil || kept.HomeDir != home {
		t.Fatalf("lost original attribution: %+v", kept)
	}
	// A stale pre-fix deadline must never authorize a second-stage delete.
	past := time.Now().Add(-time.Hour)
	if err = db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("destroy_at", past).Error; err != nil {
		t.Fatal(err)
	}
	s.sweepOnce(context.Background())
	if !ds.fg.Has(handle.Name) {
		t.Fatal("error sweeper deleted provider")
	}
	if err = db.First(&kept, row.ID).Error; err != nil || kept.DestroyAt != nil {
		t.Fatal("error retention deadline not cleared", err)
	}
	ds.fg.FailStop = true
	if err = s.Stop(context.Background(), row.ID); err == nil {
		t.Fatal("failed provider Stop reported success")
	}
	ds.fg.FailStop = false
	if err = s.Stop(context.Background(), row.ID); err != nil {
		t.Fatal("explicit Stop retry failed", err)
	}
	if err = s.Stop(context.Background(), row.ID); err != nil {
		t.Fatal("confirmed Close retry lost original environment", err)
	}
	if err = db.First(&kept, row.ID).Error; err != nil || kept.Name != handle.Name || kept.Status != "stopped" || kept.DestroyAt != nil {
		t.Fatalf("Close changed original binding: %+v %v", kept, err)
	}
	b, err := os.ReadFile(marker)
	if err != nil || string(b) != "uncommitted original bytes" || !ds.fg.Has(handle.Name) {
		t.Fatal("Close removed retained files/provider", err)
	}
	if err = s.retainSandboxStartupFailure(context.Background(), identity, handle, home, errors.New("late old startup")); !errors.Is(err, errSandboxOperationLost) {
		t.Fatalf("late callback overwrote closed generation: %v", err)
	}
}

func TestSandboxStartupRetentionRefusesChangedProvenanceAndReplacement(t *testing.T) {
	for _, change := range []string{"generation", "name", "run_id", "thread_id", "state_run_id", "run_attempt", "provider_attempt"} {
		t.Run(change, func(t *testing.T) {
			db := newTestDB(t)
			s := newSandboxService(t, db, &dockerState{})
			row := startupRetentionRow(t, s, "agent")
			ctx, identity, release, err := s.beginSandboxStartup(context.Background(), row.ID, row.Name)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			value := any("replacement")
			if change == "generation" {
				value = row.Generation + 2
			}
			if change == "state_run_id" || change == "run_attempt" || change == "provider_attempt" {
				value = 23
			}
			if err = db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update(change, value).Error; err != nil {
				t.Fatal(err)
			}
			err = s.retainSandboxStartupFailure(ctx, identity, &sandbox.Sandbox{Name: "old-provider"}, "", errors.New("old failure"))
			if !errors.Is(err, errSandboxOperationLost) {
				t.Fatalf("old callback was admitted after %s changed: %v", change, err)
			}
			var kept models.Sandbox
			db.First(&kept, row.ID)
			if kept.Status != "creating" || kept.Error != "" {
				t.Fatal("replacement was overwritten")
			}
		})
	}
}

// Exercise the actual Manager (sb,error) contract and both non-resident
// callers; a provider error is returned after allocating unique writable files.
func TestSandboxStartupPartialCreateAndACPFailureNeverDestroy(t *testing.T) {
	for _, purpose := range []string{"test", "agent"} {
		for _, failure := range []string{"provider_error", "acp_connect"} {
			t.Run(purpose+"/"+failure, func(t *testing.T) {
				db := newTestDB(t)
				s := newSandboxService(t, db, &dockerState{})
				row := startupRetentionRow(t, s, purpose)
				var deleted, stopped, created atomic.Int32
				marker := filepath.Join(t.TempDir(), "provider-files")
				const provider = "native-original"
				// The readiness WS succeeds; the subsequent real ACP connect handshake
				// returns an explicit execution error, without a two-minute retry budget.
				acp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/login" {
						http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "synthetic", Path: "/"})
						_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
						return
					}
					if r.URL.Path == "/ws" {
						conn, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
						if e != nil {
							return
						}
						defer conn.Close()
						if _, _, e = conn.ReadMessage(); e == nil {
							_ = conn.WriteJSON(map[string]any{"op": "error", "message": "synthetic ACP startup failure"})
						}
						return
					}
					http.NotFound(w, r)
				}))
				defer acp.Close()
				address := strings.TrimPrefix(acp.URL, "http://")
				gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodDelete {
						deleted.Add(1)
						_ = os.Remove(marker)
						w.WriteHeader(204)
						return
					}
					if strings.HasSuffix(r.URL.Path, "/stop") {
						stopped.Add(1)
						w.WriteHeader(204)
						return
					}
					if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sandboxes") {
						created.Add(1)
						if e := os.WriteFile(marker, []byte("dirty provider source"), 0600); e != nil {
							t.Error(e)
						}
					}
					status := "running"
					if failure == "provider_error" {
						status = "error"
					}
					_ = json.NewEncoder(w).Encode(sandbox.GWSandbox{ID: provider, Status: status, Error: "original startup error", Endpoints: map[string]string{"session": address}})
				}))
				defer gateway.Close()
				s.mgr = sandbox.NewManager(sandbox.NewGatewayClient(gateway.URL, ""), sandbox.ManagerOptions{})
				agent, _ := s.skills.Get("agentA")
				agent = s.effectiveAgent(agent)
				if purpose == "test" {
					s.startContainer(row.ID, row.Name, row.Profile, row.RunID, "synthetic-token", nil, agent, "")
				} else {
					s.startAgentContainer(row.ID, row.Name, row.Profile, row.ThreadID, row.RunID, "synthetic-token", nil, agent, agentContainerExtra{})
				}
				var kept models.Sandbox
				if err := db.First(&kept, row.ID).Error; err != nil {
					t.Fatal(err)
				}
				if created.Load() != 1 || deleted.Load() != 0 || stopped.Load() != 0 || kept.Name != provider || kept.Status != "error" || kept.DestroyAt != nil || kept.HomeDir == "" {
					t.Fatalf("provider outcome lost: create/delete/stop=%d/%d/%d row=%+v", created.Load(), deleted.Load(), stopped.Load(), kept)
				}
				if b, e := os.ReadFile(marker); e != nil || string(b) != "dirty provider source" {
					t.Fatal("original provider file removed", e)
				}
				if _, e := os.Stat(kept.HomeDir); e != nil {
					t.Fatal("failed startup removed original config/worktree home", e)
				}
				if purpose == "test" && s.activeCount() != 1 {
					t.Fatal("unconfirmed test compute disappeared from quota")
				}
				if purpose == "agent" && s.activeAgentSandboxCount() != 1 {
					t.Fatal("unconfirmed agent compute disappeared from quota")
				}
			})
		}
	}
}

func TestSandboxStartupAmbiguousCorrelationRetainsWithoutGuessing(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	row := startupRetentionRow(t, s, "test")
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first-original", "second-original"} {
		ds.setStatus(id, "running")
		ds.fg.SetLabel(id, sandbox.CorrelationNameKey(), row.Name)
	}
	s.ReconcileOnStartup(context.Background(), nil)
	var kept models.Sandbox
	if err := db.First(&kept, row.ID).Error; err != nil || kept.Name != row.Name || kept.Status != "error" || kept.DestroyAt != nil {
		t.Fatalf("ambiguous correlation guessed or removed identity: %+v %v", kept, err)
	}
	if !ds.fg.Has("first-original") || !ds.fg.Has("second-original") {
		t.Fatal("ambiguous source deleted")
	}
}

func TestSandboxStartupStaleSweeperScanCannotDeleteNewErrorReceipt(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	past := time.Now().Add(-time.Minute)
	row := models.Sandbox{Name: "original-provider", Purpose: "test", Status: "running", DestroyAt: &past}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "running")
	// The scan has already read running; an in-flight start/connection failure
	// then changes the authoritative row before sweeper obtains its lease.
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"status": "error", "error": "ACP unconfirmed"}).Error; err != nil {
		t.Fatal(err)
	}
	s.sweepRows(context.Background(), []models.Sandbox{row})
	var kept models.Sandbox
	if err := db.First(&kept, row.ID).Error; err != nil || kept.Status != "error" || kept.Error != "ACP unconfirmed" || kept.DestroyAt != nil || !ds.fg.Has(row.Name) {
		t.Fatalf("stale sweep removed error receipt: %+v %v", kept, err)
	}
	// A stale reconcile scan must not attach/restart this same failed row.
	if err := s.reconcileNonresidentRunning(context.Background(), row); !errors.Is(err, errSandboxOperationLost) {
		t.Fatalf("stale reconcile attached failed environment: %v", err)
	}
}

func TestSandboxStartupLegacyUnknownProvenanceRetainsWithoutInventingAttempt(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	row := startupRetentionRow(t, s, "test")
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"state_run_id": nil, "run_attempt": nil, "provider_attempt": nil, "thread_id": nil}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, identity, release, err := s.beginSandboxStartup(context.Background(), row.ID, row.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err = s.retainSandboxStartupFailure(ctx, identity, &sandbox.Sandbox{Name: "original-provider"}, "", errors.New("unconfirmed")); err != nil {
		t.Fatal(err)
	}
	var retained models.Sandbox
	if err = db.First(&retained, row.ID).Error; err != nil || retained.Name != "original-provider" || retained.StateRunID != 0 || retained.RunAttempt != 0 || retained.ProviderAttempt != 0 || retained.DestroyAt != nil {
		t.Fatalf("unknown origin was discarded or fabricated: %+v %v", retained, err)
	}
}

func TestSandboxStartupFailureCannotPublishAcrossExecutionLineage(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	controller, authority, owner := serviceExecutionController(t, db)
	s.execution = controller
	row := startupRetentionRow(t, s, "test")
	ctx, identity, release, err := s.beginSandboxStartup(context.Background(), row.ID, row.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	owner = uuid.NewString()
	if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = authority.BeginRestore(context.Background(), owner, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err = s.retainSandboxStartupFailure(ctx, identity, &sandbox.Sandbox{Name: "obsolete-provider"}, "", errors.New("old process failure")); err == nil {
		t.Fatal("old dataset published failure into revoked execution lineage")
	}
	var retained models.Sandbox
	if err = db.First(&retained, row.ID).Error; err != nil || retained.Name != row.Name || retained.Status != "creating" {
		t.Fatalf("revoked worker rewrote original row: %+v %v", retained, err)
	}
}

func TestSandboxStartupUnknownReuseReturnsOriginalReceiptWithoutNewCreate(t *testing.T) {
	for _, purpose := range []string{"test", "agent"} {
		t.Run(purpose, func(t *testing.T) {
			db := newTestDB(t)
			ds := &dockerState{}
			s := newSandboxService(t, db, ds)
			row := models.Sandbox{Name: "original-uncertain-provider", Profile: "agentA", Purpose: purpose, Status: "error", Error: "original creation outcome unknown", RunID: "original-run", ThreadID: "original-thread"}
			sandboxTestAppliedBindings(t, s, &row)
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			ds.setStatus(row.Name, "running")
			var got *models.Sandbox
			var err error
			if purpose == "test" {
				got, err = s.Open(context.Background(), row.Profile, nil)
			} else {
				var reused bool
				got, reused, err = s.OpenAgentSandbox(context.Background(), AgentSandboxOpenOpts{Profile: row.Profile, ThreadID: row.ThreadID, Reuse: true})
				if !reused {
					t.Fatal("unknown receipt not returned as the original")
				}
			}
			if err != nil || got.ID != row.ID || got.Name != row.Name || got.Status != "error" || got.DestroyAt != nil {
				t.Fatalf("unknown was replayed or changed: %+v %v", got, err)
			}
			var count int64
			if err = db.Model(&models.Sandbox{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatal("another workspace was allocated", err, count)
			}
		})
	}
}
