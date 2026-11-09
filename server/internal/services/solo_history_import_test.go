package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

func TestSoloImportedProcessUsesOriginalMessageRangeAndWarning(t *testing.T) {
	db := newTestDB(t)
	solo := &SoloService{db: db, durable: chatsession.NewDurableStore(db)}
	ctx := context.Background()
	if err := db.Create(&models.SoloSession{ID: "imported-history", UserID: "alice", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for i, count := range []int{6000, 2, 0} {
		events := make([]json.RawMessage, count)
		for j := range events {
			events[j] = json.RawMessage(fmt.Sprintf(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"message %d event %d"}}`, i, j))
		}
		body, err := json.Marshal(events)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO solo_messages (id,session_id,role,content,events,created_at) VALUES (?,?,?,?,?,?)", fmt.Sprintf("old-%d", i), "imported-history", "assistant", fmt.Sprintf("old answer %d", i), string(body), at.Add(time.Duration(i)*time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := historymigration.ImportSoloEvents(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	page, err := solo.MessagePage(ctx, "imported-history", "alice", "", 10)
	if err != nil || len(page.Items) != 3 {
		t.Fatal(page, err)
	}
	for _, row := range page.Items {
		if len(row.Events) != 0 || row.EventLog == nil || row.EventLog.Legacy == nil || row.Execution != nil || row.TurnID != "" {
			t.Fatal("metadata read lost origin or fabricated live identity", row)
		}
	}
	first, err := solo.MessageEvents(ctx, "imported-history", "alice", "old-0", -1)
	if err != nil || !first.LegacyIncomplete || !first.HasMore || len(first.Items) != 256 {
		t.Fatal("imported truncation warning/page lost", first, err)
	}
	second, err := solo.MessageEvents(ctx, "imported-history", "alice", "old-1", -1)
	if err != nil || second.LegacyIncomplete || second.HasMore || len(second.Items) != 2 || second.Items[0].Seq != 6000 || second.Items[0].TurnID != "" {
		t.Fatal("old empty turn ID mixed two messages", second, err)
	}
	if _, err := solo.MessageEvents(ctx, "imported-history", "alice", "old-1", 0); err == nil {
		t.Fatal("cursor escaped exact imported range")
	}
	if _, err := solo.MessageEvents(ctx, "imported-history", "bob", "old-1", -1); err == nil {
		t.Fatal("import bypassed current owner authorization")
	}
	if _, err := solo.MessageEvents(ctx, "imported-history", "alice", "missing", -1); err == nil {
		t.Fatal("missing imported message was treated as another history")
	}
	empty, err := solo.MessageEvents(ctx, "imported-history", "alice", "old-2", -1)
	if err != nil || len(empty.Items) != 0 || empty.HasMore {
		t.Fatal("empty stored array invented process events", empty, err)
	}
	messages, err := solo.Messages("imported-history", "alice")
	if err != nil || len(messages) != 3 || len(messages[0].Events) != 6000 || len(messages[1].Events) != 2 || len(messages[2].Events) != 0 {
		t.Fatal("full original history mixed imported messages", len(messages), err)
	}
	for i, row := range messages {
		if row.Content != fmt.Sprintf("old answer %d", i) || row.Execution != nil || row.TurnID != "" {
			t.Fatal("original reply or execution identity changed")
		}
	}
}

func TestSoloImportedCommonLoaderFeedsMCPAndPreambleWithoutOldBody(t *testing.T) {
	history, db, sb := conversationHistoryFixture(t)
	events := `[ {"params":{"update":{"sessionUpdate":"tool_call","toolCallId":"original-tool","title":"npm test","status":"in_progress"}}}, {"params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"original-tool","status":"completed","content":[{"content":{"type":"text","text":"original tool result"}}]}}} ]`
	if err := db.Exec("INSERT INTO solo_messages (id,session_id,role,content,events,created_at) VALUES (?,?,?,?,?,?)", "imported-tools", sb.ThreadID, "assistant", "original visible answer", events, time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := historymigration.ImportSoloEvents(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := db.Table("solo_messages").Where("id = ?", "imported-tools").Pluck("events", &source).Error; err != nil {
		t.Fatal(err)
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	old, err := files.Open(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	name := old.Name()
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(name, name+".unavailable-original"); err != nil {
		t.Fatal(err)
	}
	// Metadata source-hash identity remains. The old array body is unavailable,
	// so a successful result proves both consumers read canonical chunks.
	page, err := history.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 10})
	if err != nil || len(page.Messages) != 1 || len(page.Messages[0].ToolResults) != 1 || page.Messages[0].ToolResults[0].Text != "original tool result" {
		t.Fatal("MCP still depends on original Events body", page, err)
	}
	solo := &SoloService{db: db}
	pre, err := solo.contextPreambleChecked(sb.ThreadID, "", "handoff")
	if err != nil || !strings.Contains(pre, "original tool result") || !strings.Contains(pre, "original visible answer") {
		t.Fatal("context still depends on original Events body", pre, err)
	}
	row, err := readSoloMessage(db.Where("id = ?", "imported-tools"), soloMessageAll)
	if err != nil || len(row.Events) != 2 || row.Execution != nil || row.TurnID != "" {
		t.Fatal("common loader changed history/provenance", row, err)
	}
	wrong := *row.EventLog
	wrong.Count--
	if _, err := readImportedMessageEvents(context.Background(), db, sb.ThreadID, row.ID, &wrong); err == nil {
		t.Fatal("aggregate accepted a shorter count")
	}
	wrong = *row.EventLog
	origin := *wrong.Legacy
	origin.SHA256 = strings.Repeat("f", 64)
	wrong.Legacy = &origin
	if _, err := readImportedMessageEvents(context.Background(), db, sb.ThreadID, row.ID, &wrong); err == nil {
		t.Fatal("aggregate accepted a different source hash")
	}
}
