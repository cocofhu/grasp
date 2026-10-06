package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestAgentLayoutPersistRoundTrip verifies a per-Agent injection layout is
// persisted to agent.json and read back, with empty fields filled by protocol
// defaults. This is the data source the executor consumes at sandbox creation.
func TestAgentLayoutPersistRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)

	// Save with a custom workspaceDir but no configRoot.
	in := Agent{
		AcpBackend:        AcpBackendCursor,
		Name:              "x-agent",
		GitCredentialType: "gitlab_https",
		Files:             []AgentFile{{Path: "rules/x.md", Content: "# x"}},
		Layout:            AgentLayout{WorkspaceDir: "/srv/code"},
	}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}

	got, ok := s.Get("x-agent")
	if !ok {
		t.Fatal("agent not found")
	}
	if got.Layout.WorkspaceDir != "/srv/code" {
		t.Fatalf("workspaceDir = %q, want /srv/code", got.Layout.WorkspaceDir)
	}
	if got.Layout.ConfigRoot != DefaultConfigRoot {
		t.Fatalf("configRoot = %q, want default %q", got.Layout.ConfigRoot, DefaultConfigRoot)
	}
	if got.GitCredentialType != "gitlab_https" {
		t.Fatalf("gitCredentialType = %q, want gitlab_https", got.GitCredentialType)
	}

	// The layout is materialized on disk (so the runtime's agent.json reader
	// sees it too), with defaults applied.
	b, err := os.ReadFile(filepath.Join(root, "x-agent", "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg agentConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Layout == nil {
		t.Fatal("agent.json missing layout block")
	}
	if cfg.Layout.WorkspaceDir != "/srv/code" || cfg.Layout.ConfigRoot != DefaultConfigRoot {
		t.Fatalf("on-disk layout = %+v", *cfg.Layout)
	}
	if cfg.GitCredentialType != "gitlab_https" {
		t.Fatalf("on-disk gitCredentialType = %q", cfg.GitCredentialType)
	}
}

func TestGitCredentialTypeInvalidClearedOnSave(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)
	if err := s.Save(Agent{
		AcpBackend:        AcpBackendCursor,
		Name:              "dirty-git",
		GitCredentialType: "gitea_https",
		Files:             []AgentFile{{Path: "rules/x.md", Content: "# x"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("dirty-git")
	if !ok {
		t.Fatal("agent not found")
	}
	if got.GitCredentialType != "" {
		t.Fatalf("gitCredentialType = %q, want empty after invalid clear", got.GitCredentialType)
	}
	b, err := os.ReadFile(filepath.Join(root, "dirty-git", "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg agentConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.GitCredentialType != "" {
		t.Fatalf("on-disk gitCredentialType = %q, want empty", cfg.GitCredentialType)
	}
}

// TestAgentLayoutDefaultsWhenAbsent verifies an agent.json without a layout
// block reads back as both protocol defaults, so legacy agents need no
// migration.
func TestAgentLayoutDefaultsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "legacy-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewAgentService(root)
	got, ok := s.Get("legacy-agent")
	if !ok {
		t.Fatal("agent not found")
	}
	if got.Layout.ConfigRoot != DefaultConfigRoot || got.Layout.WorkspaceDir != DefaultWorkspaceDir {
		t.Fatalf("layout = %+v, want defaults", got.Layout)
	}
}
