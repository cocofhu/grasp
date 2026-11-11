package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
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

func TestSoloStopRejectsMessagesThatArriveAfterTheIntent(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, gateway, db := newSoloFixture(t, port, 1)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stop admission fixture ready", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	gateway.BeforeInfo = func() {
		once.Do(func() { close(started) })
		<-release
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	errCh := make(chan error, 1)
	go func() {
		_, stopErr := solo.StopEnvironment(context.Background(), view.ID, "alice")
		errCh <- stopErr
	}()
	select {
	case <-started:
	case err := <-errCh:
		t.Fatalf("stop finished before discovery: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for stop to reach gateway discovery")
	}
	var environment models.Sandbox
	if err := db.First(&environment, view.SandboxID).Error; err != nil || environment.DesiredState != "stopped" {
		t.Fatalf("intent was not recorded before discovery: %+v %v", environment, err)
	}
	_, err = solo.SendWithReferences(view.ID, "alice", "late text", nil, []ReferenceInput{{Kind: "github_issue", Target: "acme/web#7"}}, models.ReactAnnotation{Selector: "#late", OuterHTML: "<p>late</p>"})
	var failure *SoloError
	if !errors.As(err, &failure) || failure.Code != "environment_stopped" {
		t.Fatalf("message during stop was accepted: %v", err)
	}
	var queued int64
	if err := db.Model(&models.ChatQueueTurn{}).Where("state IN ?", []string{chatsession.TurnQueued, chatsession.TurnExecuting}).Count(&queued).Error; err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("rejected message entered the queue: %d", queued)
	}
	messages, err := solo.Messages(view.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.Content == "late text" {
			t.Fatalf("rejected message entered the transcript: %+v", message)
		}
		for _, ann := range message.Annotations {
			if ann.Selector == "#late" {
				t.Fatalf("rejected pick entered the transcript: %+v", message)
			}
		}
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	stopped, err := solo.Get(context.Background(), view.ID, "alice")
	if err != nil || stopped.Status != models.SoloStatusStopped || stopped.Sandbox.DesiredState != "stopped" {
		t.Fatalf("stop did not finish after the rejected message: %+v %v", stopped, err)
	}
	if _, err := solo.Send(view.ID, "alice", "still stopped", nil); err == nil {
		t.Fatal("stopped environment accepted a message")
	}
}
