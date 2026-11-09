package services

import (
	"context"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloEnvironmentStopChecksRemoteAcceptedQueueAtomically(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 1)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stop fixture ready", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	turn := &soloTurn{ID: "remote-accepted-turn", ActorID: "alice", ClientMessageID: "remote-request", Text: "must remain queued"}
	if err := solo.queueFingerprint(turn, nil); err != nil {
		t.Fatal(err)
	}
	intent, err := solo.durableSoloIntent(turn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Admit(context.Background(), soloQueueScope(view.ID), intent, soloQueueCapacity, nil); err != nil {
		t.Fatal(err)
	}
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	if other.lookup(view.ID) != nil {
		t.Fatal("second replica fixture unexpectedly owns local chat")
	}
	_, err = other.StopEnvironment(context.Background(), view.ID, "alice")
	var failure *SoloError
	if !errors.As(err, &failure) || failure.Code != "session_busy" {
		t.Fatalf("remote accepted intent did not block stop: %v", err)
	}
	var environment models.Sandbox
	if err := db.First(&environment, view.SandboxID).Error; err != nil || environment.DesiredState == "stopped" || environment.Status != "running" {
		t.Fatalf("denied stop changed environment: %+v %v", environment, err)
	}
}
