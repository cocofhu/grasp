package pmmcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAgentFSHost(t *testing.T) (projectID, token string, h *Host, skill *services.AgentService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:pmmcp_agent_fs_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	p, err := ps.Create("AgentFSProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	skill = services.NewAgentService(root)
	for _, name := range []string{"leader", "alice", "bob", "outsider"} {
		if err := skill.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: name, ProjectID: p.ID}); err != nil {
			t.Fatal(err)
		}
	}
	// Cross-project agent
	if err := skill.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "otherproj", ProjectID: "other-project"}); err != nil {
		t.Fatal(err)
	}
	pm := services.NewPmService(db, skill)
	en := true
	agent := "leader"
	if _, err := pm.UpdateBinding(p.ID, &en, &agent, []string{"pm-agent-fs"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	h = NewHost(pm, services.NewPmProgress(pm, nil, nil), nil, nil, services.NewArtifactService(db), nil)
	h.SetAgents(skill)
	tok := platformmcp.NewToken()
	h.Restore(p.ID, "thr-fs", "alice-user", "leader", tok)
	return p.ID, tok, h, skill
}

func callAgentFSTool(t *testing.T, h *Host, projectID, token, tool string, args map[string]any) (status int, result map[string]any, isError bool, raw string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	st, resp := h.ServeRPC(projectID, MCPAgentFS, token, body)
	raw = string(resp)
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &rpc); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, resp)
	}
	isError = rpc.Result.IsError
	text := ""
	if len(rpc.Result.Content) > 0 {
		text = rpc.Result.Content[0].Text
	}
	_ = json.Unmarshal([]byte(text), &result)
	if result == nil {
		result = map[string]any{"_raw": text}
	}
	return st, result, isError, raw
}

func TestPmListProjectAgentsRelations(t *testing.T) {
	pid, tok, h, _ := setupAgentFSHost(t)
	st, result, isErr, raw := callAgentFSTool(t, h, pid, tok, "pm_list_project_agents", map[string]any{})
	if st != 200 || isErr {
		t.Fatalf("status=%d isErr=%v raw=%s", st, isErr, raw)
	}
	if result["self"] != "leader" || result["projectId"] != pid {
		t.Fatalf("self/projectId wrong raw=%s", raw)
	}
	agents, _ := result["agents"].([]any)
	rel := map[string]string{}
	for _, a := range agents {
		m, _ := a.(map[string]any)
		rel[m["name"].(string)] = m["relation"].(string)
	}
	if rel["leader"] != "self" || rel["alice"] != "other" || rel["bob"] != "other" || rel["outsider"] != "other" {
		t.Fatalf("relations=%v", rel)
	}
	if _, leaked := rel["otherproj"]; leaked || len(rel) != 4 {
		t.Fatalf("cross-project agent leaked: %v", rel)
	}
}

func TestPmFSDirectIndirectSelfWrite(t *testing.T) {
	pid, tok, h, skill := setupAgentFSHost(t)
	for _, agent := range []string{"leader", "alice", "bob"} {
		st, result, isErr, raw := callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
			"agentName": agent,
			"path":      "AGENTS.md",
			"content":   "edited-by-leader:" + agent,
			"reason":    "test write",
		})
		if st != 200 || isErr {
			t.Fatalf("%s write failed: %s result=%v", agent, raw, result)
		}
		got, err := skill.ReadWorkspaceFile(agent, "AGENTS.md")
		if err != nil || got != "edited-by-leader:"+agent {
			t.Fatalf("%s disk=%q err=%v", agent, got, err)
		}
		// agent.json must stay untouched (MCP/meta preserved)
		cfg := skill.Get // compile check
		_ = cfg
		ag, ok := skill.Get(agent)
		if !ok || ag.ProjectID != pid {
			t.Fatalf("%s project lost", agent)
		}
		b, _ := os.ReadFile(filepath.Join(skill.WorkDir(agent), "..", "agent.json"))
		if !strings.Contains(string(b), `"projectId"`) {
			t.Fatalf("agent.json missing projectId for %s: %s", agent, b)
		}
	}
}

func TestPmFSRejectNonReportAndCrossProject(t *testing.T) {
	pid, tok, h, _ := setupAgentFSHost(t)
	_, result, isErr, _ := callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
		"agentName": "outsider",
		"path":      "AGENTS.md",
		"content":   "nope",
		"reason":    "test",
	})
	if isErr {
		t.Fatalf("same-project outsider should be allowed: %v", result)
	}
	_, result, isErr, _ = callAgentFSTool(t, h, pid, tok, "pm_fs_read", map[string]any{
		"agentName": "otherproj",
		"path":      "AGENTS.md",
	})
	if !isErr || !strings.Contains(result["error"].(string), "not in project") {
		t.Fatalf("cross-project: %v", result)
	}
}

func TestPmFSPathEscapeRejected(t *testing.T) {
	pid, tok, h, _ := setupAgentFSHost(t)
	_, result, isErr, _ := callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
		"agentName": "alice",
		"path":      "../escape.md",
		"content":   "x",
		"reason":    "test",
	})
	if !isErr || !strings.Contains(result["error"].(string), "invalid workspace path") {
		t.Fatalf("escape: %v", result)
	}
}

