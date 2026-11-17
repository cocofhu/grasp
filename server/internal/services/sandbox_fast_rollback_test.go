package services

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestSoloToolRollbackRestoresAppliedFastInsteadOfNewerSelection(t *testing.T) {
	for _, oldCodex := range []bool{false, true} {
		t.Run(map[bool]string{false: "standard-to-fast", true: "fast-to-unsupported"}[oldCodex], func(t *testing.T) {
			s, row, cursor, codex, ops := soloRouteFixture(t)
			resolve := s.modelBinding
			s.modelBinding = func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
				if backend == "codex" {
					return modelrouting.Resolved{Backend: backend, CredentialID: "fixture-login", Secret: `{"auth_mode":"chatgpt","tokens":{"access_token":"fixture-token","refresh_token":""}}`, Binding: modelrouting.Binding{Kind: "platform", ModelID: "gpt-6.2-sol"}}, nil
				}
				return resolve(backend, binding)
			}
			oldAgent, nextAgent := cursor, codex
			oldFast, nextFast := false, true
			oldEffort := ""
			if oldCodex {
				oldAgent, nextAgent = codex, cursor
				oldFast, nextFast, oldEffort = true, false, "high"
				if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Updates(map[string]any{
					"current_configuration_id": codex.ModelBinding.ConfigurationID, "current_model_set": true, "current_fast_mode": true, "current_reasoning_effort": oldEffort,
				}).Error; err != nil {
					t.Fatal(err)
				}
				if changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, codex); err != nil || !changed {
					t.Fatalf("prepare old Fast tool: changed=%t err=%v", changed, err)
				}
			}
			if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Updates(map[string]any{
				"current_configuration_id": nextAgent.ModelBinding.ConfigurationID, "current_model_set": true, "current_fast_mode": nextFast, "current_reasoning_effort": "",
				"applied_configuration_id": oldAgent.ModelBinding.ConfigurationID, "applied_model_set": true, "applied_fast_mode": oldFast, "applied_reasoning_effort": oldEffort,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if changed, err := s.AlignSoloModelRoute(context.Background(), row.ID, nextAgent); err != nil || !changed {
				t.Fatalf("forward tool switch: changed=%t err=%v", changed, err)
			}
			// Simulate the failed settings operation after the forward tool switch.
			// The stored selection still points at the new tool, while rollback
			// explicitly requests the last applied model/effort/speed snapshot.
			previous := models.SoloSession{ID: row.ThreadID, CurrentConfigurationID: oldAgent.ModelBinding.ConfigurationID,
				CurrentModelSet: true, CurrentFastMode: oldFast, CurrentReasoningEffort: oldEffort}
			if changed, err := s.alignSoloModelRoute(context.Background(), row.ID, oldAgent, &previous); err != nil || !changed {
				t.Fatalf("restore old tool: changed=%t err=%v", changed, err)
			}
			env := ops.specs[len(ops.specs)-1].Env
			wantFast := "false"
			if oldFast {
				wantFast = "true"
			}
			if env[runtime.EnvACPBridgeFastMode] != wantFast || env[runtime.EnvACPBridgeReasoningEffort] != oldEffort || env["AGENT_PROVIDER"] != oldAgent.AcpBackend {
				t.Fatalf("rollback used a newer selection: tool=%s Fast=%s effort=%s", env["AGENT_PROVIDER"], env[runtime.EnvACPBridgeFastMode], env[runtime.EnvACPBridgeReasoningEffort])
			}
			var stored models.SoloSession
			if err := s.db.First(&stored, "id = ?", row.ThreadID).Error; err != nil || stored.CurrentConfigurationID != nextAgent.ModelBinding.ConfigurationID || stored.CurrentFastMode != nextFast {
				t.Fatalf("runtime rollback rewrote next-turn selection before its CAS: %+v %v", stored, err)
			}
		})
	}
}
