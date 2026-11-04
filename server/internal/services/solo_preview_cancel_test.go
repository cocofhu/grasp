package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestSoloPreviewCancelTargetsCurrentTurnAndPreservesPolicy(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, _ := newSoloFixture(t, port, 3)
	gate := newGateChat(true)
	solo.SetChatterForTest(gate)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = solo.Close(context.Background(), view.ID, "alice") }()
	if _, err := solo.Send(view.ID, "alice", "first", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "active preview", func() bool { return gate.count() == 1 })
	first := solo.PreviewActiveTurnID(view.ID)
	if first == "" {
		t.Fatal("active ID missing")
	}
	if _, err := solo.Send(view.ID, "alice", "queued", nil); err != nil {
		t.Fatal(err)
	}
	if solo.CancelPreviewTurn(view.ID, "bob", first) == nil {
		t.Fatal("wrong owner cancelled")
	}
	var stale *SoloError
	if err := solo.CancelPreviewTurn(view.ID, "alice", "stale"); !errors.As(err, &stale) || stale.Status != http.StatusConflict {
		t.Fatalf("stale: %v", err)
	}
	if solo.PreviewActiveTurnID(view.ID) != first || len(solo.PreviewPending(view.ID)) != 1 {
		t.Fatal("stale request mutated queue")
	}
	if err := solo.CancelPreviewTurn(view.ID, "alice", first); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "preview cancelled", func() bool { return solo.PreviewActiveTurnID(view.ID) == "" })
	if len(solo.PreviewPending(view.ID)) != 0 || gate.count() != 1 {
		t.Fatal("Solo stop did not clear pending turns")
	}
	if _, err := solo.Send(view.ID, "alice", "next", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "next preview", func() bool { return gate.count() == 2 })
	if solo.CancelPreviewTurn(view.ID, "alice", first) == nil || solo.PreviewActiveTurnID(view.ID) == first {
		t.Fatal("late cancel affected next turn")
	}
}
