package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
)

func conversationJournalFixture(t *testing.T, entries []chatsession.JournalEntry) (*ConversationHistoryService, *SoloService, *journalViewObjects, models.Sandbox, models.SoloMessage) {
	t.Helper()
	history, db, sb := conversationHistoryFixture(t)
	solo := &SoloService{db: db, durable: chatsession.NewDurableStore(db)}
	objects := &journalViewObjects{objects: map[string]journalViewObject{}}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	turn := &soloTurn{ID: "modern-history-turn", ActorID: "alice", ClientMessageID: "history-request", Fingerprint: strings.Repeat("a", 64), Text: "original request"}
	intent, err := solo.durableSoloIntent(turn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, scope := context.Background(), soloQueueScope(sb.ThreadID)
	if _, err := solo.durable.Admit(ctx, scope, intent, 0, nil); err != nil {
		t.Fatal(err)
	}
	lease, _, err := solo.durable.Acquire(ctx, scope, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(entries); offset += chatsession.JournalBatchEvents {
		end := min(offset+chatsession.JournalBatchEvents, len(entries))
		if _, err := solo.durable.AppendJournal(ctx, scope, &lease, turn.ID, entries[offset:end], nil); err != nil {
			t.Fatal(err)
		}
	}
	pointer, err := solo.journalPointer(db, sb.ThreadID, turn.ID, lease.Generation)
	if err != nil || pointer == nil {
		t.Fatalf("journal pointer: %+v %v", pointer, err)
	}
	message := models.SoloMessage{ID: "reply:" + turn.ID, SessionID: sb.ThreadID, TurnID: turn.ID, Role: "assistant", EventLog: pointer,
		CreatedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	return history, solo, objects, sb, message
}

func historyToolStart() chatsession.JournalEntry {
	return chatsession.JournalEntry{Type: chatsession.FrameAcp, Data: json.RawMessage(`{"params":{"update":{"sessionUpdate":"tool_call","toolCallId":"original-tool","title":"npm test","status":"in_progress"}}}`)}
}

func historyToolDone() chatsession.JournalEntry {
	return chatsession.JournalEntry{Type: chatsession.FrameAcp, Data: json.RawMessage(`{"params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"original-tool","status":"completed","content":[{"content":{"type":"text","text":"original tool result"}}]}}}`)}
}

func TestConversationHistoryModernJournalMergesToolsAcrossPagesWithoutSQLObjectIO(t *testing.T) {
	entries := []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: "phase", Payload: map[string]any{"phase": "running"}}, historyToolStart()}
	for i := 0; i < 300; i++ {
		entries = append(entries, chatsession.JournalEntry{Type: chatsession.FrameAcp, Data: json.RawMessage(`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"progress"}}}}`)})
	}
	entries = append(entries, historyToolDone(), chatsession.JournalEntry{Type: chatsession.FrameAcp, Data: json.RawMessage(`{"params":{"update":{"sessionUpdate":"tool_call","toolCallId":"recursive-history","title":"artifact-store.get_history","status":"completed","content":[{"content":{"type":"text","text":"private nested history"}}]}}}`)},
		chatsession.JournalEntry{Type: chatsession.FrameSession, Event: "phase", Payload: map[string]any{"phase": "done"}})
	history, solo, objects, sb, message := conversationJournalFixture(t, entries)
	probeViewSQLConnection(t, solo, objects)
	before := objects.gets.Load()
	page, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 1})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != message.ID {
		t.Fatalf("modern history: %+v %v", page, err)
	}
	tools := page.Messages[0].ToolResults
	if len(tools) != 2 || tools[0].ID != "original-tool" || tools[0].Title != "npm test" || tools[0].Status != "completed" || tools[0].Text != "original tool result" {
		t.Fatalf("tool start/update not merged across journal pages: %+v", tools)
	}
	if strings.Contains(tools[1].Text, "private nested history") || !strings.Contains(tools[1].Text, "历史查询正文已省略") || objects.gets.Load()-before < 3 {
		t.Fatalf("recursive history exposed or journal was not read: %+v GETs=%d", tools, objects.gets.Load()-before)
	}
}

func TestConversationHistoryModernJournalRejectsForeignScopeBeforeBodyReads(t *testing.T) {
	history, _, objects, _, _ := conversationJournalFixture(t, []chatsession.JournalEntry{historyToolStart(), historyToolDone()})
	before := objects.gets.Load()
	page, err := history.ReadConversationHistory("another-run", mcp.ConversationHistoryRequest{View: "messages"})
	if err == nil || len(page.Messages) != 0 || objects.gets.Load() != before {
		t.Fatalf("foreign history read objects or succeeded: %+v err=%v GETs=%d", page, err, objects.gets.Load()-before)
	}
}

