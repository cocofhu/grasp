package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSoloCreateReceiptSurvivesCallerButCannotCrossOwnerOrDataset(t *testing.T) {
	for _, scenario := range []string{"expired_caller", "revoked_model_grant", "replaced_physical_owner", "changed_correlation", "restored_dataset"} {
		t.Run(scenario, func(t *testing.T) {
			_, service, gateway, db := newSoloFixture(t, 0, 1)
			controller, authority, owner := serviceExecutionController(t, db)
			service.execution = controller
			parent := models.SoloSession{ID: "original-creating-solo", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStarting}
			if err := db.Create(&parent).Error; err != nil {
				t.Fatal(err)
			}
			resource := models.Sandbox{Name: "original-create-correlation", Profile: "agentA", Purpose: SandboxPurposeSolo, ThreadID: parent.ID, Status: "creating"}
			if err := db.Create(&resource).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&parent).Update("sandbox_id", resource.ID).Error; err != nil {
				t.Fatal(err)
			}
			agent, _ := service.skills.Get("agentA")
			agent = service.effectiveAgent(agent)
			grant, err := service.soloStartAuthorization(t.Context(), agent.Name, agent)
			if err != nil {
				t.Fatal(err)
			}
			caller, cancel := context.WithCancel(t.Context())
			operation, release, err := service.acquireSandboxOperation(caller, resource.ID, "create")
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			defer release()
			cancel() // The provider result arrives after its original caller.
			gateway.Seed("original-provider-resource")
			switch scenario {
			case "revoked_model_grant":
				if err := db.Model(&models.Credential{}).Where("id = ?", "fixture-key").Update("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			case "replaced_physical_owner":
				if err := db.Model(&resource).Updates(map[string]any{"operation_owner": "successor", "generation": gorm.Expr("generation + 1")}).Error; err != nil {
					t.Fatal(err)
				}
			case "changed_correlation":
				if err := db.Model(&models.Sandbox{}).Where("id = ?", resource.ID).Update("name", "another-creation-receipt").Error; err != nil {
					t.Fatal(err)
				}
			case "restored_dataset":
				if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
					t.Fatal(err)
				}
				if _, err := authority.BeginRestore(t.Context(), uuid.NewString(), strings.Repeat("a", 64)); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := service.retainSoloCreateResult(operation, resource.ID, resource.Name, agent.Name, parent.ID, agent, grant, &sandbox.Sandbox{Name: "original-provider-resource", ID: "original-provider-resource"})
			if scenario == "replaced_physical_owner" || scenario == "changed_correlation" || scenario == "restored_dataset" {
				if err == nil || (scenario == "restored_dataset" && !errors.Is(err, executionlineage.ErrInactive)) {
					t.Fatalf("obsolete creator bypassed owner/dataset: %v", err)
				}
			} else if err != nil || changed != (scenario == "revoked_model_grant") {
				t.Fatalf("original receipt rejected: changed=%v err=%v", changed, err)
			}
			var actual models.Sandbox
			if err := db.Take(&actual, resource.ID).Error; err != nil {
				t.Fatal(err)
			}
			expected := "original-provider-resource"
			if scenario == "replaced_physical_owner" || scenario == "restored_dataset" {
				expected = resource.Name
			} else if scenario == "changed_correlation" {
				expected = "another-creation-receipt"
			}
			if actual.Name != expected || actual.DestroyAt != nil || actual.Status == "running" {
				t.Fatalf("failed creator changed successor or published ready: %+v", actual)
			}
			provider, err := gateway.Client().Get(t.Context(), "original-provider-resource")
			if err != nil || (scenario == "revoked_model_grant" && (actual.DesiredState != "stopped" || provider.Status != "stopped")) {
				t.Fatalf("cleanup lost original resource or failed exact stop: %+v %+v %v", actual, provider, err)
			}
		})
	}
}

func TestSoloUnconfirmedAndMissingStopNeverClaimsFilesRetained(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport_unknown", true: "provider_record_missing"}[missing], func(t *testing.T) {
			acp, port := fakeACPServer(t)
			defer acp.Close()
			solo, _, gateway, db := newSoloFixture(t, port, 1)
			view, err := solo.Create(t.Context(), "alice", SoloCreateInput{AgentName: "agentA"})
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "resident before stop", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
			parent, _ := solo.Require(view.ID, "alice")
			var original models.Sandbox
			if err := db.Take(&original, parent.SandboxID).Error; err != nil {
				t.Fatal(err)
			}
			if missing {
				gateway.SetStatus(original.Name, "")
			} else {
				gateway.FailStop = true
			}
			_, err = solo.Close(t.Context(), view.ID, "alice")
			requireSoloFailure(t, err, "environment_stop_pending")
			var actual models.Sandbox
			if err := db.Take(&actual, original.ID).Error; err != nil {
				t.Fatal(err)
			}
			recovery := actual.Recovery
			if recovery.Files == "retained" || recovery.Process == "restarted" || recovery.History != "retained" {
				t.Fatalf("unconfirmed stop invented retention: %+v", recovery)
			}
			if actual.Name != original.Name || actual.DestroyAt != nil || actual.Status == "stopped" {
				t.Fatalf("unknown resource was erased or marked stopped: %+v", actual)
			}
			if count, err := countSoloCompute(db, 0); err != nil || count != 1 {
				t.Fatalf("unknown provider incorrectly released quota: count=%d err=%v", count, err)
			}
		})
	}
}
