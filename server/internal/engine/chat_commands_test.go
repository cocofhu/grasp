package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/chatcmd"
	"github.com/cocofhu/grasp/internal/models"
)

func TestClarifyContextCommandsSettleInQueue(t *testing.T) {
	f := newClarifyFixture(t)
	f.provider.mu.Lock()
	before := f.provider.reactReplyCalls["clarify"]
	f.provider.mu.Unlock()
	for _, text := range []string{"/compact", "/new chat"} {
		f.send(t, text)
	}
	f.waitIdle(t)
	f.provider.mu.Lock()
	after := f.provider.reactReplyCalls["clarify"]
	f.provider.mu.Unlock()
	if after != before {
		t.Fatalf("context commands consumed %d Agent turns", after-before)
	}
	var conv models.ReactConversation
	if err := f.eng.db.Where("run_id = ? AND node_id = ?", f.runID, "clarify").First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	state := models.ActiveChatContext(conv.Messages)
	if conv.Done || len(conv.Messages) < 4 || state.Pending != chatcmd.New || len(state.Messages) != 0 {
		t.Fatalf("workflow chat context: done=%v, messages=%d, context=%+v", conv.Done, len(conv.Messages), state)
	}
}
