package services

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
)

func TestExtendOverlay_AgentWinsSameKeys(t *testing.T) {
	shared := SharedAgentConfig{
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

func TestSharedAgentService_SaveGetRoundTrip(t *testing.T) {
	root := t.TempDir()
	svc := NewSharedAgentService(root)
	cfg := SharedAgentConfig{
		Env:   map[string]string{"K1": "v1"},
		Files: []AgentFile{{Path: "AGENTS.md", Content: "hello"}},
		MCP:   []MCPServer{{Name: "artifact-store", URL: "${GRASP_ARTIFACT_URL}"}},
	}
	if err := svc.Save(cfg); err != nil {
		t.Fatal(err)
	}
	got := svc.Get()
	if got.Env["K1"] != "v1" {
		t.Fatalf("env = %#v", got.Env)
	}
	if len(got.Files) != 1 || got.Files[0].Content != "hello" {
		t.Fatalf("files = %#v", got.Files)
	}
	wd := svc.WorkDir()
	if wd == "" || filepath.Base(wd) != WorkDirName {
		t.Fatalf("WorkDir = %q", wd)
	}
	empty := NewSharedAgentService(t.TempDir()).Get()
	if empty.Env == nil || len(empty.Files) != 0 {
		t.Fatalf("missing should be empty valid: %#v", empty)
	}
}

func TestExtendOverlayNeverInheritsModelOrAuthorizations(t *testing.T) {
	shared := SharedAgentConfig{Env: map[string]string{"ACP_BRIDGE_MODEL": "shared", "OPENAI_BASE_URL": "https://shared.example", "KEEP": "shared"}}
	binding := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "daily"}
	agent := Agent{Name: "configured", AcpBackend: "codex", ModelBinding: binding}
	got := ExtendOverlay(shared, agent)
	if got.ModelBinding != binding || got.AcpBackend != "codex" || got.GitBindings != nil {
		t.Fatal("shared baseline changed Agent model or Git selection")
	}
	if got.Env["ACP_BRIDGE_MODEL"] != "" || got.Env["OPENAI_BASE_URL"] != "" || got.Env["KEEP"] != "shared" {
		t.Fatalf("unexpected shared environment keys")
	}
	if shared.Env["ACP_BRIDGE_MODEL"] != "shared" {
		t.Fatal("shared config mutated")
	}
}
