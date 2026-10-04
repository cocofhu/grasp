package services

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
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

func TestExtendOverlay_TokenSharedPriority(t *testing.T) {
	shared := SharedAgentConfig{
		Env: map[string]string{
			"GRASP_CURSOR_API_KEY": "shared-key",
			"FEATURE_FLAG":         "shared-flag",
		},
	}
	agent := Agent{
		Name: "demo",
		Env: map[string]string{
			"GRASP_CURSOR_API_KEY": "agent-key",
			"GITLAB_TOKEN":         "agent-gl",
			"FEATURE_FLAG":         "agent-flag",
		},
	}
	got := ExtendOverlay(shared, agent)
	if got.Env["GRASP_CURSOR_API_KEY"] != "shared-key" {
		t.Fatalf("token both present → shared wins: %#v", got.Env)
	}
	if got.Env["GITLAB_TOKEN"] != "agent-gl" {
		t.Fatalf("token only on agent → keep stock: %#v", got.Env)
	}
	if got.Env["FEATURE_FLAG"] != "agent-flag" {
		t.Fatalf("non-token still agent wins: %#v", got.Env)
	}
}

func TestExtendOverlay_ProjectIDFillEmptyOnly(t *testing.T) {
	shared := SharedAgentConfig{ProjectID: "proj-a", DefaultProjectID: "proj-default"}
	got := ExtendOverlay(shared, Agent{Name: "x"})
	if got.ProjectID != "proj-default" {
		t.Fatalf("fill empty projectId = %q", got.ProjectID)
	}
	got2 := ExtendOverlay(shared, Agent{Name: "x", ProjectID: "proj-agent"})
	if got2.ProjectID != "proj-agent" {
		t.Fatalf("keep agent projectId = %q", got2.ProjectID)
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

func TestMigrateProjectSandboxEnv_SameKeyKeepsShared(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "migrate.db"))
	if err != nil {
		t.Fatal(err)
	}
	projects := NewProjectService(db)
	shared := NewSharedAgentService(t.TempDir())

	p, err := projects.Create("Demo", "", []models.EnvEntry{
		{Key: "BOTH", Value: "from-project"},
		{Key: "PROJ_ONLY", Value: "p1"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := shared.Save(SharedAgentConfig{
		ProjectID: p.ID,
		Env:       map[string]string{"BOTH": "from-shared", "SHARED_ONLY": "s1"},
	}); err != nil {
		t.Fatal(err)
	}
	MigrateProjectSandboxEnvOnce(db, projects, shared)
	got := shared.Get(p.ID)
	if got.Env["BOTH"] != "from-shared" {
		t.Fatalf("BOTH should keep shared: %#v", got.Env)
	}
	if got.Env["PROJ_ONLY"] != "p1" || got.Env["SHARED_ONLY"] != "s1" {
		t.Fatalf("env after migrate: %#v", got.Env)
	}
	p2, ok := projects.Get(p.ID)
	if !ok || len(p2.SandboxEnv) != 0 {
		t.Fatalf("project SandboxEnv should be cleared: %#v", p2.SandboxEnv)
	}
	MigrateProjectSandboxEnvOnce(db, projects, shared)
	got2 := shared.Get(p.ID)
	if got2.Env["BOTH"] != "from-shared" || got2.Env["PROJ_ONLY"] != "p1" {
		t.Fatalf("idempotent env: %#v", got2.Env)
	}
}
