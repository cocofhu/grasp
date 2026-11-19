package services

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloCommandsUseTheSameSandbox(t *testing.T) {
	prompts := make(chan string, 12)
	acp, port := fakeACPServerFramesOnChat(t, func(raw []byte) {
		var message struct{ Content string }
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Error(err)
		}
		prompts <- message.Content
	},
		map[string]any{"type": "session_update", "update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "done"},
		}},
		map[string]any{"type": "prompt_done"},
	)
	t.Cleanup(acp.Close)
	solo, sbx, _, db := newSoloFixture(t, port, 3)
	shared := NewSharedAgentService(t.TempDir())
	if err := shared.Save(SharedAgentConfig{Files: []AgentFile{
		{Path: "skills/review/SKILL.md", Content: "# Review"},
		{Path: "skills/incomplete/README.md", Content: "# No skill entry"},
	}}); err != nil {
		t.Fatal(err)
	}
	sbx.SetSharedAgent(shared)
	agent, _ := solo.agents.Get("agentA")
	agent.Files = append(agent.Files, AgentFile{Path: "skills/notes/SKILL.md", Content: "# Notes"})
	if err := solo.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(view.Skills, []string{"notes", "review"}) {
		t.Fatalf("skills = %v", view.Skills)
	}
	sandboxID := view.SandboxID
	send := func(text string) {
		t.Helper()
		if _, err := solo.SendWithReferences(view.ID, "alice", text, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{
		"old requirement " + strings.Repeat("log ", 8000),
		"/compact",
		"continue the same task",
		"/new chat",
		"/skill review check this draft\nkeep the examples",
		"/tmp/work.txt",
	} {
		send(text)
	}
	waitFor(t, "queued Solo commands and turns", func() bool {
		c := solo.lookup(view.ID)
		state := c.sess.Snapshot()
		return !state.Busy && state.Waiting == 0
	})
	if got := len(prompts); got != 4 {
		t.Fatalf("model calls = %d, want four regular turns", got)
	}
	<-prompts
	compacted := <-prompts
	if !strings.HasPrefix(compacted, soloCompactPreambleHeader) || !strings.Contains(compacted, "old requirement") ||
		!strings.HasSuffix(compacted, "continue the same task") || len([]rune(compacted)) > 16200 {
		t.Fatalf("unexpected compacted prompt (%d runes)", len([]rune(compacted)))
	}
	skillPrompt := <-prompts
	if !strings.Contains(skillPrompt, "/root/.cursor/skills/review/SKILL.md") ||
		!strings.HasSuffix(skillPrompt, "check this draft\nkeep the examples") ||
		strings.Contains(skillPrompt, "old requirement") {
		t.Fatalf("new chat skill prompt = %q", skillPrompt)
	}
	if got := <-prompts; got != "/tmp/work.txt" {
		t.Fatalf("ordinary slash-leading text = %q", got)
	}
	row, err := solo.load(view.ID)
	if err != nil || row.SandboxID != sandboxID || row.ContextResetPending != "" || row.ContextID == "" {
		t.Fatalf("Solo state = %+v, err = %v", row, err)
	}
	sbx.mu.Lock()
	bridgeSession := sbx.live[sandboxID].acp.SessionID()
	sbx.mu.Unlock()
	if bridgeSession != "sess-3" {
		t.Fatalf("native session = %q after two resets", bridgeSession)
	}
	messages, err := solo.Messages(view.ID, "alice")
	if err != nil || len(messages) != 10 || messages[2].Status != "context_compact" || messages[5].Status != "context_new" {
		t.Fatalf("expected complete transcript and context markers, got %d messages, err = %v", len(messages), err)
	}

	sandbox, err := sbx.Get(sandboxID)
	if err != nil {
		t.Fatal(err)
	}
	history := NewConversationHistoryService(db, func(string) (models.Sandbox, bool) { return *sandbox, true })
	page, err := history.ReadConversationHistory(sandbox.RunID, mcp.ConversationHistoryRequest{View: "messages"})
	if err != nil || len(page.Messages) != 5 || page.Messages[0].Content != "/new chat" {
		t.Fatalf("active conversation history = %+v, err = %v", page, err)
	}
	if err := db.Model(row).Update("session_context_pending", true).Error; err != nil {
		t.Fatal(err)
	}
	send("continue after recovery")
	waitFor(t, "recovered Solo turn", func() bool { return len(prompts) == 1 })
	recovered := <-prompts
	if !strings.HasPrefix(recovered, soloSessionResetPreambleHeader) ||
		!strings.Contains(recovered, "check this draft") || strings.Contains(recovered, "old requirement") {
		t.Fatalf("recovered prompt = %q", recovered)
	}
	waitFor(t, "Solo queue idle", func() bool { return !solo.lookup(view.ID).sess.Snapshot().Busy })
}

func TestSoloCommandValidation(t *testing.T) {
	solo := &SoloService{}
	for _, tc := range []struct {
		turn soloTurn
		code string
	}{
		{soloTurn{Text: "/skill"}, "skill_required"},
		{soloTurn{Text: "/skill ../private"}, "skill_not_found"},
		{soloTurn{Text: "/new", Images: []models.PromptImage{{Name: "draft"}}}, "command_has_attachments"},
		{soloTurn{Text: "/new", PreviewShared: true}, "command_context_shared"},
	} {
		var rejection *SoloError
		if err := solo.prepareSoloCommand("agent", &tc.turn, false); !errors.As(err, &rejection) || rejection.Code != tc.code {
			t.Fatalf("%q: expected %s, got %v", tc.turn.Text, tc.code, err)
		}
	}
}
