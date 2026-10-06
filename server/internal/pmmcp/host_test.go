package pmmcp

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPmMCPToolsAndAuth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:pmmcp_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	p, err := ps.Create("MCPProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pm := services.NewPmService(db, nil)
	progress := services.NewPmProgress(pm, services.NewRunService(db), services.NewArtifactService(db))
	h := NewHost(pm, progress, nil, nil, services.NewArtifactService(db), nil)
	tok := platformmcp.NewToken()
	h.Restore(p.ID, "thr-1", "alice", "agent", tok)
	if !h.Authorize(p.ID, tok) {
		t.Fatal("authorize")
	}
	if h.Authorize(p.ID, "bad") {
		t.Fatal("bad token")
	}
	if h.Authorize("other", tok) {
		t.Fatal("other project")
	}

	initBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05"},
	})
	st, resp := h.ServeRPC(p.ID, MCPProgress, tok, initBody)
	if st != 200 {
		t.Fatalf("init status=%d body=%s", st, resp)
	}

	listBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list",
	})
	st, resp = h.ServeRPC(p.ID, MCPProgress, tok, listBody)
	if st != 200 {
		t.Fatalf("list status=%d", st)
	}
	var listResp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Result.Tools) < 5 {
		t.Fatalf("tools=%d", len(listResp.Result.Tools))
	}

	callBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "pm_get_progress", "arguments": map[string]any{}},
	})
	st, resp = h.ServeRPC(p.ID, MCPProgress, tok, callBody)
	if st != 200 {
		t.Fatalf("call status=%d body=%s", st, resp)
	}
	if !h.Authorize(p.ID, tok) {
		t.Fatal("still auth")
	}
	h.Unregister(tok)
	if h.Authorize(p.ID, tok) {
		t.Fatal("unregistered")
	}
}

func TestParseGraphArgs(t *testing.T) {
	_, present, err := parseGraphArgs(map[string]any{"name": "only-meta"})
	if present || err != nil {
		t.Fatalf("meta-only: present=%v err=%v", present, err)
	}
	g, present, err := parseGraphArgs(map[string]any{
		"nodes": []any{map[string]any{"id": "in", "type": "input"}},
		"edges": []any{},
	})
	if err != nil || !present || len(g.Nodes) != 1 {
		t.Fatalf("valid graph: present=%v err=%v nodes=%d", present, err, len(g.Nodes))
	}
	_, present, err = parseGraphArgs(map[string]any{
		"nodes": "not-an-array",
	})
	if !present || err == nil {
		t.Fatalf("malformed graph should error: present=%v err=%v", present, err)
	}
}

func TestPmWorkflowWriteWhenEnabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:pmmcp_write_enabled?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	p, err := ps.Create("WriteWF", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pm := services.NewPmService(db, nil)
	en := true
	agent := "agent"
	if _, err := pm.UpdateBinding(p.ID, &en, &agent, []string{"pm-workflow-write"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	wf := services.NewWorkflowService(db)
	rs := services.NewRunService(db)
	h := NewHost(pm, services.NewPmProgress(pm, rs, nil), wf, rs, services.NewArtifactService(db), nil)

	for _, tc := range []struct {
		name   string
		userID string
	}{
		{name: "consult", userID: "alice"},
		{name: "cron", userID: "cron"},
		{name: "channel", userID: "qq:group:abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok := platformmcp.NewToken()
			h.Restore(p.ID, "thr-"+tc.name, tc.userID, agent, tok)
			body, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{
					"name":      "pm_publish_workflow",
					"arguments": map[string]any{"workflowId": "wf-missing"},
				},
			})
			_, resp := h.ServeRPC(p.ID, MCPWorkflowWrite, tok, body)
			if !contains(string(resp), "workflow not found") {
				t.Fatalf("want workflow-not-found: %s", resp)
			}
		})
	}
}

func TestPmWorkflowWriteRejectedWhenDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:pmmcp_write_disabled?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	p, err := ps.Create("NoWrite", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pm := services.NewPmService(db, nil)
	en := true
	agent := "agent"
	if _, err := pm.UpdateBinding(p.ID, &en, &agent, []string{"pm-progress"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	h := NewHost(pm, services.NewPmProgress(pm, nil, nil), services.NewWorkflowService(db), nil, services.NewArtifactService(db), nil)
	tok := platformmcp.NewToken()
	h.Restore(p.ID, "thr", "alice", agent, tok)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "pm_create_workflow",
			"arguments": map[string]any{"name": "x"},
		},
	})
	st, resp := h.ServeRPC(p.ID, MCPWorkflowWrite, tok, body)
	if st != 404 || !contains(string(resp), "mcp disabled") {
		t.Fatalf("want disabled mcp: %d %s", st, resp)
	}
}

func TestFilterSafeToolUnknown(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:pmmcp_unk?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(models.AllModels()...)
	ps := services.NewProjectService(db)
	p, _ := ps.Create("X", "", nil)
	pm := services.NewPmService(db, nil)
	h := NewHost(pm, services.NewPmProgress(pm, nil, nil), nil, nil, services.NewArtifactService(db), nil)
	tok := platformmcp.NewToken()
	h.Restore(p.ID, "t", "u", "agent", tok)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "write_artifact", "arguments": map[string]any{}},
	})
	_, resp := h.ServeRPC(p.ID, MCPProgress, tok, body)
	if !json.Valid(resp) {
		t.Fatal(string(resp))
	}
	if got := string(resp); !contains(got, "unknown tool") && !contains(got, "isError\":true") {
		t.Fatalf("expected unknown tool rejection: %s", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
