package contextmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupContextDB(t *testing.T) (*gorm.DB, *services.PmService, models.Project) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:contextmcp_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	p, err := ps.Create("CtxMCP", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return db, services.NewPmService(db, nil), p
}

func ctxToolCall(id int, name string, args map[string]any) []byte {
	if args == nil {
		args = map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	return b
}

func ctxToolText(t *testing.T, resp []byte) (string, bool) {
	t.Helper()
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("parse: %v body=%s", err, resp)
	}
	if len(out.Result.Content) == 0 {
		return "", out.Result.IsError
	}
	return out.Result.Content[0].Text, out.Result.IsError
}

func TestContextMCPAuthToolsAndIsolation(t *testing.T) {
	_, pm, p := setupContextDB(t)
	ta, err := pm.CreateThread(p.ID, "alice", "A会话", "agent-a", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	taOther, err := pm.CreateThread(p.ID, "carol", "C会话", "agent-a", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := pm.CreateThread(p.ID, "bob", "B会话", "agent-b", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	cronTh, err := pm.CreateCronThread(p.ID, "agent-a", "定时会话")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(ta.ID, "user", "hello-a", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(taOther.ID, "user", "hello-carol", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(tb.ID, "user", "hello-b", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(cronTh.ID, "user", "hello-cron", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	h := NewHost(pm)
	tokA := platformmcp.NewToken()
	h.Restore(tokA, p.ID, "agent-a", ta.ID, "alice")
	if _, ok := h.Authorize(p.ID, tokA); !ok {
		t.Fatal("authorize")
	}
	if _, ok := h.Authorize("other", tokA); ok {
		t.Fatal("other project")
	}

	listBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	st, resp := h.ServeRPC(p.ID, tokA, listBody)
	if st != 200 {
		t.Fatalf("tools/list %d", st)
	}
	for _, name := range []string{
		"list_conversations", "get_messages", "search_messages",
		"get_current_conversation", "get_attached_context",
	} {
		if !strings.Contains(string(resp), `"name":"`+name+`"`) {
			t.Fatalf("missing %s", name)
		}
	}

	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(2, "list_conversations", nil))
	text, isErr := ctxToolText(t, resp)
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, ta.ID) || !strings.Contains(text, cronTh.ID) {
		t.Fatalf("list must include own + cron: %s", text)
	}
	if strings.Contains(text, tb.ID) || strings.Contains(text, taOther.ID) {
		t.Fatalf("list must not include other agents/users: %s", text)
	}

	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(3, "get_messages", map[string]any{
		"conversationId": tb.ID,
	}))
	text, isErr = ctxToolText(t, resp)
	if !isErr || !strings.Contains(text, "conversation not found") {
		t.Fatalf("cross-agent get_messages want not found, got isErr=%v %s", isErr, text)
	}

	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(3, "get_messages", map[string]any{
		"conversationId": taOther.ID,
	}))
	text, isErr = ctxToolText(t, resp)
	if !isErr || !strings.Contains(text, "conversation not found") {
		t.Fatalf("cross-user get_messages want not found, got isErr=%v %s", isErr, text)
	}

	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(4, "get_messages", map[string]any{
		"conversationId": ta.ID,
	}))
	text, isErr = ctxToolText(t, resp)
	if isErr || !strings.Contains(text, "hello-a") {
		t.Fatalf("own messages: %s", text)
	}

	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(5, "get_current_conversation", nil))
	text, isErr = ctxToolText(t, resp)
	if isErr || !strings.Contains(text, ta.ID) {
		t.Fatalf("current: %s", text)
	}

	h.SetAttached(tokA, &models.AttachedContext{Kind: "run", ID: "run-1", Label: "demo"})
	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(6, "get_attached_context", nil))
	text, isErr = ctxToolText(t, resp)
	if isErr || !strings.Contains(text, "run-1") {
		t.Fatalf("attached: %s", text)
	}

	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(7, "search_messages", map[string]any{"query": "hello"}))
	text, isErr = ctxToolText(t, resp)
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, "hello-a") || strings.Contains(text, "hello-b") || strings.Contains(text, "hello-carol") {
		t.Fatalf("search must be user-scoped (+cron): %s", text)
	}
	if !strings.Contains(text, "hello-cron") {
		t.Fatalf("search should include cron thread: %s", text)
	}
	st, resp = h.ServeRPC(p.ID, tokA, ctxToolCall(8, "search_messages", map[string]any{"query": ""}))
	text, isErr = ctxToolText(t, resp)
	if !isErr {
		t.Fatalf("empty query should error: %s", text)
	}
	_ = st
}

func TestContextMCPRestoreAndUnregister(t *testing.T) {
	_, pm, p := setupContextDB(t)
	h := NewHost(pm)
	tok := platformmcp.NewToken()
	h.Restore(tok, p.ID, "agent-a", "thr-1", "alice")
	h.Restore(tok, p.ID, "agent-a", "thr-1", "alice")
	h.Restore("", p.ID, "agent-a", "thr-1", "alice")
	if _, ok := h.Authorize(p.ID, tok); !ok {
		t.Fatal("restored token")
	}
	h.Unregister(tok)
	if _, ok := h.Authorize(p.ID, tok); ok {
		t.Fatal("unregistered")
	}
}
