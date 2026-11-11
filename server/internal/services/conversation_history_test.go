package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func conversationHistoryFixture(t *testing.T) (*ConversationHistoryService, *gorm.DB, models.Sandbox) {
	t.Helper()
	db := newTestDB(t)
	sb := models.Sandbox{ID: 901, Name: "history-sandbox", RunID: "current-generation", ThreadID: "current-thread", Profile: "dev", Purpose: SandboxPurposeSolo, Status: "running"}
	session := models.SoloSession{ID: sb.ThreadID, UserID: "alice", AgentName: sb.Profile, SandboxID: sb.ID, Status: models.SoloStatusRunning, Title: "订单页面"}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewConversationHistoryService(db, func(runID string) (models.Sandbox, bool) { return sb, runID == sb.RunID })
	return svc, db, sb
}

func TestConversationHistoryPagesOriginalMessagesWithinSnapshot(t *testing.T) {
	svc, db, sb := conversationHistoryFixture(t)
	at := time.Date(2026, 10, 28, 1, 0, 0, 0, time.UTC)
	for i := 1; i <= 6; i++ {
		row := models.SoloMessage{ID: fmt.Sprintf("m%d", i), SessionID: sb.ThreadID, Role: "user", Content: fmt.Sprintf("要求%d", i), CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if i == 6 {
			row.Content = strings.Repeat("完整原文", 1000)
			row.Images = []models.PromptImage{{Data: "inline bytes must not leak", Ref: "blob:attachment", Name: "screen.png", MimeType: "image/png"}}
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	first, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 2})
	if err != nil || len(first.Messages) != 2 || first.Messages[0].ID != "m5" || first.Messages[1].ID != "m6" || !first.HasMore {
		t.Fatalf("first page: %+v %v", first, err)
	}
	if len([]rune(first.Messages[1].Content)) != 4000 || first.Messages[1].Images[0].Data != "" || first.Messages[1].Images[0].Ref != "blob:attachment" {
		t.Fatal("message truncated or inline image exposed")
	}
	if err := db.Create(&models.SoloMessage{ID: "m7", SessionID: sb.ThreadID, Role: "assistant", Content: "new after snapshot", CreatedAt: at.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	second, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 2, Cursor: first.NextCursor})
	if err != nil || second.HistoryVersion != first.HistoryVersion || len(second.Messages) != 2 || second.Messages[0].ID != "m3" || second.Messages[1].ID != "m4" {
		t.Fatalf("second page drifted: %+v %v", second, err)
	}
	last, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "summary", Limit: 2, Cursor: second.NextCursor})
	if err != nil || last.HasMore || last.NextCursor != "" || last.Messages[0].ID != "m1" || !strings.Contains(last.Summary, "要求1") {
		t.Fatalf("last page: %+v %v", last, err)
	}
}

func TestConversationHistoryRejectsForeignAndStaleScopes(t *testing.T) {
	svc, db, sb := conversationHistoryFixture(t)
	if _, err := svc.ReadConversationHistory("other-run", mcp.ConversationHistoryRequest{}); err == nil {
		t.Fatal("foreign run accepted")
	}
	for _, cursor := range []string{"bad", strings.Repeat("x", 4097), base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"another-thread","h":"m1"}`)), encodeConversationCursor(conversationCursor{Version: 1, SessionID: sb.ThreadID, SnapshotID: "missing"})} {
		if _, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{Cursor: cursor}); err == nil {
			t.Fatal("foreign, malformed or stale cursor accepted")
		}
	}
	for _, req := range []mcp.ConversationHistoryRequest{{View: "unknown"}, {Limit: -1}, {Limit: 51}} {
		if _, err := svc.ReadConversationHistory(sb.RunID, req); err == nil {
			t.Fatal("invalid page request accepted")
		}
	}
	empty, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{})
	if err != nil || len(empty.Messages) != 0 || !strings.Contains(empty.Summary, "尚无历史") {
		t.Fatalf("empty history: %+v %v", empty, err)
	}
	if err := db.Model(&models.SoloSession{}).Where("id = ?", sb.ThreadID).Update("sandbox_id", sb.ID+1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{}); err == nil {
		t.Fatal("previous sandbox generation retained history access")
	}
	if _, err := NewConversationHistoryService(nil, nil).ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{}); err == nil {
		t.Fatal("unavailable history accepted")
	}
}

func TestContextPreambleKeepsEarlyConstraintsAndRecentFullContent(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	for i := 0; i < 42; i++ {
		row := models.SoloMessage{ID: fmt.Sprintf("msg-%02d", i), SessionID: sb.ThreadID, Role: "assistant", Content: fmt.Sprintf("进度%d", i), CreatedAt: time.Date(2026, 10, 28, 1, 0, i, 0, time.UTC)}
		if i == 0 {
			row.Role, row.Content = "user", "按钮圆角19px，金额使用人民币"
		}
		if i == 40 {
			row.Content = strings.Repeat("长回复", 1000) + "最新进度尾部必须保留"
			row.Status, row.Error = "failed", "工具异常已有结果"
		}
		if i == 41 {
			row.Role, row.Content = "user", "本轮输入只能出现一次"
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	solo := &SoloService{db: db}
	pre := solo.contextPreamble(sb.ThreadID, "msg-41", "工作区保留\n")
	for _, required := range []string{"圆角19px", "最新进度尾部必须保留", "get_history", "工具异常已有结果"} {
		if !strings.Contains(pre, required) {
			t.Fatalf("missing context %q", required)
		}
	}
	if strings.Contains(pre, "本轮输入只能出现一次") || solo.contextPreamble("empty", "", "") != "" {
		t.Fatal("current input duplicated or empty history synthesized")
	}
}

func TestConversationHistoryIncludesVisibleToolResults(t *testing.T) {
	row := models.SoloMessage{Events: []json.RawMessage{
		json.RawMessage(`not JSON`),
		json.RawMessage(`{"params":{"update":{"sessionUpdate":"agent_thought_chunk","toolCallId":"ignored"}}}`),
		json.RawMessage(`{"params":{"update":{"sessionUpdate":"tool_call","toolCallId":"test","title":"npm test","status":"in_progress"}}}`),
		json.RawMessage(`{"params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"test","status":"completed","content":[{"content":{"type":"text","text":"42 tests passed"}},{"content":{"type":"image","data":"binary must not appear"}}]}}}`),
	}}
	message := conversationMessage(row)
	if len(message.ToolResults) != 1 || message.ToolResults[0].Title != "npm test" || message.ToolResults[0].Status != "completed" || message.ToolResults[0].Text != "42 tests passed" {
		t.Fatalf("tool continuity: %+v", message.ToolResults)
	}
	if got := historyExcerpt(strings.Repeat("大", 4000), 2000, ownHistoryTool); !strings.Contains(got, "摘录") || !strings.Contains(got, "get_history") {
		t.Fatal("large excerpts need an explicit retrieval instruction")
	}
}

