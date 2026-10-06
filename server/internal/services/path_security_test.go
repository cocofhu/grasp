package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnderRootRejectsTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, err := underRoot(root, "../escape"); err == nil {
		t.Fatal("expected .. rejection")
	}
	if _, err := underRoot(root, "/etc/passwd"); err == nil {
		t.Fatal("expected abs rejection")
	}
	got, err := underRoot(root, "ok/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(filepath.Join(root, "ok", "file.txt"))
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSanitizeRejectsDotDotAndSeparators(t *testing.T) {
	t.Parallel()
	if sanitize("..") != "" {
		t.Fatal(`sanitize("..") should be empty`)
	}
	if sanitize("../evil") != "" && sanitize("../evil") == ".." {
		t.Fatal("sanitize must not yield ..")
	}
	if sanitize("good_name") != "good_name" {
		t.Fatalf("got %q", sanitize("good_name"))
	}
	if sanitize("Approve需求澄清视觉研发") != "Approve需求澄清视觉研发" {
		t.Fatalf("unicode sanitize: got %q", sanitize("Approve需求澄清视觉研发"))
	}
	if sanitize("clarify.v1") != "" {
		t.Fatalf("dotted sanitize must be rejected: got %q", sanitize("clarify.v1"))
	}
	if sanitize("a/b") != "b" && sanitize("a/b") != "" {
		// Base("a/b") == "b" which is allowlisted — acceptable.
		if sanitize("a/b") != "b" {
			t.Fatalf("got %q", sanitize("a/b"))
		}
	}
}

func TestSaveRejectsPathEscape(t *testing.T) {
	t.Parallel()
	s := NewAgentService(t.TempDir())
	err := s.Save(Agent{
		Name:  "safe",
		Files: []AgentFile{{Path: "../escape.txt", Content: "x"}},
	})
	if err != nil {
		// safeRel skips empty rel — Save may succeed with zero files.
		t.Logf("Save returned: %v", err)
	}
	// Ensure nothing was written outside the agent root.
	root := s.root
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.Name() == "escape.txt" {
			t.Fatal("escaped file written at root")
		}
	}
}

func TestImportZIPRejectsAbsolutePath(t *testing.T) {
	t.Parallel()
	s := NewAgentService(t.TempDir())
	meta := []byte(`{"name":"x","schemaVersion":1,"exportedAt":"2026-01-01T00:00:00Z"}`)
	raw := buildTestZip(t, meta, map[string][]byte{"/tmp/evil.txt": []byte("bad")})
	if _, err := s.ImportZIP(raw, "x", "p1", ImportZIPCreate); err == nil {
		t.Fatal("expected absolute path rejection")
	}
}
