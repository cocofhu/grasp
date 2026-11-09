package services

import (
	"encoding/json"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
	"testing"
)

func nativeAuthorizationFixture(t *testing.T) (*WorkspaceAuthorization, runtime.NodeReq, map[string]string) {
	t.Helper()
	a, job, credentials, _ := workspaceAuthorizationFixture(t)
	var original models.Sandbox
	if err := a.db.Take(&original, job.SandboxID).Error; err != nil {
		t.Fatal(err)
	}
	run := models.Run{ID: "native-authorized-run", Status: "running", WorkspaceActorID: "alice", OriginSoloID: job.ScopeID, Attempt: 2}
	if err := a.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	state := models.StateRun{RunID: run.ID, NodeID: "agent", NodeType: "agent", Iteration: 3, Attempt: 2, ProviderAttempt: 4, ProviderOperationID: "g-original-native", ProviderDispatchState: "preparing", Status: "running"}
	if err := a.db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	resource := models.Sandbox{Name: "original-native-provider", Purpose: "run", RunID: run.ID, NodeID: state.NodeID, Profile: original.Profile, StateRunID: state.ID, Iteration: 3, RunAttempt: 2, ProviderAttempt: 4, Status: "running", Generation: 5}
	if err := a.db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	req := runtime.NodeReq{RunID: run.ID, NodeID: state.NodeID, StateRunID: state.ID, RunAttempt: 2, Iteration: 3, ProviderAttempt: 4, Config: map[string]any{"agent_profile": resource.Profile}, Operation: sandbox.OperationIdentity{ID: state.ProviderOperationID, Attempt: 4, Resource: resource.Name, SandboxID: resource.ID, Generation: 5, SessionID: "native-session", Workspace: "/workspace"}}
	digest, err := a.NativeAuthorization(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Operation.Authorization = digest
	raw, _ := json.Marshal(req.Operation)
	if err := a.db.Table("state_runs").Where("id = ?", state.ID).Updates(map[string]any{"provider_dispatch_state": "dispatched", "provider_operation_receipt": string(raw)}).Error; err != nil {
		t.Fatal(err)
	}
	req.AttachOnly = true
	return a, req, credentials
}

func TestNativeAttachAuthorizationRevalidatesOriginalGrantAndExactMetadata(t *testing.T) {
	a, req, _ := nativeAuthorizationFixture(t)
	got, err := a.NativeAuthorization(t.Context(), req)
	if err != nil || got != req.Operation.Authorization {
		t.Fatal(got, err)
	}
	for _, change := range []string{"credential", "source-generation", "source-visit", "operation", "parent-owner", "parent-attempt", "quarantine"} {
		t.Run(change, func(t *testing.T) {
			a, req, creds := nativeAuthorizationFixture(t)
			switch change {
			case "credential":
				a.db.Model(&models.Credential{}).Where("id = ?", creds["model"]).Update("secret_revision", gorm.Expr("secret_revision+1"))
			case "source-generation":
				a.db.Table("sandboxes").Where("id = ?", req.Operation.SandboxID).Update("generation", 6)
			case "source-visit":
				a.db.Table("sandboxes").Where("id = ?", req.Operation.SandboxID).Update("iteration", 4)
			case "operation":
				req.Operation.ID = "g-other"
			case "parent-owner":
				a.db.Table("solo_sessions").Where("id = ?", "authorized-solo").Update("user_id", "removed-account")
			case "parent-attempt":
				a.db.Table("runs").Where("id = ?", req.RunID).Update("attempt", 3)
			case "quarantine":
				a.db.Table("state_runs").Where("id = ?", req.StateRunID).Update("workspace_checkpoint_id", "blocked-original")
			}
			if _, err := a.NativeAuthorization(t.Context(), req); err == nil {
				t.Fatal("changed authorization/identity admitted")
			}
		})
	}
}

func TestNativeAttachOriginSoloComputeLifecycleIsIndependentOfWorkflowGrant(t *testing.T) {
	a, req, _ := nativeAuthorizationFixture(t)
	if err := a.db.Table("solo_sessions").Where("id = ?", "authorized-solo").Updates(map[string]any{"status": models.SoloStatusStopped, "lifecycle_epoch": 99, "lifecycle_attempt_id": "solo-new-independent-environment"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := a.NativeAuthorization(t.Context(), req); err != nil {
		t.Fatal("independent Solo environment changed workflow authorization", err)
	}
}

func TestNativeAttachResolvesCurrentCredentialsBeforeAnySQLTransaction(t *testing.T) {
	a, req, _ := nativeAuthorizationFixture(t)
	pool, err := a.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	original := a.sandbox.modelBinding
	calls := 0
	a.sandbox.modelBinding = func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
		calls++
		if pool.Stats().InUse != 0 {
			t.Fatal("current secret/model resolver ran while SQL was held")
		}
		return original(backend, binding)
	}
	if _, err := a.NativeAuthorization(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("saved receipt replaced current authorization resolution")
	}
}