func TestPmFSDisabledMCP(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:pmmcp_agent_fs_off?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(models.AllModels()...)
	ps := services.NewProjectService(db)
	p, _ := ps.Create("Off", "", nil)
	pm := services.NewPmService(db, nil)
	en := true
	agent := "leader"
	if _, err := pm.UpdateBinding(p.ID, &en, &agent, []string{"pm-progress"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	h := NewHost(pm, services.NewPmProgress(pm, nil, nil), nil, nil, services.NewArtifactService(db), nil)
	tok := platformmcp.NewToken()
	h.Restore(p.ID, "t", "u", agent, tok)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "pm_list_project_agents", "arguments": map[string]any{}},
	})
	st, resp := h.ServeRPC(p.ID, MCPAgentFS, tok, body)
	if st != 404 || !strings.Contains(string(resp), "mcp disabled") {
		t.Fatalf("want disabled: %d %s", st, resp)
	}
}

func TestPmFSTooLargeRejected(t *testing.T) {
	pid, tok, h, _ := setupAgentFSHost(t)
	big := strings.Repeat("x", services.WorkspaceFileMaxBytes+1)
	_, result, isErr, _ := callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
		"agentName": "alice",
		"path":      "big.md",
		"content":   big,
		"reason":    "test",
	})
	if !isErr || !strings.Contains(result["error"].(string), "1MiB") {
		t.Fatalf("size: %v", result)
	}
}

func TestPmFSListDeleteMkdirRename(t *testing.T) {
	pid, tok, h, skill := setupAgentFSHost(t)
	callAgentFSTool(t, h, pid, tok, "pm_fs_mkdir", map[string]any{"agentName": "bob", "path": "rules", "reason": "mkdir test"})
	_, _, isErr, raw := callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
		"agentName": "bob", "path": "rules/a.md", "content": "rule-a", "reason": "write rule",
	})
	if isErr {
		t.Fatal(raw)
	}
	_, result, isErr, raw := callAgentFSTool(t, h, pid, tok, "pm_fs_list", map[string]any{
		"agentName": "bob", "path": "rules",
	})
	if isErr {
		t.Fatal(raw)
	}
	entries, _ := result["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries=%v", result)
	}
	_, _, isErr, raw = callAgentFSTool(t, h, pid, tok, "pm_fs_rename", map[string]any{
		"agentName": "bob", "path": "rules/a.md", "toPath": "rules/b.md", "reason": "rename",
	})
	if isErr {
		t.Fatal(raw)
	}
	got, err := skill.ReadWorkspaceFile("bob", "rules/b.md")
	if err != nil || got != "rule-a" {
		t.Fatalf("rename disk=%q err=%v", got, err)
	}
	_, _, isErr, raw = callAgentFSTool(t, h, pid, tok, "pm_fs_delete", map[string]any{
		"agentName": "bob", "path": "rules", "reason": "cleanup",
	})
	if isErr {
		t.Fatal(raw)
	}
	if _, err := skill.ReadWorkspaceFile("bob", "rules/b.md"); err == nil {
		t.Fatal("expected deleted")
	}
}

func TestPmFSReasonRequired(t *testing.T) {
	pid, tok, h, skill := setupAgentFSHost(t)
	_, result, isErr, _ := callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
		"agentName": "alice",
		"path":      "AGENTS.md",
		"content":   "x",
	})
	if !isErr || !strings.Contains(result["error"].(string), "reason") {
		t.Fatalf("want reason error: %v", result)
	}
	if _, err := skill.ReadWorkspaceFile("alice", "AGENTS.md"); err == nil {
		t.Fatal("write without reason must not persist")
	}
}

func TestPmFSHistoryDiffRestore(t *testing.T) {
	pid, tok, h, skill := setupAgentFSHost(t)
	_, result, isErr, _ := callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
		"agentName": "alice",
		"path":      "AGENTS.md",
		"content":   "v1",
		"reason":    "seed",
	})
	if isErr {
		t.Fatalf("write: %v", result)
	}
	sha, _ := result["sha"].(string)
	if sha == "" {
		t.Fatal("missing sha")
	}
	_, result, isErr, _ = callAgentFSTool(t, h, pid, tok, "pm_fs_history", map[string]any{"agentName": "alice"})
	if isErr || result["revisions"] == nil {
		t.Fatalf("history: %v", result)
	}
	_, result, isErr, _ = callAgentFSTool(t, h, pid, tok, "pm_fs_diff", map[string]any{"agentName": "alice", "sha": sha})
	if isErr {
		t.Fatalf("diff: %v", result)
	}
	_, _, isErr, _ = callAgentFSTool(t, h, pid, tok, "pm_fs_write", map[string]any{
		"agentName": "alice", "path": "AGENTS.md", "content": "v2", "reason": "update",
	})
	_, result, isErr, _ = callAgentFSTool(t, h, pid, tok, "pm_fs_restore", map[string]any{
		"agentName": "alice", "sha": sha, "reason": "rollback test",
	})
	if isErr {
		t.Fatalf("restore: %v", result)
	}
	got, _ := skill.ReadWorkspaceFile("alice", "AGENTS.md")
	if got != "v1" {
		t.Fatalf("restored=%q", got)
	}
}

func TestPmAgentFSToolsList(t *testing.T) {
	pid, tok, h, _ := setupAgentFSHost(t)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	st, resp := h.ServeRPC(pid, MCPAgentFS, tok, body)
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	var listResp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal(resp, &listResp)
	names := map[string]bool{}
	for _, tool := range listResp.Result.Tools {
		names[tool["name"].(string)] = true
	}
	for _, want := range []string{
		"pm_list_project_agents", "pm_fs_list", "pm_fs_read", "pm_fs_write", "pm_fs_delete", "pm_fs_mkdir", "pm_fs_rename",
		"pm_fs_history", "pm_fs_diff", "pm_fs_restore",
	} {
		if !names[want] {
			t.Fatalf("missing tool %s in %v", want, names)
		}
	}
}
