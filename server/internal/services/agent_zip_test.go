package services

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func buildTestZip(t *testing.T, meta []byte, files map[string][]byte) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, err := zw.Create("agent.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(meta); err != nil {
		t.Fatal(err)
	}
	for path, body := range files {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExportImportZIPRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)

	binary := []byte{0x00, 0x01, 0xFF, 0xFE, 0x89, 0x50, 0x4E, 0x47}
	err := s.Save(Agent{
		Name:              "trip",
		AcpBackend:        AcpBackendClaudeCode,
		GitCredentialType: "gitlab_https",
		Env:               map[string]string{"GITLAB_URL": "https://gl.example"},
		MCP:               DefaultPlatformMCP(),
		Files: []AgentFile{
			{Path: "rules/trip.md", Content: "# trip"},
			{Path: "assets/logo.bin", Content: string(binary)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := s.ExportZIP("trip")
	if err != nil {
		t.Fatalf("ExportZIP: %v", err)
	}

	s2 := NewAgentService(t.TempDir())
	imported, err := s2.ImportZIP(raw, "trip-copy", "p1", ImportZIPCreate)
	if err != nil {
		t.Fatalf("ImportZIP: %v", err)
	}
	if imported.Name != "trip-copy" {
		t.Fatalf("name = %q", imported.Name)
	}
	if imported.Env["GITLAB_URL"] != "https://gl.example" {
		t.Fatalf("env not preserved: %+v", imported.Env)
	}
	if imported.GitCredentialType != "gitlab_https" {
		t.Fatalf("gitCredentialType = %q, want gitlab_https", imported.GitCredentialType)
	}
	if imported.AcpBackend != AcpBackendClaudeCode {
		t.Fatalf("acpBackend = %q, want %s", imported.AcpBackend, AcpBackendClaudeCode)
	}
	if len(imported.Files) != 2 {
		t.Fatalf("files = %d", len(imported.Files))
	}
	var gotBin string
	for _, f := range imported.Files {
		if f.Path == "assets/logo.bin" {
			gotBin = f.Content
		}
	}
	if gotBin != string(binary) {
		t.Fatalf("binary round-trip failed")
	}
}

func TestImportZIPRejectsBadSchema(t *testing.T) {
	s := NewAgentService(t.TempDir())
	meta := []byte(`{"name":"x","schemaVersion":99,"exportedAt":"2026-01-01T00:00:00Z"}`)
	raw := buildTestZip(t, meta, nil)
	if _, err := s.ImportZIP(raw, "x", "p1", ImportZIPCreate); err == nil {
		t.Fatal("expected schema error")
	}
}

func TestImportZIPRejectsPathTraversal(t *testing.T) {
	s := NewAgentService(t.TempDir())
	meta := []byte(`{"name":"x","schemaVersion":1,"exportedAt":"2026-01-01T00:00:00Z"}`)
	raw := buildTestZip(t, meta, map[string][]byte{"../evil.txt": []byte("bad")})
	if _, err := s.ImportZIP(raw, "x", "p1", ImportZIPCreate); err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestImportZIPOverwriteClearsOldFiles(t *testing.T) {
	root := t.TempDir()
	s := NewAgentService(root)

	if err := s.Save(Agent{
		AcpBackend: AcpBackendCursor,
		Name:       "target",
		Files: []AgentFile{
			{Path: "rules/old.md", Content: "old"},
			{Path: "skills/keep/SKILL.md", Content: "keep"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	meta := []byte(`{"name":"target","acpBackend":"cursor","schemaVersion":1,"exportedAt":"2026-01-01T00:00:00Z","mcp":[]}`)
	raw := buildTestZip(t, meta, map[string][]byte{"rules/new.md": []byte("new")})

	if _, err := s.ImportZIP(raw, "target", "p1", ImportZIPOverwrite); err != nil {
		t.Fatalf("ImportZIP overwrite: %v", err)
	}

	got, ok := s.Get("target")
	if !ok {
		t.Fatal("agent missing")
	}
	if len(got.Files) != 1 || got.Files[0].Path != "rules/new.md" {
		t.Fatalf("unexpected files after overwrite: %+v", got.Files)
	}
	if _, err := os.Stat(filepath.Join(root, "target", WorkDirName, "skills", "keep", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("old cursor file should be removed on overwrite")
	}
}

func TestImportZIPAcpBackendCreateAndOverwrite(t *testing.T) {
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{Name: "src", AcpBackend: AcpBackendTrae}); err != nil {
		t.Fatal(err)
	}
	raw, err := s.ExportZIP("src")
	if err != nil {
		t.Fatal(err)
	}
	dst := NewAgentService(t.TempDir())
	created, err := dst.ImportZIP(raw, "src-copy", "p1", ImportZIPCreate)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.AcpBackend != AcpBackendTrae {
		t.Fatalf("create acpBackend=%q", created.AcpBackend)
	}
	if err := dst.Save(Agent{Name: "src-copy", AcpBackend: AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	over, err := dst.ImportZIP(raw, "src-copy", "p1", ImportZIPOverwrite)
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if over.AcpBackend != AcpBackendTrae {
		t.Fatalf("overwrite acpBackend=%q", over.AcpBackend)
	}
}

func TestImportZIPMissingAcpBackendRejected(t *testing.T) {
	s := NewAgentService(t.TempDir())
	meta := []byte(`{"name":"nobackend","schemaVersion":1,"exportedAt":"2026-01-01T00:00:00Z"}`)
	raw := buildTestZip(t, meta, map[string][]byte{"rules/a.md": []byte("a")})
	if _, err := s.ImportZIP(raw, "nobackend", "p1", ImportZIPCreate); !errors.Is(err, ErrInvalidAcpBackend) {
		t.Fatalf("err=%v want ErrInvalidAcpBackend", err)
	}
}

func TestImportZIPMissingAgentJSON(t *testing.T) {
	s := NewAgentService(t.TempDir())
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, _ := zw.Create("rules/x.md")
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	if _, err := s.ImportZIP(buf.Bytes(), "x", "p1", ImportZIPCreate); err == nil {
		t.Fatal("expected missing agent.json error")
	}
}

func TestImportZIPRequiresProjectAndBindsTarget(t *testing.T) {
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{AcpBackend: AcpBackendCursor, Name: "src", ProjectID: "other"}); err != nil {
		t.Fatal(err)
	}
	raw, err := s.ExportZIP("src")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportZIP(raw, "copy", " ", ImportZIPCreate); !errors.Is(err, ErrAgentProjectRequired) {
		t.Fatalf("empty project: err=%v", err)
	}
	got, err := s.ImportZIP(raw, "copy", "target", ImportZIPCreate)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "target" {
		t.Fatalf("projectId=%q, want target", got.ProjectID)
	}
}
