package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/execution"
)

func TestLocalBackendFiltersAmbientSecrets(t *testing.T) {
	for _, mode := range []string{execution.ModeWorktree, execution.ModeLocal} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv(execution.EnvMode, mode)
			for _, key := range append(envauth.SecretEnvKeys(), "GH_TOKEN", "GLAB_TOKEN", "SSH_AUTH_SOCK", "SSH_AGENT_PID", "GIT_CONFIG_COUNT", "GIT_SSH_COMMAND") {
				t.Setenv(key, "unselected-host-value")
			}
			for _, localAuth := range []string{"", "1"} {
				env := envList(map[string]string{"GRASP_LOCAL_AUTH": localAuth, "GRASP_MODEL_API_KEY": "selected-key"})
				if strings.Contains(strings.Join(env, "\n"), "unselected-host-value") {
					t.Fatal("backend inherited a host secret or Git authorization override")
				}
				if envValue(env, "GRASP_MODEL_API_KEY") != "selected-key" {
					t.Fatal("explicit workspace credential was dropped")
				}
			}
		})
	}
}

func TestLocalAuthSnapshotDoesNotWriteBackOrFollowLinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	hostAuth := filepath.Join(source, "auth.json")
	if err := os.WriteFile(hostAuth, []byte("original-auth"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "token"), []byte("nested-auth"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "unrelated"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, target := range map[string]string{
		filepath.Join(source, "external"):               outside,
		filepath.Join(source, "nested", "external"):     outside,
		filepath.Join(source, "nested", "linked-token"): hostAuth,
	} {
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	destination := t.TempDir()
	snapshotLocalConfig(source, destination, true)
	for _, name := range []string{"auth.json", "nested/token"} {
		path := filepath.Join(destination, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("auth snapshot is not a private regular file: %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte("changed-by-cli"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"external", "nested/external", "nested/linked-token"} {
		if _, err := os.Lstat(filepath.Join(destination, name)); !os.IsNotExist(err) {
			t.Fatalf("snapshot followed or retained a source link: %s", name)
		}
	}
	for name, want := range map[string]string{"auth.json": "original-auth", "nested/token": "nested-auth"} {
		body, err := os.ReadFile(filepath.Join(source, name))
		if err != nil || string(body) != want {
			t.Fatalf("CLI write changed the host auth: %s", name)
		}
	}
}

func TestLocalAuthMergeDoesNotWriteThroughDestinationLinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := filepath.Join(home, ".claude")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "settings.json"), []byte(`{"model":"host-model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, hardlink := range []bool{false, true} {
		destination := t.TempDir()
		target := filepath.Join(t.TempDir(), "settings.json")
		const original = `{"mcp":{"managed":true}}`
		if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		link := os.Symlink
		if hardlink {
			link = os.Link
		}
		if err := link(target, filepath.Join(destination, "settings.json")); err != nil {
			t.Fatal(err)
		}
		seedLocalAuthConfigHome("claude_code", destination)
		if body, err := os.ReadFile(target); err != nil || string(body) != original {
			t.Fatal("merging the private configuration changed a linked host file")
		}
	}
}

func TestLocalAuthFromExplicitEmptyBindingsIgnoresLegacySecrets(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeLocal)
	spec := Spec{GitBindingsConfigured: true, SSHPrivateKey: "unselected-private-key", SSHKnownHosts: "unselected-hosts", Env: map[string]string{"GITHUB_TOKEN": "unselected-token", "GIT_SSH_PRIVATE_KEY": "unselected-key"}}
	auth := authFromSpec(spec)
	if auth.Configured || len(auth.Credentials) != 0 || auth.GitHubToken != "" || auth.SSHPrivateKey != "" || auth.SSHKnownHosts != "" {
		t.Fatal("empty local binding revived legacy secrets instead of host Git authorization")
	}
	spec.GitCredentials = []GitCredential{{Kind: RepoCredentialGitHubHTTPS, Secret: "selected-token"}}
	auth = authFromSpec(spec)
	if !auth.Configured || len(auth.Credentials) != 1 || auth.Credentials[0].Secret != "selected-token" || auth.GitHubToken != "" || auth.SSHPrivateKey != "" {
		t.Fatal("selected local credentials fell back to another identity")
	}
}

func TestLocalCreateAndReprimeKeepExecutionMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(execution.EnvMode, execution.ModeLocal)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	var environments []map[string]string
	t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, env map[string]string, _ string) (int, func(), error) {
		environments = append(environments, env)
		return 9, func() {}, nil
	}))
	spec := Spec{Name: "local-mode", ConfigHome: t.TempDir(), GitBindingsConfigured: true, Env: map[string]string{BridgePasswordEnv: "pw", "AGENT_PROVIDER": "codex"}}
	manager := NewManager(nil, ManagerOptions{})
	sb, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()) })
	spec.ConfigHome = t.TempDir()
	spec.Env["AGENT_PROVIDER"] = "claude_code"
	if err := manager.Reprime(context.Background(), sb, spec, nil); err != nil {
		t.Fatal(err)
	}
	if len(environments) != 2 {
		t.Fatal("create and reprime did not both start a backend")
	}
	for _, env := range environments {
		if env[execution.EnvMode] != execution.ModeLocal || env["HOME"] == os.Getenv("HOME") || env["HOME"] == "" {
			t.Fatal("local backend lost its mode or retained the host HOME")
		}
	}
}

func TestLocalExecutionSeedsHostCLIAuthOnlyForLocalBindings(t *testing.T) {
	for _, localAuth := range []bool{false, true} {
		name := "cloud-binding"
		if localAuth {
			name = "local-binding"
		}
		t.Run(name, func(t *testing.T) {
			hostHome := t.TempDir()
			t.Setenv("HOME", hostHome)
			t.Setenv(execution.EnvMode, execution.ModeLocal)
			t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
			execution.ResetForTest()
			t.Cleanup(execution.ResetForTest)
			t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
			for providerDir, file := range map[string]string{".codex": "auth.json", ".claude": ".credentials.json"} {
				if err := os.MkdirAll(filepath.Join(hostHome, providerDir), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(hostHome, providerDir, file), []byte("host-login"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(hostHome, ".claude.json"), []byte(`{"hostLogin":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, env map[string]string, _ string) (int, func(), error) {
				if env["HOME"] == "" || env["HOME"] == hostHome {
					t.Fatal("local execution did not use a private HOME")
				}
				file := "auth.json"
				if env["AGENT_PROVIDER"] == "claude_code" {
					file = ".credentials.json"
					_, err := os.Stat(filepath.Join(env["HOME"], ".claude.json"))
					if localAuth && err != nil || !localAuth && !os.IsNotExist(err) {
						t.Fatal("Claude root auth did not follow the selected binding kind")
					}
				}
				_, err := os.Stat(filepath.Join(env["CONFIG_ROOT"], file))
				if localAuth && err != nil || !localAuth && !os.IsNotExist(err) {
					t.Fatal("host CLI auth did not follow the selected binding kind")
				}
				return 9, func() {}, nil
			}))
			spec := Spec{Name: name, ConfigHome: t.TempDir(), GitBindingsConfigured: true, Env: map[string]string{BridgePasswordEnv: "pw", "AGENT_PROVIDER": "codex"}}
			if localAuth {
				spec.Env["GRASP_LOCAL_AUTH"] = "1"
			}
			manager := NewManager(nil, ManagerOptions{})
			sb, err := manager.Create(context.Background(), spec)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sb.Destroy(context.Background()) })
			spec.ConfigHome = t.TempDir()
			spec.Env["AGENT_PROVIDER"] = "claude_code"
			if err := manager.Reprime(context.Background(), sb, spec, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalAuthSeedOmitsHostWorktreesAndHistory(t *testing.T) {
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	hostCodex := filepath.Join(hostHome, ".codex")
	destination := filepath.Join(hostCodex, "worktrees", "current", "grasp-acp-private")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"auth.json":                   "host-login",
		"history.jsonl":               "unrelated-chat-history",
		"state.sqlite":                "unrelated-state",
		"worktrees/current/unrelated": "unrelated-working-tree",
	} {
		if err := os.WriteFile(filepath.Join(hostCodex, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seedLocalAuthConfigHome("codex", destination)
	if body, err := os.ReadFile(filepath.Join(destination, "auth.json")); err != nil || string(body) != "host-login" {
		t.Fatal("native auth was not imported")
	}
	for _, name := range []string{"worktrees", "history.jsonl", "state.sqlite"} {
		if _, err := os.Stat(filepath.Join(destination, name)); !os.IsNotExist(err) {
			t.Fatalf("host runtime data was copied or recursively reimported: %s", name)
		}
	}
}

func TestLocalSnapshotKeepsExecutableHooksAndExcludesDestination(t *testing.T) {
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	source := filepath.Join(hostHome, ".claude")
	if err := os.MkdirAll(filepath.Join(source, "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "hooks", "hook.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	seedLocalAuthConfigHome("claude_code", destination)
	info, err := os.Stat(filepath.Join(destination, "hooks", "hook.sh"))
	if err != nil || info.Mode().Perm() != 0o711 {
		t.Fatal("private hook snapshot lost its executable bits")
	}
	// Exercise overlap handling independently of the provider file allowlist.
	nestedDestination := filepath.Join(source, "hooks", "managed-config")
	snapshotLocalConfig(source, nestedDestination, false)
	if _, err := os.Stat(filepath.Join(nestedDestination, "hooks")); !os.IsNotExist(err) {
		t.Fatal("snapshot recursively copied the source subtree containing itself")
	}
}

func TestLocalKimiOAuthCredentialsArePrivateSnapshots(t *testing.T) {
	for _, nativeDir := range []string{".kimi", ".kimi-code"} {
		t.Run(nativeDir, func(t *testing.T) {
			hostHome := t.TempDir()
			t.Setenv("HOME", hostHome)
			hostCredentials := filepath.Join(hostHome, nativeDir, "credentials")
			if err := os.MkdirAll(filepath.Join(hostCredentials, "oauth"), 0o700); err != nil {
				t.Fatal(err)
			}
			hostToken := filepath.Join(hostCredentials, "oauth", "token.json")
			const original = `{"access_token":"synthetic-kimi-oauth-token"}`
			if err := os.WriteFile(hostToken, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			destination := t.TempDir()
			seedLocalAuthConfigHome("kimi_code", destination)
			privateToken := filepath.Join(destination, "credentials", "oauth", "token.json")
			info, err := os.Lstat(privateToken)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatal("Kimi OAuth token was not copied to a private regular file")
			}
			if body, err := os.ReadFile(privateToken); err != nil || string(body) != original {
				t.Fatal("Kimi OAuth snapshot did not retain the native login")
			}
			if err := os.WriteFile(privateToken, []byte(`{"access_token":"changed-by-cli"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if body, err := os.ReadFile(hostToken); err != nil || string(body) != original {
				t.Fatal("Kimi OAuth snapshot wrote back to the host login")
			}
		})
	}
}
