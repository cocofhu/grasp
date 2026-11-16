package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripRetiredSchedulerMCP(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "agent.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("with-scheduler", `{"acpBackend":"cursor","mcp":[
		{"name":"artifact-store","url":"${GRASP_ARTIFACT_URL}"},
		{"name":"task-scheduler","url":"${GRASP_SCHEDULER_URL}"},
		{"name":"renamed","url":"http://grasp:8080/mcp/task-scheduler/a"}
	]}`)
	cleanBody := `{"acpBackend":"cursor","mcp":[{"name":"git","url":"http://example.com/mcp"}]}`
	clean := write("clean", cleanBody)
	brokenBody := `{not json`
	broken := write("broken", brokenBody)
	if err := os.WriteFile(filepath.Join(root, "stray-file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewAgentService(root)
	for range 2 {
		if err := s.StripRetiredSchedulerMCP(); err != nil {
			t.Fatal(err)
		}
		a, ok := s.Get("with-scheduler")
		if !ok || len(a.MCP) != 1 || a.MCP[0].Name != "artifact-store" || a.AcpBackend != "cursor" {
			t.Fatalf("agent = %+v", a)
		}
	}
	for path, want := range map[string]string{clean: cleanBody, broken: brokenBody} {
		if b, _ := os.ReadFile(path); string(b) != want {
			t.Fatalf("%s rewritten: %s", path, b)
		}
	}
	if err := NewAgentService(filepath.Join(root, "missing")).StripRetiredSchedulerMCP(); err != nil {
		t.Fatal(err)
	}
}
