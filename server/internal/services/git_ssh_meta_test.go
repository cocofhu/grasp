package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
)

func TestRejectSecretEnvKeys(t *testing.T) {
	if err := RejectSecretEnvKeys(map[string]string{"FEATURE": "1", "GITLAB_URL": "https://gl"}); err != nil {
		t.Fatal(err)
	}
	err := RejectSecretEnvKeys(map[string]string{"GITHUB_TOKEN": "t", "CURSOR_API_KEY": "c"})
	if !errors.Is(err, ErrSecretEnvKey) || !strings.Contains(err.Error(), "CURSOR_API_KEY / GITHUB_TOKEN") {
		t.Fatalf("err=%v", err)
	}
}

func TestAgentSaveRejectsSecretEnvKeys(t *testing.T) {
	svc := NewAgentService(t.TempDir())
	for _, k := range envauth.SecretEnvKeys() {
		err := svc.Save(Agent{AcpBackend: AcpBackendCursor, Name: "secret-agent", Env: map[string]string{k: "x"}})
		if !errors.Is(err, ErrSecretEnvKey) {
			t.Fatalf("%s: err=%v", k, err)
		}
	}
	if err := svc.Save(Agent{AcpBackend: AcpBackendCursor, Name: "secret-agent", Env: map[string]string{"GITLAB_URL": "https://gl"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedAgentSaveRejectsSecretEnvKeys(t *testing.T) {
	svc := NewSharedAgentService(t.TempDir())
	for _, k := range []string{EnvGitSSHKnownHosts, "GITLAB_TOKEN", "GRASP_CLAUDE_API_KEY", "ACP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_HARNESS_API_KEY"} {
		err := svc.Save(SharedAgentConfig{Env: map[string]string{k: "h"}})
		if !errors.Is(err, ErrSecretEnvKey) {
			t.Fatalf("%s: err=%v", k, err)
		}
	}
}
