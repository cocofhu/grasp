package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexBackendRouting(t *testing.T) {
	if NormalizeBackend("codex") != BackendCodex || DefaultConfigRoot(BackendCodex) != "/root/.codex" || AgentRuntimeLabel(BackendCodex) != "codex-cli" {
		t.Fatal("Codex fell back to Cursor")
	}
	t.Setenv("GRASP_CODEX_AUTH_FILE", "")
	if _, err := PrepareAuthEnv(BackendCodex, nil, ""); err == nil {
		t.Fatal("missing login accepted")
	}
	env, err := PrepareAuthEnv(BackendCodex, map[string]string{"CODEX_API_KEY": "test"}, "")
	if err != nil || env["OPENAI_API_KEY"] != "test" {
		t.Fatal("API key alias failed")
	}
}

func TestCodexUsesExplicitLocalLoginOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(p, []byte(`{"tokens":{"access_token":"test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRASP_CODEX_AUTH_FILE", p)
	env, err := PrepareAuthEnv(BackendCodex, map[string]string{"FEATURE": "on"}, "")
	if err != nil || env["FEATURE"] != "on" || env["OPENAI_API_KEY"] != "" {
		t.Fatal("local login gate failed or leaked a key")
	}
	if _, err := PrepareAuthEnv(BackendClaudeCode, nil, ""); err == nil {
		t.Fatal("local Codex auth authorized another provider")
	}
}
