package services

import (
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

type workspaceStopHook func(*WorkspaceBindingService, models.SoloSession, http.ResponseWriter, *http.Request) bool

func workspaceStopFixture(t *testing.T, hook workspaceStopHook) (*WorkspaceBindingService, models.SoloSession, WorkspaceConsumerAccess, *atomic.Int32) {
	t.Helper()
	bindings, _, parent := bindingAccessFixture(t)
	access, err := bindings.sandbox.BeginWorkspaceConsumer(t.Context(), parent.SandboxID, "ide")
	if err != nil {
		t.Fatal(err)
	}
	if err := bindings.db.Create(&models.WorkspaceBindingUse{ID: "unrelated-environment", SandboxID: parent.SandboxID + 100, Kind: "terminal", Owner: "other-owner", State: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	upstream, err := url.Parse(bindings.sandbox.mgr.Gateway().BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	stops := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			stops.Add(1)
			if hook != nil && hook(bindings, parent, w, r) {
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	bindings.sandbox.mgr = sandbox.NewManager(sandbox.NewGatewayClient(server.URL, ""), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	return bindings, parent, access, stops
}

func workspaceStopActiveUses(t *testing.T, bindings *WorkspaceBindingService, sandboxID uint) int64 {
	t.Helper()
	var count int64
	if err := bindings.db.Model(&models.WorkspaceBindingUse{}).Where("sandbox_id = ? AND state <> 'closed'", sandboxID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestWorkspaceConfirmedSoloStopClosesDurableConsumersWithItsPublication(t *testing.T) {
	for _, closing := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "close"}[closing], func(t *testing.T) {
			bindings, parent, access, stops := workspaceStopFixture(t, nil)
			if closing {
				if err := bindings.db.Model(&models.SoloSession{}).Where("id = ?", parent.ID).Updates(map[string]any{"status": models.SoloStatusClosed, "lifecycle_action": soloCloseCleanup}).Error; err != nil {
					t.Fatal(err)
				}
			}
			// Existing pins remain meaningful even when this process has not
			// installed the optional feature service after an application restart.
			bindings.sandbox.workspaceBindings.Store(nil)
			if err := bindings.sandbox.stopSoloEnvironment(t.Context(), parent.SandboxID); err != nil {
				t.Fatal(err)
			}
			if stops.Load() != 1 || workspaceStopActiveUses(t, bindings, parent.SandboxID) != 0 || workspaceStopActiveUses(t, bindings, parent.SandboxID+100) != 1 {
				t.Fatal("confirmed stop did not close only its own environment's pins")
			}
			if err := access.Close(); err != nil {
				t.Fatal("late actual disconnect lost its existing receipt", err)
			}
			var row models.Sandbox
			if err := bindings.db.First(&row, parent.SandboxID).Error; err != nil || row.Status != "stopped" || row.DesiredState != "stopped" || row.Name != "canonical-resource" {
				t.Fatal("stopped original resource was not retained", row, err)
			}
			if err := bindings.db.First(&parent, "id = ?", parent.ID).Error; err != nil {
				t.Fatal(err)
			}
			want := models.SoloStatusStopped
			if closing {
				want = models.SoloStatusClosed
			}
			if parent.Status != want || parent.LifecycleAction != "" {
				t.Fatal("consumer confirmation did not share the parent publication", parent)
			}
		})
	}
}

func TestWorkspaceUnknownSoloStopNeverClosesConsumers(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "provider_failure", true: "still_running"}[pending], func(t *testing.T) {
			bindings, parent, _, stops := workspaceStopFixture(t, func(_ *WorkspaceBindingService, _ models.SoloSession, w http.ResponseWriter, _ *http.Request) bool {
				if pending {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				return true // The original fake provider remains actually running.
			})
			if err := bindings.sandbox.stopSoloEnvironment(t.Context(), parent.SandboxID); err == nil {
				t.Fatal("unconfirmed stop succeeded")
			}
			if stops.Load() != 1 || workspaceStopActiveUses(t, bindings, parent.SandboxID) != 1 {
				t.Fatal("failed/pending provider stop cleared a durable consumer")
			}
		})
	}
}

func TestWorkspaceStoppedConsumersPreserveSuccessorPhysicalLeaseAndLogicalHead(t *testing.T) {
	for _, change := range []string{"physical_lease", "binding", "native_fingerprint", "missing_head_installed"} {
		t.Run(change, func(t *testing.T) {
			bindings, parent, _, _ := workspaceStopFixture(t, func(bindings *WorkspaceBindingService, parent models.SoloSession, _ http.ResponseWriter, _ *http.Request) bool {
				var err error
				switch change {
				case "physical_lease":
					err = bindings.db.Model(&models.Sandbox{}).Where("id = ?", parent.SandboxID).Updates(map[string]any{"operation_owner": "successor", "generation": gorm.Expr("generation + 1"), "operation_expires_at": time.Now().Add(time.Minute)}).Error
				case "binding":
					err = bindings.db.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", parent.SandboxID).Updates(map[string]any{"binding_id": "successor-binding", "revision": 2}).Error
				case "native_fingerprint":
					err = bindings.db.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", parent.SandboxID).Update("native_fingerprint", strings.Repeat("f", 64)).Error
				case "missing_head_installed":
					err = bindings.db.Create(&models.WorkspaceBindingHead{SandboxID: parent.SandboxID, ScopeKind: "solo", ScopeID: parent.ID, BindingID: "new-head", Revision: 1}).Error
				}
				if err != nil {
					t.Error(err)
				}
				return false // Native Stop succeeds, but its old publication is fenced.
			})
			if change == "missing_head_installed" {
				if err := bindings.db.Delete(&models.WorkspaceBindingHead{}, "sandbox_id = ?", parent.SandboxID).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := bindings.sandbox.stopSoloEnvironment(t.Context(), parent.SandboxID); err == nil {
				t.Fatal("old stop published against a successor")
			}
			if workspaceStopActiveUses(t, bindings, parent.SandboxID) != 1 {
				t.Fatal("stale stop cleared a connection belonging to unconfirmed state")
			}
			var row models.Sandbox
			if err := bindings.db.First(&row, parent.SandboxID).Error; err != nil || row.Status != "running" {
				t.Fatal("stale stop overwrote the successor metadata", row, err)
			}
			if change == "physical_lease" && row.OperationOwner != "successor" {
				t.Fatal("stale stop released successor ownership")
			}
		})
	}
}

func TestWorkspaceStoppedConsumerReceiptFailureRollsBackStoppedPublication(t *testing.T) {
	bindings, parent, _, stops := workspaceStopFixture(t, nil)
	failure := errors.New("disconnect receipt storage unavailable")
	if err := bindings.db.Callback().Update().Before("gorm:update").Register("test:stopped-consumers-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "workspace_binding_uses" {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bindings.db.Callback().Update().Remove("test:stopped-consumers-failure") })
	if err := bindings.sandbox.stopSoloEnvironment(t.Context(), parent.SandboxID); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var row models.Sandbox
	if err := bindings.db.First(&row, parent.SandboxID).Error; err != nil || row.Status != "running" || row.DesiredState != "stopped" {
		t.Fatal("failed receipt commit published a confirmed stop", row, err)
	}
	if stops.Load() != 1 || workspaceStopActiveUses(t, bindings, parent.SandboxID) != 1 {
		t.Fatal("receipt failure lost the original consumer")
	}
	if err := bindings.db.First(&parent, "id = ?", parent.ID).Error; err != nil || parent.Status != models.SoloStatusRunning {
		t.Fatal("receipt failure published a stopped parent", parent, err)
	}
}

func TestWorkspaceConfirmedLegacyStopAndFailedHeadRead(t *testing.T) {
	for _, readFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged_missing_head", true: "head_read_failed"}[readFailure], func(t *testing.T) {
			bindings, parent, _, stops := workspaceStopFixture(t, nil)
			if !readFailure {
				if err := bindings.db.Delete(&models.WorkspaceBindingHead{}, "sandbox_id = ?", parent.SandboxID).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := bindings.db.Callback().Query().Before("gorm:query").Register("test:stop-head-read-failed", func(tx *gorm.DB) {
					if tx.Statement.Table == "workspace_binding_heads" {
						_ = tx.AddError(errors.New("head metadata unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = bindings.db.Callback().Query().Remove("test:stop-head-read-failed") })
			}
			err := bindings.sandbox.stopSoloEnvironment(t.Context(), parent.SandboxID)
			if readFailure {
				if err == nil || stops.Load() != 0 || workspaceStopActiveUses(t, bindings, parent.SandboxID) != 1 {
					t.Fatal("failed head read allowed provider dispatch or released a consumer", err)
				}
			} else if err != nil || stops.Load() != 1 || workspaceStopActiveUses(t, bindings, parent.SandboxID) != 0 {
				t.Fatal("confirmed stop failed to preserve legacy missing-head compatibility", err)
			}
		})
	}
}
