package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestWorkflowFailedStartupPreservesExactIdentityWithoutReadyOrRetirement(t *testing.T) {
	a, _, _, _ := workspaceAuthorizationFixture(t)
	s := a.sandbox
	s.execution = a.execution
	run := models.Run{ID: "failed-startup-run", Status: "running"}
	if err := a.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Table("runs").Where("id = ?", run.ID).Update("attempt", 1).Error; err != nil {
		t.Fatal(err)
	}
	state := models.StateRun{RunID: run.ID, NodeID: "work", Status: "running", Iteration: 3, Attempt: 1, ProviderAttempt: 2}
	if err := a.db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	info := runtime.RunSandboxInfo{Name: "original-planned-create", RunID: run.ID, NodeID: state.NodeID, StateRunID: state.ID, Iteration: 3, RunAttempt: 1, ProviderAttempt: 2}
	s.BeginRunSandbox(info)
	var original models.Sandbox
	if err := a.db.Take(&original, "name = ?", info.Name).Error; err != nil {
		t.Fatal(err)
	}
	info.CorrelationName, info.Name, info.StartupError = info.Name, "original-provider-resource", "startup wait expired after create"
	s.RegisterRunSandbox(info)
	var retained models.Sandbox
	if err := a.db.Take(&retained, original.ID).Error; err != nil || retained.Name != info.Name || retained.Status != "error" || retained.Error == "" || retained.DestroyAt != nil {
		t.Fatalf("failed startup lost retention: %+v %v", retained, err)
	}
	var visit models.StateRun
	if err := a.db.Select("sandbox_ready_at", "provider_attempt").Take(&visit, state.ID).Error; err != nil || visit.SandboxReadyAt != nil || visit.ProviderAttempt != 2 {
		t.Fatalf("failed startup became executable/ready: %+v %v", visit, err)
	}
	s.mu.Lock()
	active := s.runActive[info.Name]
	s.mu.Unlock()
	if active {
		t.Fatal("unconfirmed startup presented a live agent")
	}
	if err := a.db.Table("state_runs").Where("id = ?", state.ID).Update("provider_attempt", 3).Error; err != nil {
		t.Fatal(err)
	}
	info.Name = "late-provider-result"
	s.RegisterRunSandbox(info)
	if err := a.db.Take(&retained, original.ID).Error; err != nil || retained.Name != "original-provider-resource" {
		t.Fatalf("old startup adopted a successor attempt: %+v %v", retained, err)
	}
}
