package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeAcpBackend(t *testing.T) {
	cases := map[string]string{
		"cursor":      AcpBackendCursor,
		"claude_code": AcpBackendClaudeCode,
		"codebuddy":   AcpBackendCodeBuddy,
		"trae":        AcpBackendTrae,
		"opencode":    AcpBackendOpenCode,
		"codex":       AcpBackendCodex,
		"":            AcpBackendCursor,
		"  trae  ":    AcpBackendTrae,
		"CURSOR":      AcpBackendCursor, // case-sensitive; unknown → cursor
		"bogus":       AcpBackendCursor,
	}
	for in, want := range cases {
		if got := NormalizeAcpBackend(in); got != want {
			t.Errorf("NormalizeAcpBackend(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultConfigRootForBackend(t *testing.T) {
	cases := map[string]string{
		AcpBackendCursor:     "/root/.cursor",
		AcpBackendClaudeCode: "/root/.claude",
		AcpBackendCodeBuddy:  "/root/.codebuddy",
		AcpBackendTrae:       "/root/.trae",
		AcpBackendOpenCode:   "/root/.config/opencode",
		AcpBackendCodex:      "/root/.codex",
		"unknown":            "/root/.cursor",
	}
	for backend, want := range cases {
		if got := DefaultConfigRootForBackend(backend); got != want {
			t.Errorf("DefaultConfigRootForBackend(%q) = %q, want %q", backend, got, want)
		}
	}
}

// TestSaveGetBackendConfigRoot verifies that saving an Agent without an explicit
// ConfigRoot derives the backend's protocol default, and reading it back keeps
// that root while normalizing the backend.
func TestSaveGetBackendConfigRoot(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)

	cases := []struct {
		name        string
		backend     string
		wantRoot    string
		wantBackend string
	}{
		{"cursor-agent", AcpBackendCursor, "/root/.cursor", AcpBackendCursor},
		{"claude-agent", AcpBackendClaudeCode, "/root/.claude", AcpBackendClaudeCode},
		{"buddy-agent", AcpBackendCodeBuddy, "/root/.codebuddy", AcpBackendCodeBuddy},
		{"trae-agent", AcpBackendTrae, "/root/.trae", AcpBackendTrae},
		{"opencode-agent", AcpBackendOpenCode, "/root/.config/opencode", AcpBackendOpenCode},
		{"legacy-empty", "", "/root/.cursor", AcpBackendCursor},
		{"bogus-backend", "made-up", "/root/.cursor", AcpBackendCursor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Save(Agent{
				Name:       tc.name,
				AcpBackend: tc.backend,
				Files:      []AgentFile{{Path: "rules/a.md", Content: "# a"}},
			}); err != nil {
				t.Fatal(err)
			}
			got, ok := s.Get(tc.name)
			if !ok {
				t.Fatal("agent not found after save")
			}
			if got.AcpBackend != tc.wantBackend {
				t.Errorf("AcpBackend = %q, want %q", got.AcpBackend, tc.wantBackend)
			}
			if got.Layout.ConfigRoot != tc.wantRoot {
				t.Errorf("ConfigRoot = %q, want %q", got.Layout.ConfigRoot, tc.wantRoot)
			}
			if got.Layout.WorkspaceDir != DefaultWorkspaceDir {
				t.Errorf("WorkspaceDir = %q, want %q", got.Layout.WorkspaceDir, DefaultWorkspaceDir)
			}

			// The backend + derived root are persisted on disk for the runtime reader.
			b, err := os.ReadFile(filepath.Join(root, tc.name, "agent.json"))
			if err != nil {
				t.Fatal(err)
			}
			var cfg agentConfig
			if err := json.Unmarshal(b, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.AcpBackend != tc.wantBackend {
				t.Errorf("on-disk acpBackend = %q, want %q", cfg.AcpBackend, tc.wantBackend)
			}
			if cfg.Layout == nil || cfg.Layout.ConfigRoot != tc.wantRoot {
				t.Errorf("on-disk layout = %+v, want ConfigRoot %q", cfg.Layout, tc.wantRoot)
			}
		})
	}
}

// TestSaveExplicitConfigRootWins verifies a caller-pinned ConfigRoot is
// preserved even for a non-cursor backend (no default override).
func TestSaveExplicitConfigRootWins(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)
	if err := s.Save(Agent{
		Name:       "pinned",
		AcpBackend: AcpBackendClaudeCode,
		Layout:     AgentLayout{ConfigRoot: "/custom/home"},
		Files:      []AgentFile{{Path: "rules/a.md", Content: "# a"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("pinned")
	if !ok {
		t.Fatal("agent not found")
	}
	if got.Layout.ConfigRoot != "/custom/home" {
		t.Fatalf("ConfigRoot = %q, want /custom/home", got.Layout.ConfigRoot)
	}
	if got.AcpBackend != AcpBackendClaudeCode {
		t.Fatalf("AcpBackend = %q, want claude_code", got.AcpBackend)
	}
}

// TestGetBackendDefaultRootForLegacyPinnedCursorRoot verifies that a non-cursor
// agent.json whose ConfigRoot equals the cursor default is upgraded to the
// backend's default root on read (migration-free legacy handling).
func TestGetBackendDefaultRootForLegacyPinnedCursorRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "legacy-claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"acpBackend":"claude_code","layout":{"configRoot":"/root/.cursor"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := NewAgentService(root).Get("legacy-claude")
	if !ok {
		t.Fatal("agent not found")
	}
	if got.Layout.ConfigRoot != "/root/.claude" {
		t.Fatalf("ConfigRoot = %q, want /root/.claude", got.Layout.ConfigRoot)
	}
}