func TestConversationHistoryModernJournalRejectsChangedOriginOrRange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*models.SoloMessage)
	}{
		{"version", func(m *models.SoloMessage) { m.EventLog.Version++ }},
		{"from", func(m *models.SoloMessage) { m.EventLog.From++ }},
		{"through", func(m *models.SoloMessage) { m.EventLog.Through++ }},
		{"count", func(m *models.SoloMessage) { m.EventLog.Count++ }},
		{"generation", func(m *models.SoloMessage) { m.EventLog.Generation++ }},
		{"turn", func(m *models.SoloMessage) { m.TurnID = "other-turn" }},
		{"role", func(m *models.SoloMessage) { m.Role = "user" }},
		{"zero-generation", func(m *models.SoloMessage) { m.EventLog.Generation = 0 }},
		{"negative-from", func(m *models.SoloMessage) { m.EventLog.From = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history, solo, objects, sb, message := conversationJournalFixture(t, []chatsession.JournalEntry{historyToolStart(), historyToolDone()})
			tc.mutate(&message)
			pointer, err := json.Marshal(message.EventLog)
			if err != nil {
				t.Fatal(err)
			}
			if err := solo.db.Table("solo_messages").Where("id = ?", message.ID).Updates(map[string]any{"event_log": string(pointer), "turn_id": message.TurnID, "role": message.Role}).Error; err != nil {
				t.Fatal(err)
			}
			before := objects.gets.Load()
			page, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages"})
			if !errors.Is(err, chatsession.ErrJournalGap) || len(page.Messages) != 0 || objects.gets.Load() != before {
				t.Fatalf("changed origin accepted or loaded: %+v err=%v GETs=%d", page, err, objects.gets.Load()-before)
			}
		})
	}
}

func TestConversationHistoryModernJournalRejectsMissingOrCorruptBody(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrupt", true: "missing"}[missing], func(t *testing.T) {
			history, solo, objects, sb, message := conversationJournalFixture(t, []chatsession.JournalEntry{historyToolStart(), historyToolDone()})
			if err := solo.db.Create(&models.SoloMessage{ID: "older-user", SessionID: sb.ThreadID, Role: "user", CreatedAt: message.CreatedAt.Add(-time.Second)}).Error; err != nil {
				t.Fatal(err)
			}
			objects.mu.Lock()
			for key, object := range objects.objects {
				if missing {
					delete(objects.objects, key)
				} else {
					object.body = []byte("corrupt original journal")
					objects.objects[key] = object
				}
			}
			objects.mu.Unlock()
			page, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 1})
			if err == nil || len(page.Messages) != 0 || page.HasMore || page.NextCursor != "" {
				t.Fatalf("missing/corrupt tool history silently succeeded: %+v %v", page, err)
			}
		})
	}
}

func TestConversationHistoryModernJournalOnlyLoadsSelectedMessagePage(t *testing.T) {
	history, solo, objects, sb, message := conversationJournalFixture(t, []chatsession.JournalEntry{historyToolStart(), historyToolDone()})
	if err := solo.db.Create(&models.SoloMessage{ID: "later-message", SessionID: sb.ThreadID, Role: "user", CreatedAt: message.CreatedAt.Add(time.Second)}).Error; err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	clear(objects.objects)
	objects.mu.Unlock()
	before := objects.gets.Load()
	first, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 1})
	if err != nil || len(first.Messages) != 1 || first.Messages[0].ID != "later-message" || !first.HasMore || first.NextCursor == "" || objects.gets.Load() != before {
		t.Fatalf("lookahead tool body was loaded: %+v %v", first, err)
	}
	next, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 1, Cursor: first.NextCursor})
	if err == nil || len(next.Messages) != 0 {
		t.Fatalf("selected missing tool body silently skipped: %+v %v", next, err)
	}
}

