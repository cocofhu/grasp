package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupWorkspaceAgent(t *testing.T, name string) *AgentService {
	t.Helper()
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{AcpBackend: AcpBackendCursor, Name: name, ProjectID: "proj-1", MCP: []MCPServer{{Name: "keep-me", URL: "http://x"}}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func agentJSON(t *testing.T, s *AgentService, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.root, name, "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWorkspaceFSReadWriteList(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	before := agentJSON(t, s, "coder")

	if err := s.WriteWorkspaceFile("coder", "AGENTS.md", "# hello"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadWorkspaceFile("coder", "AGENTS.md")
	if err != nil || got != "# hello" {
		t.Fatalf("read=%q err=%v", got, err)
	}
	entries, err := s.ListWorkspace("coder", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Path == "AGENTS.md" && !e.IsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("list=%+v", entries)
	}
	if after := agentJSON(t, s, "coder"); after != before {
		t.Fatalf("agent.json mutated:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestWorkspaceFSAutoMkdirParent(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	if err := s.WriteWorkspaceFile("coder", "rules/identity.md", "id"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadWorkspaceFile("coder", "rules/identity.md")
	if err != nil || got != "id" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestWorkspaceFSDeleteRecursive(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	if err := s.WriteWorkspaceFile("coder", "rules/a.md", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteWorkspaceFile("coder", "rules/sub/b.md", "b"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWorkspacePath("coder", "rules"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadWorkspaceFile("coder", "rules/a.md"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestWorkspaceFSMkdirAndRename(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	if err := s.MkdirWorkspace("coder", "notes"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteWorkspaceFile("coder", "notes/draft.md", "x"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameWorkspace("coder", "notes/draft.md", "notes/final.md"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadWorkspaceFile("coder", "notes/final.md")
	if err != nil || got != "x" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := s.ReadWorkspaceFile("coder", "notes/draft.md"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("old path should be gone: %v", err)
	}
}

func TestWorkspaceFSRejectsTraversal(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	for _, p := range []string{"../escape", "/etc/passwd", "..", "foo/../../etc/passwd"} {
		if err := s.WriteWorkspaceFile("coder", p, "x"); !errors.Is(err, ErrWorkspacePathInvalid) {
			t.Fatalf("path %q: want invalid, got %v", p, err)
		}
		if _, err := s.ReadWorkspaceFile("coder", p); !errors.Is(err, ErrWorkspacePathInvalid) {
			t.Fatalf("read %q: want invalid, got %v", p, err)
		}
	}
}

func TestWorkspaceFSRejectsSymlink(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	root, err := s.WorkspaceRootAbs("coder")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(s.root, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "leak")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadWorkspaceFile("coder", "leak"); !errors.Is(err, ErrWorkspacePathInvalid) {
		t.Fatalf("want symlink reject, got %v", err)
	}
	if err := s.WriteWorkspaceFile("coder", "leak", "x"); !errors.Is(err, ErrWorkspacePathInvalid) {
		t.Fatalf("want symlink reject on write, got %v", err)
	}
}

func TestWorkspaceFSFileSizeLimit(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	big := strings.Repeat("a", WorkspaceFileMaxBytes+1)
	if err := s.WriteWorkspaceFile("coder", "big.md", big); !errors.Is(err, ErrWorkspaceFileTooLarge) {
		t.Fatalf("want too large, got %v", err)
	}
	// Oversized file already on disk must also refuse read (no truncate success).
	root, _ := s.WorkspaceRootAbs("coder")
	if err := os.WriteFile(filepath.Join(root, "huge.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadWorkspaceFile("coder", "huge.md"); !errors.Is(err, ErrWorkspaceFileTooLarge) {
		t.Fatalf("want too large on read, got %v", err)
	}
}

func TestWorkspaceFSRenameEscapeRejected(t *testing.T) {
	s := setupWorkspaceAgent(t, "coder")
	if err := s.WriteWorkspaceFile("coder", "a.md", "x"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameWorkspace("coder", "a.md", "../escape.md"); !errors.Is(err, ErrWorkspacePathInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
}
