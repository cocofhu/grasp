package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"gorm.io/gorm"
)

func TestWorkflowProviderRegistrationRejectsLateAttemptAndUnsealedSharedReuse(t *testing.T) {
	a, _, _, _ := workspaceAuthorizationFixture(t)
	s := a.sandbox
	s.execution = a.execution
	run := models.Run{ID: "workflow-provider-run", Status: "running"}
	if err := a.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	state := models.StateRun{RunID: run.ID, NodeID: "work", NodeType: "agent", Status: "running", Iteration: 2, Attempt: 1, ProviderAttempt: 2}
	if err := a.db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Table("runs").Where("id = ?", run.ID).Update("attempt", 1).Error; err != nil {
		t.Fatal(err)
	}
	info := runtime.RunSandboxInfo{Name: "planned-provider", RunID: run.ID, NodeID: state.NodeID, StateRunID: state.ID, Iteration: 2, RunAttempt: 1, ProviderAttempt: 2}
	s.BeginRunSandbox(info)
	var resource models.Sandbox
	if err := a.db.Take(&resource, "name = ?", info.Name).Error; err != nil {
		t.Fatal("exact planned resource missing", err)
	}
	info.CorrelationName, info.Name = info.Name, "native-provider-resource"
	s.RegisterRunSandbox(info)
	if err := a.db.Take(&resource, resource.ID).Error; err != nil || resource.Name != info.Name || resource.ProviderAttempt != 2 {
		t.Fatal(resource, err)
	}
	var original models.StateRun
	a.db.Select("sandbox_ready_at").Take(&original, state.ID)
	if original.SandboxReadyAt == nil {
		t.Fatal("exact ready stamp missing")
	}
	// The old provider callback must never claim the same native resource for
	// a newer allocator generation, even if it reaches the server late.
	a.db.Table("state_runs").Where("id = ?", state.ID).Update("provider_attempt", 3)
	late := info
	late.Name = "late-native-provider"
	s.RegisterRunSandbox(late)
	if err := a.db.Take(&resource, resource.ID).Error; err != nil || resource.Name != info.Name {
		t.Fatal("late callback overwrote source", resource, err)
	}
	var count int64
	a.db.Table("sandboxes").Where("name = ?", late.Name).Count(&count)
	if count != 0 {
		t.Fatal("late source inserted", count)
	}
	// A sealed decision owns the physical workspace until its verified files
	// and route atomically settle. A different visit cannot rebind that row.
	checkpoint := models.WorkflowVisitCheckpoint{ID: strings.Repeat("a", 64), RunID: run.ID, StateRunID: state.ID, SandboxID: resource.ID, RouteState: "sealed", FileState: "pending"}
	if err := a.db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Transaction(func(tx *gorm.DB) error { return GuardWorkflowWorkspaceReuseTx(context.Background(), tx, resource.ID) }); !errors.Is(err, ErrWorkflowCheckpointBlocked) {
		t.Fatal("unsealed reuse admitted", err)
	}
	if err := a.db.Model(&checkpoint).Update("route_state", "applied").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Transaction(func(tx *gorm.DB) error { return GuardWorkflowWorkspaceReuseTx(context.Background(), tx, resource.ID) }); err != nil {
		t.Fatal("settled reuse blocked", err)
	}
}