func TestConversationHistoryModernJournalSharesLimitsAcrossMessages(t *testing.T) {
	history, _, objects, sb, message := conversationJournalFixture(t, []chatsession.JournalEntry{historyToolStart(), historyToolDone()})
	legacy := models.SoloMessage{ID: "legacy", SessionID: sb.ThreadID, Role: "assistant", Events: make([]json.RawMessage, historymigration.MaxSourceEvents-1)}
	for i := range legacy.Events {
		legacy.Events[i] = json.RawMessage(`{}`)
	}
	before := objects.gets.Load()
	if err := history.hydrateJournalTools(historyScope{SessionID: sb.ThreadID}, []models.SoloMessage{legacy, message}); !errors.Is(err, errConversationHistoryBound) || objects.gets.Load() != before {
		t.Fatalf("per-message reset allowed request beyond shared event cap: %v", err)
	}
	// The byte counter must reject an additional row before integer addition,
	// while still accepting the exact existing cap. No 256 MiB fixture needed.
	var budget conversationJournalBudget
	if err := budget.add(1, historymigration.MaxSourceBytes); err != nil {
		t.Fatal(err)
	}
	if err := budget.add(1, 1); !errors.Is(err, errConversationHistoryBound) || budget.bytes != historymigration.MaxSourceBytes || budget.events != 1 {
		t.Fatalf("shared byte cap changed on refusal: %+v %v", budget, err)
	}
	oversized := message
	pointer := *message.EventLog
	pointer.Through = pointer.From + historymigration.MaxSourceEvents
	oversized.EventLog = &pointer
	if err := history.hydrateJournalTools(historyScope{SessionID: sb.ThreadID}, []models.SoloMessage{oversized}); !errors.Is(err, errConversationHistoryBound) || objects.gets.Load() != before {
		t.Fatalf("oversized modern range read objects: %v", err)
	}
}

func TestConversationHistoryModernJournalRejectsCrossGenerationACPInsideRange(t *testing.T) {
	history, solo, _, sb, message := conversationJournalFixture(t, []chatsession.JournalEntry{historyToolStart(), historyToolDone()})
	// A foreign generation cannot be silently ignored even if a corrupted SQL
	// index claims the same total range/count. LoadJournalChunk checks the
	// immutable body against that captured index before any tool is returned.
	if err := solo.db.Model(&models.ChatEventChunk{}).Where("queue_id = ?", soloQueueScope(sb.ThreadID).ID()).Update("execution_epoch", message.EventLog.Generation+1).Error; err != nil {
		t.Fatal(err)
	}
	pointer := *message.EventLog
	pointer.Generation++
	raw, err := json.Marshal(pointer)
	if err != nil {
		t.Fatal(err)
	}
	if err := solo.db.Table("solo_messages").Where("id = ?", message.ID).Update("event_log", string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	page, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages"})
	if !errors.Is(err, chatsession.ErrJournalGap) || len(page.Messages) != 0 {
		t.Fatalf("cross-generation ACP yielded tools: %+v %v", page, err)
	}
}

func TestConversationHistoryModernJournalKeepsCapturedRangeWhileCurrentTurnAppends(t *testing.T) {
	history, solo, _, sb, message := conversationJournalFixture(t, []chatsession.JournalEntry{historyToolStart(), historyToolDone()})
	var queue models.ChatQueue
	if err := solo.db.First(&queue, "id = ?", soloQueueScope(sb.ThreadID).ID()).Error; err != nil || queue.OwnerExpiresAt == nil {
		t.Fatalf("queue: %+v %v", queue, err)
	}
	lease := chatsession.QueueLease{Scope: soloQueueScope(sb.ThreadID), Owner: queue.Owner, Generation: queue.Generation, ExpiresAt: *queue.OwnerExpiresAt}
	if _, err := solo.durable.AppendJournal(context.Background(), lease.Scope, &lease, message.TurnID, []chatsession.JournalEntry{{Type: chatsession.FrameAcp,
		Data: json.RawMessage(`{"params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"original-tool","content":[{"content":{"type":"text","text":"later output outside the captured message"}}]}}}`)}}, nil); err != nil {
		t.Fatal(err)
	}
	// An artifact placeholder for the active turn has no event pointer yet.
	// It must remain readable without inventing a range from the live turn.
	if err := solo.db.Create(&models.SoloMessage{ID: "active-placeholder", SessionID: sb.ThreadID, TurnID: message.TurnID, Role: "assistant",
		Content: "current progress", CreatedAt: message.CreatedAt.Add(time.Second)}).Error; err != nil {
		t.Fatal(err)
	}
	page, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 2})
	if err != nil || len(page.Messages) != 2 || len(page.Messages[0].ToolResults) != 1 || page.Messages[0].ToolResults[0].Text != "original tool result" ||
		page.Messages[1].Content != "current progress" || len(page.Messages[1].ToolResults) != 0 {
		t.Fatalf("live turn changed captured tools or hid placeholder: %+v %v", page, err)
	}
}