func TestContextPreambleKeepsMidConversationUserCorrections(t *testing.T) {
	svc, db, sb := conversationHistoryFixture(t)
	for i := 0; i < 30; i++ {
		row := models.SoloMessage{ID: fmt.Sprintf("correction-%02d", i), SessionID: sb.ThreadID, Role: "assistant", Content: "后续工具进度", CreatedAt: time.Date(2026, 10, 28, 1, 0, i, 0, time.UTC)}
		if i < 5 {
			row.Role = "user"
			row.Content = fmt.Sprintf("早期要求%d，使用 Vue", i)
		}
		if i == 8 {
			row.Role = "user"
			row.Content = "更正：改用 React，取消支付功能"
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	solo := &SoloService{db: db}
	pre, err := solo.contextPreambleChecked(sb.ThreadID, "", "handoff\n")
	if err != nil || !strings.Contains(pre, "改用 React，取消支付功能") || !strings.Contains(pre, "较新的更正优先") || !strings.Contains(pre, "较早的 18 条对话正文") {
		t.Fatalf("middle correction disappeared: %s %v", pre, err)
	}
	page, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "summary", Limit: 2})
	if err != nil || !strings.Contains(page.Summary, "改用 React，取消支付功能") {
		t.Fatalf("summary lost correction: %+v %v", page, err)
	}
	var users []models.SoloMessage
	for i := 0; i < 20; i++ {
		users = append(users, models.SoloMessage{ID: fmt.Sprint(i), Content: strings.Repeat("要求", 1000)})
	}
	ledger := renderUserInstructions(users, 3000, ownHistoryTool)
	if !strings.Contains(ledger, "条用户消息未装入") || !strings.Contains(ledger, "[用户要求 · 19]") {
		t.Fatalf("missing budget disclosure/latest decision: %s", ledger)
	}
}

func TestConversationHistoryDoesNotRecursivelyEmbedHistoryToolResponses(t *testing.T) {
	for _, title := range []string{"mcp__artifact-store__get_history", "MCP tool"} {
		row := models.SoloMessage{Events: []json.RawMessage{
			json.RawMessage(fmt.Sprintf(`{"params":{"update":{"sessionUpdate":"tool_call","toolCallId":"history","title":%q}}}`, title)),
			json.RawMessage(`{"params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"history","status":"completed","content":[{"content":{"type":"text","text":"{\"history_version\":\"v1\",\"messages\":[{\"content\":\"nested old history must not recur\"}]}"}}]}}}`),
		}}
		tools := conversationMessage(row).ToolResults
		if len(tools) != 1 || tools[0].ID != "history" || tools[0].Status != "completed" || strings.Contains(tools[0].Text, "nested old history") || !strings.Contains(tools[0].Text, "已省略") {
			t.Fatalf("recursive history response: %+v", tools)
		}
	}
}
