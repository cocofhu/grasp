package services

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloFailedStopKeepsComputeReservedUntilObservedStopped(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 1)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	gateway.FailStop = true
	if _, err := solo.StopEnvironment(context.Background(), view.ID, "alice"); err == nil {
		t.Fatal("failed stop was acknowledged")
	}
	state, err := solo.Get(context.Background(), view.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status == models.SoloStatusStopped || state.Sandbox.DesiredState != "stopped" {
		t.Fatalf("intent confused with observation: %+v", state)
	}
	if sbx.activeSoloSandboxCount() != 1 {
		t.Fatal("failed stop freed still-running compute capacity")
	}
	if _, err := solo.Send(view.ID, "alice", "do not run while stop is pending", nil); err == nil {
		t.Fatal("pending stop admitted a new turn")
	}
	sbx.RecoverSoloSandboxes(context.Background())
	if sbx.activeSoloSandboxCount() != 1 {
		t.Fatal("failed reconciliation released compute quota")
	}
	gateway.FailStop = false
	sbx.RecoverSoloSandboxes(context.Background())
	state, err = solo.Get(context.Background(), view.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != models.SoloStatusStopped || sbx.activeSoloSandboxCount() != 0 {
		t.Fatalf("interrupted explicit stop did not reconcile: %+v", state)
	}
	sbx.RecoverSoloSandboxes(context.Background())
	state, err = solo.Get(context.Background(), view.ID, "alice")
	if err != nil || state.Status != models.SoloStatusStopped {
		t.Fatal("stopped compute was resumed", err)
	}
}
