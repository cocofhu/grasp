package memorymcp

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

func setupMemoryDB(t *testing.T) (*gorm.DB, *services.PmService, models.Project) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:memorymcp_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	p, err := ps.Create("MemMCP", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return db, services.NewPmService(db, nil), p
}

func toolCallBody(id int, name string, args map[string]any) []byte {
	if args == nil {
		args = map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	return b
}

func parseToolText(t *testing.T, resp []byte) (text string, isErr bool) {
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

func TestMemoryMCPAuthAndTools(t *testing.T) {
	_, pm, p := setupMemoryDB(t)
	h := NewHost(pm)
	tok := platformmcp.NewToken()
	h.Restore(tok, p.ID, "agent-a", "thr-1", "alice", true)
	if _, ok := h.Authorize(p.ID, tok); !ok {
		t.Fatal("authorize")
	}
	if _, ok := h.Authorize(p.ID, "bad"); ok {
		t.Fatal("bad token")
	}
	if _, ok := h.Authorize("other", tok); ok {
		t.Fatal("other project")
	}

	listBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	})
	st, resp := h.ServeRPC(p.ID, tok, listBody)
	if st != 200 {
		t.Fatalf("list status=%d", st)
	}
	body := string(resp)
	for _, name := range []string{"list_memories", "get_memory", "search_memories", "upsert_memory", "delete_memory"} {
		if !strings.Contains(body, `"name":"`+name+`"`) {
			t.Fatalf("missing tool %s in %s", name, body)
		}
	}
	h.Unregister(tok)
	if _, ok := h.Authorize(p.ID, tok); ok {
		t.Fatal("unregistered")
	}
}

func TestMemoryMCPRestoreToken(t *testing.T) {
	_, pm, p := setupMemoryDB(t)
	h := NewHost(pm)
	const tok = "restored-token-abc"
	h.Restore(tok, p.ID, "agent-a", "thr", "u", true)
	if _, ok := h.Authorize(p.ID, tok); !ok {
		t.Fatal("restored token should authorize")
	}
	st, resp := h.ServeRPC(p.ID, tok, toolCallBody(1, "upsert_memory", map[string]any{
		"title": "restored", "content": "ok",
	}))
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	text, isErr := parseToolText(t, resp)
	if isErr {
		t.Fatalf("upsert via restore: %s", text)
	}
	h.Restore("", p.ID, "agent-a", "thr", "u", true) // no-op
	if _, ok := h.Authorize(p.ID, ""); ok {
		t.Fatal("empty token must not authorize")
	}
}

func TestMemoryMCPWriteAllowedGate(t *testing.T) {
	_, pm, p := setupMemoryDB(t)
	h := NewHost(pm)
	tok := platformmcp.NewToken()
	h.Restore(tok, p.ID, "agent-a", "thr", "u", false)
	st, resp := h.ServeRPC(p.ID, tok, toolCallBody(1, "upsert_memory", map[string]any{
		"title": "t", "content": "c",
	}))
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	text, isErr := parseToolText(t, resp)
	if !isErr || !strings.Contains(text, "当前渠道未允许写入记忆") {
		t.Fatalf("want write gate, got isErr=%v text=%s", isErr, text)
	}
	st, resp = h.ServeRPC(p.ID, tok, toolCallBody(2, "delete_memory", map[string]any{"id": "x"}))
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	text, isErr = parseToolText(t, resp)
	if !isErr || !strings.Contains(text, "当前渠道未允许写入记忆") {
		t.Fatalf("want write gate on delete, got isErr=%v text=%s", isErr, text)
	}
}

func TestMemoryMCPAgentIsolationAndCRUD(t *testing.T) {
	_, pm, p := setupMemoryDB(t)
	h := NewHost(pm)
	tokA := platformmcp.NewToken()
	h.Restore(tokA, p.ID, "agent-a", "thr-a", "alice", true)
	tokB := platformmcp.NewToken()
	h.Restore(tokB, p.ID, "agent-b", "thr-b", "bob", true)
	st, resp := h.ServeRPC(p.ID, tokA, toolCallBody(1, "upsert_memory", map[string]any{
		"title": "共享标题", "content": "secret-a",
	}))
	if st != 200 {
		t.Fatalf("upsert status=%d", st)
	}
	text, isErr := parseToolText(t, resp)
	if isErr {
		t.Fatalf("upsert err: %s", text)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text), &created); err != nil || created.ID == "" {
		t.Fatalf("upsert body=%s err=%v", text, err)
	}

	st, resp = h.ServeRPC(p.ID, tokB, toolCallBody(2, "list_memories", nil))
	text, isErr = parseToolText(t, resp)
	if isErr {
		t.Fatal(text)
	}
	if strings.Contains(text, "secret-a") || strings.Contains(text, created.ID) {
		t.Fatalf("agent-b must not see agent-a memory: %s", text)
	}

	st, resp = h.ServeRPC(p.ID, tokB, toolCallBody(3, "delete_memory", map[string]any{"id": created.ID}))
	text, isErr = parseToolText(t, resp)
	if !isErr {
		t.Fatalf("cross-agent delete by id should fail, got %s", text)
	}
	if _, err := pm.GetMemory(p.ID, "agent-a", created.ID); err != nil {
		t.Fatalf("row should remain: %v", err)
	}

	// Same title for B must not delete A's row via title path.
	if _, err := pm.UpsertMemory(p.ID, "agent-b", "共享标题", "secret-b", "agent", "b"); err != nil {
		t.Fatal(err)
	}
	st, resp = h.ServeRPC(p.ID, tokB, toolCallBody(4, "delete_memory", map[string]any{"title": "共享标题"}))
	text, isErr = parseToolText(t, resp)
	if isErr {
		t.Fatalf("delete by title: %s", text)
	}
	if _, err := pm.GetMemory(p.ID, "agent-a", created.ID); err != nil {
		t.Fatalf("agent-a memory must survive title delete by b: %v", err)
	}
	bItems, _ := pm.ListMemories(p.ID, "agent-b")
	if len(bItems) != 0 {
		t.Fatalf("agent-b title delete should clear own row, left=%v", bItems)
	}

	st, resp = h.ServeRPC(p.ID, tokA, toolCallBody(5, "list_memories", nil))
	text, isErr = parseToolText(t, resp)
	if isErr || !strings.Contains(text, created.ID) {
		t.Fatalf("list: %s", text)
	}
	st, resp = h.ServeRPC(p.ID, tokA, toolCallBody(6, "get_memory", map[string]any{"id": created.ID}))
	text, isErr = parseToolText(t, resp)
	if isErr || !strings.Contains(text, "secret-a") {
		t.Fatalf("get: %s", text)
	}
	st, resp = h.ServeRPC(p.ID, tokA, toolCallBody(7, "search_memories", map[string]any{"query": "secret"}))
	text, isErr = parseToolText(t, resp)
	if isErr || !strings.Contains(text, created.ID) {
		t.Fatalf("search: %s", text)
	}
	st, resp = h.ServeRPC(p.ID, tokA, toolCallBody(8, "delete_memory", map[string]any{"id": created.ID}))
	text, isErr = parseToolText(t, resp)
	if isErr {
		t.Fatalf("delete: %s", text)
	}
	left, _ := pm.ListMemories(p.ID, "agent-a")
	if len(left) != 0 {
		t.Fatalf("left=%v", left)
	}
	_ = st
}
