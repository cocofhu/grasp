package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/services"
)

func setupExternalMcpHarness(t *testing.T) (*harness, string) {
	t.Helper()
	hn := newHarness(t)
	enableAdmin(t)
	hn.cookie = hn.login(t)
	cfg := config.GetConfig()
	cfg.Server.MCPAdvertise = "http://api.example.com"
	config.StoreConfig(cfg)
	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, hn.h.WF, hn.h.Runs, hn.h.Arts, nil)
	hn.h.PMMCP.SetAuditRecorder(hn.h.Audit.Record)
	hn.h.ExternalMcp = services.NewProjectExternalMcpService(hn.db, "")
	hn.h.ProjectMcpKeys = services.NewProjectMcpApiKeyService(hn.db)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "ExtMCP"})
	if w.Code != http.StatusOK {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	return hn, proj["id"].(string)
}

func postExternalMcpRPC(hn *harness, pid, mcpID, bearer string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp/external/"+pid+"/"+mcpID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	return w
}

// plan_coverage leaves: g1.3 REST CRUD, g2.1 route+auth, g4.1 revoke immediate 401.
func TestExternalMcpSettingsAndKeysREST(t *testing.T) {
	hn, pid := setupExternalMcpHarness(t)

	w := hn.do(http.MethodGet, "/api/projects/"+pid+"/external-mcp", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get settings: %d %s", w.Code, w.Body.String())
	}
	var settings map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &settings)
	if settings["enabled"] != false {
		t.Fatalf("default enabled = %v", settings["enabled"])
	}

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/external-mcp", map[string]any{
		"enabled": true, "enabledPacks": []string{"pm-progress"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update settings: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/external-mcp/keys", map[string]any{"name": "cursor"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	plain, _ := created["key"].(string)
	keyID, _ := created["id"].(string)
	if plain == "" || keyID == "" {
		t.Fatalf("create response = %v", created)
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/external-mcp/keys", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", w.Code, w.Body.String())
	}

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	})
	resp := postExternalMcpRPC(hn, pid, "pm-progress", plain, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("tools/list: %d %s", resp.Code, resp.Body.String())
	}

	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/external-mcp/keys/"+keyID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	resp = postExternalMcpRPC(hn, pid, "pm-progress", plain, body)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key want 401 got %d", resp.Code)
	}
}

// plan_coverage leaves: g2.1 disabled 403, g2.2 unenabled pack 404.
func TestExternalMcpDisabledAndPackFilter(t *testing.T) {
	hn, pid := setupExternalMcpHarness(t)

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	w := hn.do(http.MethodPut, "/api/projects/"+pid+"/external-mcp", map[string]any{
		"enabled": false, "enabledPacks": []string{"pm-progress"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update disabled: %d", w.Code)
	}
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/external-mcp/keys", map[string]any{"name": "k"})
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	plain := created["key"].(string)

	resp := postExternalMcpRPC(hn, pid, "pm-progress", plain, body)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("disabled want 403 got %d", resp.Code)
	}

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/external-mcp", map[string]any{
		"enabled": true, "enabledPacks": []string{"pm-progress"},
	})
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	resp = postExternalMcpRPC(hn, pid, "pm-workflow-write", plain, body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("unenabled pack call status = %d %s", resp.Code, resp.Body.String())
	}
	var rpc map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &rpc)
	if rpc["error"] == nil {
		t.Fatalf("expected disabled pack error, got %v", rpc)
	}
}

