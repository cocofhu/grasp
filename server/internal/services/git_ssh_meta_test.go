package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestApplyProjectSSHToSpecUsesOnlyProjectCredentials(t *testing.T) {
	spec := &sandbox.Spec{Env: map[string]string{
		EnvGitSSHPrivateKey: "env-key",
		EnvGitSSHKnownHosts: "env-hosts",
		"KEEP":              "1",
	}}
	ApplyProjectSSHToSpec(spec, map[string]string{
		EnvGitSSHPrivateKey: "project-key",
		EnvGitSSHKnownHosts: "project-hosts",
	})
	if spec.SSHPrivateKey != "project-key" || spec.SSHKnownHosts != "project-hosts" {
		t.Fatalf("spec ssh = %q / %q", spec.SSHPrivateKey, spec.SSHKnownHosts)
	}
	if _, ok := spec.Env[EnvGitSSHPrivateKey]; ok {
		t.Fatal("GIT_SSH_* must be stripped from Spec.Env")
	}
	if spec.Env["KEEP"] != "1" {
		t.Fatalf("env=%v", spec.Env)
	}

	bare := &sandbox.Spec{Env: map[string]string{EnvGitSSHPrivateKey: "env-key"}}
	ApplyProjectSSHToSpec(bare, nil)
	if bare.SSHPrivateKey != "" {
		t.Fatalf("env value must not be injected: %q", bare.SSHPrivateKey)
	}
}

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
		err := svc.Save(Agent{AcpBackend: AcpBackendCursor, Name: "secret-agent", ProjectID: "p1", Env: map[string]string{k: "x"}})
		if !errors.Is(err, ErrSecretEnvKey) {
			t.Fatalf("%s: err=%v", k, err)
		}
	}
	if err := svc.Save(Agent{AcpBackend: AcpBackendCursor, Name: "secret-agent", ProjectID: "p1", Env: map[string]string{"GITLAB_URL": "https://gl"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedAgentSaveRejectsSecretEnvKeys(t *testing.T) {
	svc := NewSharedAgentService(t.TempDir())
	for _, k := range []string{EnvGitSSHKnownHosts, "GITLAB_TOKEN", "GRASP_CLAUDE_API_KEY"} {
		err := svc.Save(SharedAgentConfig{ProjectID: "p1", Env: map[string]string{k: "h"}})
		if !errors.Is(err, ErrSecretEnvKey) {
			t.Fatalf("%s: err=%v", k, err)
		}
	}
}
