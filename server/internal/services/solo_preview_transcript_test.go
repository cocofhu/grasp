package services

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestSoloPreviewTranscriptKeepsLiveProcessBeforeTextAndQueue(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
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
	waitFor(t, "active preview turn", func() bool { return gate.count() == 1 })
	chat := solo.lookup(view.ID)
	chat.onAcp(json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"Thinking before a reply"}}}`))
	chat.onAcp(json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"read-1","title":"read_file","status":"in_progress"}}`))
	if _, err := solo.Send(view.ID, "alice", "queued", nil); err != nil {
		t.Fatal(err)
	}
	solo.appendSystem(view.ID, "Model settings changed")
	rows, busy, err := solo.PreviewTranscript(view.ID, "alice")
	if err != nil || !busy || len(rows) != 3 || rows[0].Content != "first" || rows[1].Role != "assistant" || rows[2].Role != "system" {
		t.Fatalf("live turn with queued message: %+v busy=%v err=%v", rows, busy, err)
	}
	if !strings.HasPrefix(rows[1].ID, "live:") || rows[1].Content != "" || len(rows[1].Events) != 2 {
		t.Fatalf("thought/tool-only turn was hidden: %+v", rows[1])
	}
	rows[1].Events[0][0] = '!'
	again, _, _ := solo.PreviewTranscript(view.ID, "alice")
	if again[1].Events[0][0] == '!' {
		t.Fatal("snapshot mutated the active turn")
	}
	var queries atomic.Int32
	if err := db.Callback().Query().After("gorm:query").Register("preview-live-query-count", func(*gorm.DB) { queries.Add(1) }); err != nil {
		t.Fatal(err)
	}
	live, active := solo.PreviewLiveReply(view.ID)
	if live == nil || !active || len(live.Events) != 2 || queries.Load() != 0 {
		t.Fatalf("live snapshot read persisted history: active=%v queries=%d reply=%+v", active, queries.Load(), live)
	}
	if err := db.Callback().Query().Remove("preview-live-query-count"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloMessage{ID: "saved-reply", SessionID: view.ID, Role: "assistant", Content: "Saved"}).Error; err != nil {
		t.Fatal(err)
	}
	rows, _, err = solo.PreviewTranscript(view.ID, "alice")
	if err != nil || len(rows) != 3 || rows[2].ID != "saved-reply" {
		t.Fatalf("finished reply duplicated during settlement: %+v %v", rows, err)
	}
}
