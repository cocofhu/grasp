package sandbox

import (
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexLocalLoginConfigHome(t *testing.T) {
	source := filepath.Join(t.TempDir(), "auth.json")
	secret := `{"tokens":{"access_token":"test-only-token"}}`
	if err := os.WriteFile(source, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRASP_CODEX_AUTH_FILE", source)
	home, err := BuildConfigHome(ConfigHomeSpec{Codex: true, MCP: []MCPServerSpec{{Name: "artifact-store", URL: "http://example.test/mcp", Headers: map[string]string{"Authorization": "Bearer test"}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	b, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil || string(b) != secret {
		t.Fatal("login was not copied")
	}
	b, err = os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "http_headers") || !strings.Contains(string(b), "artifact-store") {
		t.Fatal("MCP config missing")
	}
	if b, err := os.ReadFile(filepath.Join(home, "AGENTS.md")); err != nil || len(b) == 0 {
		t.Fatal("rules missing")
	}
	other, err := BuildConfigHome(ConfigHomeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(other) })
	if _, err := os.Stat(filepath.Join(other, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("credentials leaked into other backend")
	}
}

func TestCodexLocalLoginFailsClosed(t *testing.T) {
	t.Setenv("GRASP_CODEX_AUTH_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := ReadCodexLocalAuth(); err == nil {
		t.Fatal("missing auth accepted")
	}
	p := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(p, []byte(`{"tokens":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRASP_CODEX_AUTH_FILE", p)
	if _, err := ReadCodexLocalAuth(); err == nil {
		t.Fatal("empty auth accepted")
	}
}
