package services

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestPmThreadAvailabilityFollowsOriginalAgentAndPreservesHistory(t *testing.T) {
	db := newTestDB(t)
	agents := NewAgentService(t.TempDir())
	pm := NewPmService(db, agents)
	for _, name := range []string{"original", "new-default"} {
		if err := agents.Save(Agent{Name: name, ProjectID: models.DefaultProjectID, AcpBackend: AcpBackendCursor}); err != nil {
			t.Fatal(err)
		}
	}
	enabled, current := true, "new-default"
	if _, err := pm.UpdateBinding(models.DefaultProjectID, &enabled, &current, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	thread, err := pm.CreateThread(models.DefaultProjectID, "alice", "Retained conversation", "original", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pm.AppendMessage(thread.ID, "assistant", "Retained reply", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = agents.Delete(current); err != nil {
		t.Fatal(err)
	}
	if _, err = pm.RequireEnabled(models.DefaultProjectID); !errors.Is(err, ErrPmLeaderAgentMissing) {
		t.Fatalf("new threads require the default Agent: %v", err)
	}
	if _, err = pm.RequireSendEnabled(models.DefaultProjectID); err != nil {
		t.Fatal("missing default blocked existing thread", err)
	}
	view := pm.ThreadView(thread)
	if !view.AgentAvailable || view.AgentName != "original" || view.AgentError != "" {
		t.Fatalf("original Agent availability=%+v", view)
	}
	if err = agents.Delete("original"); err != nil {
		t.Fatal(err)
	}
	threads, err := pm.ListThreads(models.DefaultProjectID, "alice")
	if err != nil || len(threads) != 1 {
		t.Fatalf("history disappeared: %+v %v", threads, err)
	}
	views := pm.ThreadViews(threads)
	if views[0].AgentAvailable || views[0].AgentError == "" || views[0].AgentName != "original" {
		t.Fatalf("deleted Agent availability=%+v", views[0])
	}
	messages, err := pm.ListMessages(thread.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "Retained reply" {
		t.Fatalf("message history lost: %+v %v", messages, err)
	}
	if _, err = pm.GetThread(models.DefaultProjectID, thread.ID, "bob"); !errors.Is(err, ErrPmThreadNotFound) {
		t.Fatal("availability bypassed owner permission", err)
	}
	if err = agents.Save(Agent{Name: "original", ProjectID: models.DefaultProjectID, AcpBackend: AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	if !pm.ThreadViews(threads)[0].AgentAvailable {
		t.Fatal("repaired Agent remained unavailable")
	}
	enabled = false
	if _, err = pm.UpdateBinding(models.DefaultProjectID, &enabled, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !pm.ThreadView(thread).AgentAvailable {
		t.Fatal("send switch changed Agent existence")
	}
	if _, err = pm.RequireSendEnabled(models.DefaultProjectID); !errors.Is(err, ErrPmLeaderDisabled) {
		t.Fatal("send switch bypassed", err)
	}
}

func TestPmThreadAvailabilityFailsClosedWithoutMatchingAgentIdentity(t *testing.T) {
	pm := NewPmService(newTestDB(t), NewAgentService(t.TempDir()))
	if err := pm.skills.Save(Agent{Name: "foreign", ProjectID: "another", AcpBackend: AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "missing", "foreign"} {
		view := pm.ThreadView(models.ChatThread{AgentName: name, ProjectID: models.DefaultProjectID})
		if view.AgentAvailable || view.AgentError == "" {
			t.Fatalf("unavailable identity=%+v", view)
		}
		body, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err = json.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		if available, ok := wire["agentAvailable"].(bool); !ok || available {
			t.Fatalf("required false boolean missing: %s", body)
		}
	}
}
