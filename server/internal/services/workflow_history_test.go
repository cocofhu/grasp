package services

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkflowHistoryPrivateOriginAndDeletedFallback(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 10)
	origin := models.SoloSession{ID: "private-origin", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStopped}
	if err := db.Create(&origin).Error; err != nil {
		t.Fatal(err)
	}
	run := models.Run{ID: "history-run", OriginSoloID: origin.ID, CreatedBy: "alice", Status: "completed", WorkflowName: "Build", Title: "History",
		McpToken: "never-copy-this-token", FirstMessage: &models.CompositeText{Text: "original requirement"}}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloMessage{ID: "private", SessionID: origin.ID, Role: "user", Content: "private Solo text"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ user, want string }{{"alice", origin.ID}, {"bob", ""}} {
		id, err := solo.WorkflowChat(run.ID, tc.user)
		if err != nil || id != tc.want {
			t.Fatalf("%s: %q %v", tc.user, id, err)
		}
	}
	var sessions, sandboxes int64
	db.Model(&models.SoloSession{}).Count(&sessions)
	db.Model(&models.Sandbox{}).Count(&sandboxes)
	if sessions != 1 || sandboxes != 0 {
		t.Fatal("passive history read created resources")
	}
	view, err := solo.ContinueWorkflow(context.Background(), run.ID, "bob", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	if view.ID == origin.ID || view.UserID != "bob" {
		t.Fatal("private origin was reused")
	}
	messages, err := solo.Messages(view.ID, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !strings.Contains(soloHistoryContent(messages[0]), "original requirement") || strings.Contains(soloHistoryContent(messages[0]), "private Solo text") || strings.Contains(soloHistoryContent(messages[0]), "never-copy-this-token") {
		t.Fatalf("wrong continuation context: %+v", messages)
	}
	preamble, err := solo.contextPreambleChecked(view.ID, "none", "history")
	if err != nil || !strings.Contains(preamble, "original requirement") {
		t.Fatalf("model did not receive Run context: %q %v", preamble, err)
	}
	// A repeated action reuses the committed identity even if an Agent was
	// removed/changed; it does not create an additional environment or Run.
	again, err := solo.ContinueWorkflow(context.Background(), run.ID, "bob", SoloCreateInput{AgentName: "deleted-agent"})
	if err != nil || again.ID != view.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if err := db.Delete(&models.SoloSession{}, "id = ?", origin.ID).Error; err != nil {
		t.Fatal(err)
	}
	id, err := solo.WorkflowChat(run.ID, "alice")
	if err != nil || id != "" {
		t.Fatalf("deleted origin resolved: %q %v", id, err)
	}
	newChat, err := solo.ContinueWorkflow(context.Background(), run.ID, "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil || newChat.ID == origin.ID {
		t.Fatalf("deleted chat resurrected: %+v %v", newChat, err)
	}
}

func TestWorkflowHistoryConcurrentContinuationUsesOneChat(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 10)
	if err := db.Create(&models.Run{ID: "legacy-run", Status: "failed"}).Error; err != nil {
		t.Fatal(err)
	}
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := range 8 {
		wg.Go(func() {
			service := solo
			if i%2 == 0 {
				service = other
			}
			view, err := service.ContinueWorkflow(context.Background(), "legacy-run", "alice", SoloCreateInput{AgentName: "agentA"})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- view.ID
		})
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate continuation chats")
		}
	}
	var count int64
	for _, model := range []any{&models.SoloSession{}, &models.SoloWorkflowLink{}, &models.SoloMessage{}, &models.Sandbox{}} {
		if err := db.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%T: %d", model, count)
		}
	}
}
