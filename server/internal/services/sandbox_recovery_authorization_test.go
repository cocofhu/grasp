package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
)

type recoveryAuthorizationControl struct {
	starts   atomic.Int32
	failStop atomic.Bool
	hook     atomic.Pointer[func(*http.Request)]
	provider *sandboxtest.FakeGateway
}

func recoveryAuthorizationFixture(t *testing.T) (*SandboxService, models.Sandbox, *CredentialService, map[string]string, *recoveryAuthorizationControl) {
	t.Helper()
	s, agent, credentials, ids := authorizationFixture(t)
	if err := s.skills.Save(agent); err != nil {
		t.Fatal(err)
	}
	agent, _ = s.skills.Get(agent.Name)
	proof, err := s.currentAuthorizationFingerprint(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	fg.SetStatus("authorization-resident", "stopped")
	control := recoveryAuthorizationControl{provider: fg}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") && control.failStop.Load() {
			http.Error(w, "stop outcome unknown", http.StatusServiceUnavailable)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/start") {
			control.starts.Add(1)
		}
		fg.Server.Config.Handler.ServeHTTP(w, r)
		if hook := control.hook.Load(); hook != nil {
			(*hook)(r)
		}
	}))
	t.Cleanup(proxy.Close)
	s.mgr = sandbox.NewManager(sandbox.NewGatewayClient(proxy.URL, ""), sandbox.ManagerOptions{})
	row := models.Sandbox{Name: "authorization-resident", Profile: agent.Name, Purpose: SandboxPurposeSolo, Status: "stopped", ThreadID: "authorization-session", RunID: "authorization-run", Token: "authorization-token", AuthorizationFingerprint: proof}
	if err := s.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&models.SoloSession{ID: row.ThreadID, UserID: "alice", AgentName: agent.Name, SandboxID: row.ID, Status: models.SoloStatusRunning, InitialConfigurationID: agent.ModelBinding.ConfigurationID}).Error; err != nil {
		t.Fatal(err)
	}
	return s, row, credentials, ids, &control
}

func TestAuthorizationDeniedUnknownStopStillConsumesComputeQuota(t *testing.T) {
	s, row, credentials, ids, control := recoveryAuthorizationFixture(t)
	control.provider.SetStatus(row.Name, "running")
	control.failStop.Store(true)
	if err := s.db.Model(&row).Update("status", "running").Error; err != nil {
		t.Fatal(err)
	}
	if err := credentials.Revoke(ids["model"]); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSoloRunning(context.Background(), row.ID); err == nil {
		t.Fatal("revoked authorization with unknown Stop outcome was accepted")
	}
	kept, err := s.Get(row.ID)
	if err != nil || kept.Status != "error" || kept.DesiredState != "stopped" || kept.Name != row.Name || s.activeSoloSandboxCount() != 1 {
		t.Fatalf("unknown provider outcome lost data attribution or compute reservation: %+v %v", kept, err)
	}
	if state, err := s.mgr.ProbeStatus(context.Background(), row.Name); err != nil || state != "running" || control.starts.Load() != 0 {
		t.Fatalf("test did not retain the actual unconfirmed provider: state=%s starts=%d err=%v", state, control.starts.Load(), err)
	}
}

func TestRecoveryAuthorizationBlocksBeforeProviderStart(t *testing.T) {
	for _, change := range []string{"model", "git", "extra", "mcp", "missing proof", "cleanup"} {
		t.Run(change, func(t *testing.T) {
			s, row, credentials, ids, control := recoveryAuthorizationFixture(t)
			if change == "missing proof" {
				if err := s.db.Model(&row).Update("authorization_fingerprint", "").Error; err != nil {
					t.Fatal(err)
				}
			} else if change == "cleanup" {
				if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Update("lifecycle_action", "restart_cleanup").Error; err != nil {
					t.Fatal(err)
				}
			} else if err := credentials.Revoke(ids[change]); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureSoloRunning(context.Background(), row.ID); err == nil {
				t.Fatal("changed or unknown grant resumed a provider")
			}
			if control.starts.Load() != 0 {
				t.Fatal("provider was started before current authorization")
			}
			var kept models.Sandbox
			if err := s.db.First(&kept, row.ID).Error; err != nil || kept.Name != row.Name || kept.ThreadID != row.ThreadID || kept.Token != row.Token {
				t.Fatal("authorization denial discarded the retained binding", err)
			}
		})
	}
}

func TestRecoveryAuthorizationChangeBeforeAndDuringProviderStart(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "beforeStart", true: "duringStart"}[during], func(t *testing.T) {
			s, row, credentials, ids, control := recoveryAuthorizationFixture(t)
			var changed atomic.Bool
			result := make(chan error, 1)
			hook := func(r *http.Request) {
				match := r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status")
				if during {
					match = r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start")
				}
				if match && changed.CompareAndSwap(false, true) {
					result <- credentials.Revoke(ids["model"])
				}
			}
			control.hook.Store(&hook)
			if err := s.EnsureSoloRunning(context.Background(), row.ID); err == nil {
				t.Fatal("changed grant published recovered state")
			}
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("test did not cross the provider mutation boundary")
			}
			want := int32(0)
			if during {
				want = 1
			}
			if control.starts.Load() != want {
				t.Fatalf("provider starts=%d, want%d", control.starts.Load(), want)
			}
			var kept models.Sandbox
			if err := s.db.First(&kept, row.ID).Error; err != nil || kept.Name != row.Name || kept.Status == "running" || kept.DesiredState != "stopped" {
				t.Fatal("revocation lost retained resource or published running", err)
			}
		})
	}
}

func TestRecoveryGrantRechecksLifecycleAndCachedMCPAuthority(t *testing.T) {
	s, row, credentials, ids, _ := recoveryAuthorizationFixture(t)
	grant, err := s.soloRecoveryAuthorization(context.Background(), &row)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Update("lifecycle_epoch", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.recheckSoloRecoveryGrant(context.Background(), &row, grant); err == nil {
		t.Fatal("late recovery adopted a newer lifecycle")
	}
	if err := s.db.Model(&row).Update("status", "running").Error; err != nil {
		t.Fatal(err)
	}
	if !s.CurrentSoloMCPAuthorization(row.RunID) {
		t.Fatal("current resident authorization rejected")
	}
	if err := credentials.Revoke(ids["mcp"]); err != nil {
		t.Fatal(err)
	}
	if s.CurrentSoloMCPAuthorization(row.RunID) {
		t.Fatal("cached MCP credential survived revocation")
	}
}
