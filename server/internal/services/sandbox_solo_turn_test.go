package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
)

func TestSoloTurnCanReplaceRemovedGroupOnlyInAlreadyRunningEnvironment(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(map[bool]string{true: "running forward switch", false: "paused old authorization"}[running], func(t *testing.T) {
			s, row, original, target, ops := soloRouteFixture(t)
			if err := s.db.Create(&models.ModelConfiguration{ID: "second-group", Name: "Second", Backend: target.AcpBackend, State: "active", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "fixture-key", ModelID: "fixture-model"}}).Error; err != nil {
				t.Fatal(err)
			}
			s.skills.SetModelConfigurationResolver(func(binding *modelrouting.Binding) (string, error) {
				if binding.ConfigurationID == "second-group" {
					return target.AcpBackend, nil
				}
				return original.AcpBackend, nil
			})
			profile, _ := s.skills.Get(original.Name)
			profile.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: "second-group", ConfigurationIDs: []string{"second-group", "sandbox-fixture"}}
			if err := s.skills.Save(profile); err != nil {
				t.Fatal(err)
			}
			if err := s.db.Delete(&models.ModelConfiguration{}, "id = ?", "sandbox-fixture").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Updates(map[string]any{
				"current_configuration_id": "second-group", "current_model_set": true, "applied_configuration_id": "sandbox-fixture", "applied_model_set": true,
			}).Error; err != nil {
				t.Fatal(err)
			}
			fg := sandboxtest.New(t)
			state := "stopped"
			if running {
				state = "running"
			}
			fg.SetStatus(row.Name, state)
			var starts atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start") {
					starts.Add(1)
				}
				fg.Server.Config.Handler.ServeHTTP(w, r)
			}))
			defer proxy.Close()
			s.mgr = sandbox.NewManager(sandbox.NewGatewayClient(proxy.URL, ""), sandbox.ManagerOptions{})
			var parent models.SoloSession
			if err := s.db.First(&parent, "id = ?", row.ThreadID).Error; err != nil {
				t.Fatal(err)
			}
			err := s.EnsureSoloTurnRunning(context.Background(), &parent, target)
			if running {
				if err != nil || len(ops.specs) != 1 {
					t.Fatalf("valid forward switch failed: %v native switches=%d", err, len(ops.specs))
				}
				if err := s.db.First(&parent, "id = ?", row.ThreadID).Error; err != nil || parent.SandboxID != row.ID || parent.AppliedConfigurationID != "second-group" {
					t.Fatalf("forward switch lost the original environment: %+v %v", parent, err)
				}
			} else if err == nil || len(ops.specs) != 0 {
				t.Fatalf("paused old route was used to apply a new grant: %v switches=%d", err, len(ops.specs))
			}
			if starts.Load() != 0 {
				t.Fatal("forward route selection woke preserved old credentials")
			}
		})
	}
}