// plan_coverage: g4.1 — project A key must not authorize project B external MCP.
func TestExternalMcpCrossProjectKeyRejected(t *testing.T) {
	hn, pidA := setupExternalMcpHarness(t)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "Other"})
	if w.Code != http.StatusOK {
		t.Fatalf("create project B: %d %s", w.Code, w.Body.String())
	}
	var projB map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &projB)
	pidB := projB["id"].(string)

	for _, pid := range []string{pidA, pidB} {
		w = hn.do(http.MethodPut, "/api/projects/"+pid+"/external-mcp", map[string]any{
			"enabled": true, "enabledPacks": []string{"pm-progress"},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("enable %s: %d %s", pid, w.Code, w.Body.String())
		}
	}

	w = hn.do(http.MethodPost, "/api/projects/"+pidA+"/external-mcp/keys", map[string]any{"name": "a-key"})
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	plain := created["key"].(string)

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	resp := postExternalMcpRPC(hn, pidB, "pm-progress", plain, body)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("cross-project key want 401 got %d %s", resp.Code, resp.Body.String())
	}
}

// plan_coverage: g2.3 — external MCP tool calls audit with CallerKind=external.
func TestExternalMcpAuditExternalCallerKind(t *testing.T) {
	hn, pid := setupExternalMcpHarness(t)

	w := hn.do(http.MethodPut, "/api/projects/"+pid+"/external-mcp", map[string]any{
		"enabled": true, "enabledPacks": []string{"pm-progress"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/external-mcp/keys", map[string]any{"name": "audit-key"})
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	plain := created["key"].(string)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "pm_get_progress", "arguments": map[string]any{}},
	})
	resp := postExternalMcpRPC(hn, pid, "pm-progress", plain, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("tools/list: %d %s", resp.Code, resp.Body.String())
	}

	items, total, err := hn.h.Audit.ListPage(services.AuditListFilter{ProjectID: pid, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	var sawExternal bool
	for _, ev := range items {
		if ev.Action == models.AuditActionMCPCall && ev.CallerKind == models.CallerKindExternal {
			sawExternal = true
			if ev.Actor != "external-mcp:audit-key" {
				t.Fatalf("actor = %q, want external-mcp:audit-key", ev.Actor)
			}
			if !strings.Contains(ev.Summary, "external mcp") {
				t.Fatalf("summary = %q", ev.Summary)
			}
		}
	}
	if total == 0 || !sawExternal {
		t.Fatalf("expected external mcp audit event, total=%d saw=%v", total, sawExternal)
	}
}

func wireExternalAgentFS(t *testing.T, hn *harness) *services.OrgService {
	t.Helper()
	root := t.TempDir()
	skill := services.NewAgentService(root)
	hn.h.Agents = skill
	pm := services.NewPmService(hn.db, skill)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, hn.h.WF, hn.h.Runs, hn.h.Arts, nil)
	hn.h.PMMCP.SetAuditRecorder(hn.h.Audit.Record)
	org := services.NewOrgService(root, skill)
	hn.h.Org = org
	team := services.NewTeamService(hn.h.Projects, skill, org, pm, nil)
	hn.h.PMMCP.SetOrgAndAgent(org, skill)
	hn.h.PMMCP.SetTeam(team)
	return org
}

func externalToolCallResult(t *testing.T, body []byte) (result map[string]any, isError bool, raw string) {
	t.Helper()
	raw = string(body)
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		t.Fatalf("unmarshal rpc: %v body=%s", err, raw)
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
	return result, isError, raw
}

// plan_coverage: g2.2 / g4.1 — RestoreExternal injects PmLeaderAgent so pm-agent-fs works;
// unbound leader returns distinguishable "pm leader not bound".
func TestExternalMcpAgentFSUsesPmLeader(t *testing.T) {
	hn, pid := setupExternalMcpHarness(t)
	org := wireExternalAgentFS(t, hn)

	if err := hn.h.Agents.Save(services.Agent{Name: "pm-leader", ProjectID: pid}); err != nil {
		t.Fatal(err)
	}
	if err := hn.h.Agents.Save(services.Agent{Name: "member-a", ProjectID: pid}); err != nil {
		t.Fatal(err)
	}
	if _, err := org.Put(services.AgentOrg{
		Groups: []services.OrgGroup{{ID: "g-pipe", Name: "Pipeline"}},
		Agents: map[string]services.OrgAgentMembership{
			"pm-leader": {},
			"member-a":  {},
		},
	}, 0); err != nil {
		t.Fatal(err)
	}
	en := true
	leader := "pm-leader"
	if _, err := hn.h.Pm.UpdateBinding(pid, &en, &leader, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	w := hn.do(http.MethodPut, "/api/projects/"+pid+"/external-mcp", map[string]any{
		"enabled": true, "enabledPacks": []string{"pm-agent-fs"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/external-mcp/keys", map[string]any{"name": "fs-key"})
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	plain := created["key"].(string)

	listBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "pm_fs_list",
			"arguments": map[string]any{"agentName": "member-a", "path": "."},
		},
	})
	resp := postExternalMcpRPC(hn, pid, "pm-agent-fs", plain, listBody)
	if resp.Code != http.StatusOK {
		t.Fatalf("pm_fs_list status=%d %s", resp.Code, resp.Body.String())
	}
	result, isErr, raw := externalToolCallResult(t, resp.Body.Bytes())
	if isErr {
		t.Fatalf("pm_fs_list isError raw=%s", raw)
	}
	if result["agentName"] != "member-a" {
		t.Fatalf("pm_fs_list result=%v", result)
	}

	memBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name": "pm_set_org_membership",
			"arguments": map[string]any{
				"agentName": "member-a",
				"groupIds":  []string{"g-pipe"},
			},
		},
	})
	resp = postExternalMcpRPC(hn, pid, "pm-agent-fs", plain, memBody)
	if resp.Code != http.StatusOK {
		t.Fatalf("pm_set_org_membership status=%d %s", resp.Code, resp.Body.String())
	}
	result, isErr, raw = externalToolCallResult(t, resp.Body.Bytes())
	if isErr {
		t.Fatalf("pm_set_org_membership isError raw=%s", raw)
	}
	if result["ok"] != true {
		t.Fatalf("membership result=%v", result)
	}
}

