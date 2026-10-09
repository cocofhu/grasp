package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/runtime"
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

func TestSharedAgentCredentialIDsRoundTripAndClear(t *testing.T) {
	svc := NewSharedAgentService(t.TempDir())
	cfg := SharedAgentConfig{
		ProjectID:            "proj-1",
		AcpBackend:           AcpBackendCursor,
		AiCredentialID:       "cred-cursor",
		OpenCodeCredentialID: "cred-oc",
		Env:                  map[string]string{"GITLAB_URL": "https://gl"},
	}
	if err := svc.Save(cfg); err != nil {
		t.Fatal(err)
	}
	got := svc.Get("proj-1")
	if got.AiCredentialID != "cred-cursor" || got.OpenCodeCredentialID != "cred-oc" {
		t.Fatalf("ids = %q %q", got.AiCredentialID, got.OpenCodeCredentialID)
	}
	raw, err := os.ReadFile(filepath.Join(svc.Root(), sanitizeProjectID("proj-1"), "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-") || strings.Contains(string(raw), "secret") {
		t.Fatalf("shared config stored a secret: %s", raw)
	}

	svc.ClearCredentialSelection("proj-1", "cred-cursor")
	got = svc.Get("proj-1")
	if got.AiCredentialID != "" {
		t.Fatalf("ai id still set: %q", got.AiCredentialID)
	}
	if got.OpenCodeCredentialID != "cred-oc" {
		t.Fatalf("other slot changed: %q", got.OpenCodeCredentialID)
	}
	svc.ClearCredentialSelection("proj-1", "cred-oc")
	got = svc.Get("proj-1")
	if got.OpenCodeCredentialID != "" {
		t.Fatalf("opencode id still set: %q", got.OpenCodeCredentialID)
	}
	if got.Env["GITLAB_URL"] != "https://gl" {
		t.Fatalf("clear rewrote env: %#v", got.Env)
	}
}

func TestExtendOverlayCredentialPriority(t *testing.T) {
	kind := func(id string) (string, bool) {
		switch id {
		case "shared-cursor":
			return envauth.EnvCursorAPIKey, true
		case "shared-claude":
			return envauth.EnvClaudeAPIKey, true
		case "shared-oc":
			return envauth.EnvOpenCodeAPIKey, true
		default:
			return "", false
		}
	}
	agentWins := ExtendOverlayWithKind(SharedAgentConfig{
		AcpBackend:     AcpBackendCursor,
		AiCredentialID: "shared-cursor",
	}, Agent{
		Name:           "demo",
		AcpBackend:     AcpBackendCursor,
		AiCredentialID: "agent-cursor",
	}, kind)
	if agentWins.AiCredentialID != "agent-cursor" {
		t.Fatalf("agent id lost: %q", agentWins.AiCredentialID)
	}

	sharedUsed := ExtendOverlayWithKind(SharedAgentConfig{
		AcpBackend:     AcpBackendCursor,
		AiCredentialID: "shared-cursor",
	}, Agent{Name: "demo", ProjectID: "p"}, kind)
	if sharedUsed.AiCredentialID != "shared-cursor" {
		t.Fatalf("shared fallback = %q", sharedUsed.AiCredentialID)
	}
	if sharedUsed.AcpBackend != AcpBackendCursor {
		t.Fatalf("backend = %q", sharedUsed.AcpBackend)
	}

	mismatch := ExtendOverlayWithKind(SharedAgentConfig{
		AcpBackend:     AcpBackendClaudeCode,
		AiCredentialID: "shared-cursor",
	}, Agent{Name: "demo"}, kind)
	if mismatch.AiCredentialID != "" {
		t.Fatalf("mismatched shared id leaked: %q", mismatch.AiCredentialID)
	}

	bothEmpty := ExtendOverlayWithKind(SharedAgentConfig{AcpBackend: AcpBackendCursor}, Agent{Name: "demo", AcpBackend: AcpBackendCursor}, kind)
	if bothEmpty.AiCredentialID != "" || bothEmpty.OpenCodeCredentialID != "" {
		t.Fatalf("empty slots = %q %q", bothEmpty.AiCredentialID, bothEmpty.OpenCodeCredentialID)
	}

	oc := ExtendOverlayWithKind(SharedAgentConfig{
		AcpBackend:           AcpBackendOpenCode,
		AiCredentialID:       "shared-cursor",
		OpenCodeCredentialID: "shared-oc",
	}, Agent{Name: "demo"}, kind)
	if oc.OpenCodeCredentialID != "shared-oc" || oc.AiCredentialID != "" {
		t.Fatalf("opencode slots ai=%q oc=%q", oc.AiCredentialID, oc.OpenCodeCredentialID)
	}

	kept := ExtendOverlay(SharedAgentConfig{AiCredentialID: "shared-cursor"}, Agent{
		Name:           "demo",
		AcpBackend:     AcpBackendCursor,
		AiCredentialID: "agent-cursor",
	})
	if kept.AiCredentialID != "agent-cursor" {
		t.Fatalf("overlay dropped agent id: %q", kept.AiCredentialID)
	}

	ai, ocID := runtime.MergeCodingCredentialIDs(sharedUsed.AcpBackend, "", "", "shared-cursor", "", kind)
	if ai != sharedUsed.AiCredentialID || ocID != sharedUsed.OpenCodeCredentialID {
		t.Fatalf("chat merge %q %q != runtime %q %q", sharedUsed.AiCredentialID, sharedUsed.OpenCodeCredentialID, ai, ocID)
	}
}
