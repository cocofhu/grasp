package services

import (
	"path/filepath"
	"testing"
)

func TestExtendOverlay_AgentWinsSameKeys(t *testing.T) {
	shared := SharedAgentConfig{
		ProjectID:  "proj-a",
		AcpBackend: AcpBackendClaudeCode,
		Env: map[string]string{
			"SHARED": "from-shared",
			"BOTH":   "shared-val",
		},
		Files: []AgentFile{
			{Path: "rules/base.md", Content: "shared-base"},
			{Path: "rules/shared.md", Content: "only-shared"},
		},
		MCP: []MCPServer{
			{Name: "shared-mcp", URL: "http://shared"},
			{Name: "both", URL: "http://shared-both"},
		},
		Layout: AgentLayout{ConfigRoot: "/root/.claude", WorkspaceDir: "/root/ws-shared"},
	}
	agent := Agent{
		Name:       "demo",
		ProjectID:  "proj-agent",
		AcpBackend: AcpBackendCursor,
		Env: map[string]string{
			"BOTH":       "agent-val",
			"AGENT_ONLY": "a1",
		},
		Files: []AgentFile{
			{Path: "rules/base.md", Content: "agent-base"},
			{Path: "rules/agent.md", Content: "only-agent"},
		},
		MCP: []MCPServer{
			{Name: "both", URL: "http://agent-both"},
			{Name: "agent-mcp", URL: "http://agent"},
		},
		Layout: AgentLayout{ConfigRoot: "/root/.cursor"},
	}
	got := ExtendOverlay(shared, agent)
	if got.ProjectID != "proj-agent" {
		t.Fatalf("projectId = %q", got.ProjectID)
	}
	if got.AcpBackend != AcpBackendCursor {
		t.Fatalf("acpBackend = %q", got.AcpBackend)
	}
	if got.Env["SHARED"] != "from-shared" || got.Env["BOTH"] != "agent-val" || got.Env["AGENT_ONLY"] != "a1" {
		t.Fatalf("env = %#v", got.Env)
	}
	byPath := map[string]string{}
	for _, f := range got.Files {
		byPath[f.Path] = f.Content
	}
	if byPath["rules/base.md"] != "agent-base" || byPath["rules/shared.md"] != "only-shared" || byPath["rules/agent.md"] != "only-agent" {
		t.Fatalf("files = %#v", byPath)
	}
	byMCP := map[string]string{}
	for _, m := range got.MCP {
		byMCP[m.Name] = m.URL
	}
	if byMCP["both"] != "http://agent-both" || byMCP["shared-mcp"] != "http://shared" {
		t.Fatalf("mcp = %#v", byMCP)
	}
	if got.Layout.ConfigRoot != "/root/.cursor" {
		t.Fatalf("configRoot = %q", got.Layout.ConfigRoot)
	}
	if got.Layout.WorkspaceDir != "/root/ws-shared" {
		t.Fatalf("workspaceDir = %q (want shared fill)", got.Layout.WorkspaceDir)
	}
}

func TestExtendOverlay_AgentEnvWins(t *testing.T) {
	shared := SharedAgentConfig{
		Env: map[string]string{"FEATURE_FLAG": "shared-flag", "SHARED_ONLY": "1"},
	}
	agent := Agent{Name: "demo", Env: map[string]string{"FEATURE_FLAG": "agent-flag"}}
	got := ExtendOverlay(shared, agent)
	if got.Env["FEATURE_FLAG"] != "agent-flag" || got.Env["SHARED_ONLY"] != "1" {
		t.Fatalf("env = %#v", got.Env)
	}
}

func TestExtendOverlay_KeepsAgentProjectID(t *testing.T) {
	got := ExtendOverlay(SharedAgentConfig{ProjectID: "proj-a"}, Agent{Name: "x", ProjectID: "proj-agent"})
	if got.ProjectID != "proj-agent" {
		t.Fatalf("keep agent projectId = %q", got.ProjectID)
	}
}

func TestSharedAgentService_SaveGetRoundTrip(t *testing.T) {
	root := t.TempDir()
	svc := NewSharedAgentService(root)
	cfg := SharedAgentConfig{
		ProjectID:  "proj-1",
		AcpBackend: AcpBackendCursor,
		Env:        map[string]string{"K1": "v1"},
		Files:      []AgentFile{{Path: "AGENTS.md", Content: "hello"}},
		MCP:        []MCPServer{{Name: "artifact-store", URL: "${GRASP_ARTIFACT_URL}"}},
	}
	if err := svc.Save(cfg); err != nil {
		t.Fatal(err)
	}
	got := svc.Get("proj-1")
	if got.Env["K1"] != "v1" {
		t.Fatalf("env = %#v", got.Env)
	}
	if len(got.Files) != 1 || got.Files[0].Content != "hello" {
		t.Fatalf("files = %#v", got.Files)
	}
	wd := svc.WorkDir("proj-1")
	if wd == "" || filepath.Base(wd) != WorkDirName {
		t.Fatalf("WorkDir = %q", wd)
	}
	empty := svc.Get("missing")
	if empty.Env == nil || len(empty.Files) != 0 {
		t.Fatalf("missing should be empty valid: %#v", empty)
	}
}
