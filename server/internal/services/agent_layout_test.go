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
		AcpBackend: AcpBackendCursor,
		Name:       "x-agent",
		Files:      []AgentFile{{Path: "rules/x.md", Content: "# x"}},
		Layout:     AgentLayout{WorkspaceDir: "/srv/code"},
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
}

// TestAgentLayoutDefaultsWhenAbsent verifies an agent.json without a layout
// block reads back as both protocol defaults, as optional layout defaults.
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