func TestExternalMcpAgentFSRequiresPmLeader(t *testing.T) {
	hn, pid := setupExternalMcpHarness(t)
	_ = wireExternalAgentFS(t, hn)

	if err := hn.h.Agents.Save(services.Agent{Name: "orphan", ProjectID: pid}); err != nil {
		t.Fatal(err)
	}

	w := hn.do(http.MethodPut, "/api/projects/"+pid+"/external-mcp", map[string]any{
		"enabled": true, "enabledPacks": []string{"pm-agent-fs"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/external-mcp/keys", map[string]any{"name": "no-leader"})
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	plain := created["key"].(string)

	listBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "pm_fs_list",
			"arguments": map[string]any{"agentName": "orphan", "path": "."},
		},
	})
	resp := postExternalMcpRPC(hn, pid, "pm-agent-fs", plain, listBody)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d %s", resp.Code, resp.Body.String())
	}
	result, isErr, raw := externalToolCallResult(t, resp.Body.Bytes())
	if !isErr {
		t.Fatalf("want isError, got result=%v raw=%s", result, raw)
	}
	errMsg, _ := result["error"].(string)
	if !strings.Contains(errMsg, "pm leader not bound") {
		t.Fatalf("want pm leader not bound, got %q raw=%s", errMsg, raw)
	}

	teamBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name": "pm_set_org_membership",
			"arguments": map[string]any{
				"agentName": "orphan",
				"groupIds":  []string{"g1"},
			},
		},
	})
	resp = postExternalMcpRPC(hn, pid, "pm-agent-fs", plain, teamBody)
	result, isErr, raw = externalToolCallResult(t, resp.Body.Bytes())
	if !isErr {
		t.Fatalf("team tool want isError, got %v raw=%s", result, raw)
	}
	errMsg, _ = result["error"].(string)
	if !strings.Contains(errMsg, "pm leader not bound") {
		t.Fatalf("team tool want pm leader not bound, got %q raw=%s", errMsg, raw)
	}
}
