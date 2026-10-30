package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/execution"
)

func useWorktreeGitHelper(t *testing.T) {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	backend := filepath.Join(filepath.Dir(source), "..", "..", "..", "sandbox-gateway", "sandbox", "bin", "backend")
	t.Setenv("GRASP_WORKTREE_BACKEND", backend)
}

func TestWorktreeAgentPushUsesSelectedCredential(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	isolateWorktreeGit(t)
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	var backendEnv map[string]string
	t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, env map[string]string, _ string) (int, func(), error) {
		backendEnv = env
		return 9, func() {}, nil
	}))
	bare := bareWorktreeRepo(t)
	gitWorktreeRun(t, bare, "config", "http.receivepack", "true")
	const selected = "selected-push-token"
	srv := authGitServer(t, bare, selected)
	repoURL := srv.URL + "/repo.git"
	sb, err := NewManager(nil, ManagerOptions{}).Create(context.Background(), Spec{
		Name: "push", GitBindingsConfigured: true,
		GitCredentials: []GitCredential{{ID: "selected", Kind: RepoCredentialGitHubHTTPS, Scope: repoURL, Secret: selected}},
		Env: map[string]string{
			BridgePasswordEnv: "pw", "AGENT_PROVIDER": "codex", envRunID: "run-push", envLaunchKind: execution.LaunchRun,
			"GIT_REPOS": EncodeRepos([]RepoSpec{{Name: "app", URL: repoURL, Branch: "feature"}}),
			"GH_TOKEN":  "unselected-token", "GITHUB_TOKEN": "unselected-token",
			"GIT_USER_NAME": "Chosen Author", "GIT_USER_EMAIL": "chosen@example.com",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()); execution.End("run-push") })
	for _, value := range backendEnv {
		if strings.Contains(value, selected) || strings.Contains(value, "unselected-token") {
			t.Fatal("backend environment contains a Git secret")
		}
	}
	author := exec.Command("git", "var", "GIT_AUTHOR_IDENT")
	author.Dir = filepath.Join(sb.WorkspaceDir, "app")
	author.Env = envList(backendEnv)
	if out, err := author.CombinedOutput(); err != nil || !strings.Contains(string(out), "Chosen Author <chosen@example.com>") {
		t.Fatal("private Git config did not retain the selected commit author")
	}
	gitWorktreeRun(t, author.Dir, "config", "--local", "user.name", "Repository Author")
	gitWorktreeRun(t, author.Dir, "config", "--local", "user.email", "repository@example.com")
	localAuthor := exec.Command("git", "var", "GIT_AUTHOR_IDENT")
	localAuthor.Dir = author.Dir
	localAuthor.Env = envList(backendEnv)
	if out, err := localAuthor.CombinedOutput(); err != nil || !strings.Contains(string(out), "Repository Author <repository@example.com>") {
		t.Fatal("global Git author defaults overrode the repository's author")
	}
	cmd := exec.Command("git", "push", "origin", "HEAD:refs/heads/agent-pushed")
	cmd.Dir = filepath.Join(sb.WorkspaceDir, "app")
	cmd.Env = envList(backendEnv)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("agent push failed: %v: %s", err, out)
	}
	if gitWorktreeOut(t, bare, "rev-parse", "refs/heads/agent-pushed") != gitWorktreeOut(t, bare, "rev-parse", "refs/heads/feature") {
		t.Fatal("agent push did not reach the authenticated remote")
	}
	// The manager's local command path must use the same authorization as the
	// backend rather than reverting to the server's environment.
	out, err := sb.ExecScript(context.Background(), 10*time.Second, "sh", "cd app && git push origin HEAD:refs/heads/platform-pushed")
	if err != nil {
		t.Fatalf("local push failed: %v: %s", err, out)
	}
	authDir := filepath.Dir(filepath.Dir(backendEnv["GIT_CONFIG_GLOBAL"]))
	execution.End("run-push")
	if _, err := os.Stat(authDir); !os.IsNotExist(err) {
		t.Fatal("ending the launch did not remove the backend's credentials")
	}
}

func TestWorktreeGitSessionsKeepAuthorizationSeparate(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	isolateWorktreeGit(t)
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	t.Setenv("GH_TOKEN", "ambient-token")
	t.Setenv("GITHUB_TOKEN", "ambient-token")
	t.Setenv("GIT_ASKPASS", "/unselected/askpass")
	t.Setenv("GIT_SSH_VARIANT", "plink")
	t.Setenv("GRASP_GIT_CREDENTIAL_HELPER", "/unselected/helper.py")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "!printf 'username=ambient\\npassword=ambient-token\\n'")
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir())
	originalHome := os.Getenv("HOME")
	var sessions []map[string]string
	t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, env map[string]string, _ string) (int, func(), error) {
		sessions = append(sessions, env)
		return 9, func() {}, nil
	}))
	repo := initWorktreeRepo(t)
	// Local Git config is shared by all worktrees. It must not add another
	// account after the selected helper, including for an empty selection.
	gitWorktreeRun(t, repo, "config", "credential.helper", "!printf 'username=local\\npassword=local-token\\n'")
	m := NewManager(nil, ManagerOptions{})
	create := func(name, token string) *Sandbox {
		t.Helper()
		spec := Spec{Name: name, GitBindingsConfigured: true, Env: map[string]string{
			BridgePasswordEnv: "pw", "AGENT_PROVIDER": "codex", envRunID: "run-identities", envLaunchKind: execution.LaunchRun,
			"GIT_REPOS": EncodeRepos([]RepoSpec{{Name: "app", URL: repo, Branch: "main"}}),
			"GH_TOKEN":  "spec-token", "GIT_CONFIG_PARAMETERS": "unselected-config", "GIT_SSH_COMMAND": "unselected-ssh",
		}}
		if token != "" {
			spec.GitCredentials = []GitCredential{
				{ID: name, Kind: RepoCredentialGitHubHTTPS, Scope: "https://github.com/acme/app", Secret: token},
				{ID: name + "-gl", Kind: RepoCredentialGitLabHTTPS, Scope: "https://gitlab.example", Secret: token + "-gl"},
			}
		}
		sb, err := m.Create(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { sb.Destroy(context.Background()) })
		return sb
	}
	first := create("first", "first-selected")
	second := create("second", "second-selected")
	empty := create("empty", "")
	t.Cleanup(func() { execution.End("run-identities") })
	if first.WorkspaceDir != second.WorkspaceDir || first.WorkspaceDir != empty.WorkspaceDir {
		t.Fatal("test sessions did not share the launch worktree")
	}
	for i, sb := range []*Sandbox{first, second, empty} {
		env := sessions[i]
		if env["GIT_CONFIG_NOSYSTEM"] != "1" || env["GIT_CONFIG_VALUE_0"] != "" || env["GH_TOKEN"] != "" || env["GIT_CONFIG_PARAMETERS"] != "" || env["GIT_SSH_VARIANT"] != "ssh" {
			t.Fatal("session retained an ambient or spec Git authorization override")
		}
		if i > 0 && env["GIT_CONFIG_GLOBAL"] == sessions[i-1]["GIT_CONFIG_GLOBAL"] {
			t.Fatal("sessions shared a credential directory")
		}
		if envValue(envList(env), "HOME") != originalHome {
			t.Fatal("Git configuration changed the process home")
		}
		wantHelper := filepath.Join(filepath.Dir(env["GIT_CONFIG_GLOBAL"]), ".grasp", "git-credentials", "helper.py")
		if env["GRASP_GIT_CREDENTIAL_HELPER"] != wantHelper {
			t.Fatal("runtime Git operations did not use the session's private helper")
		}
		out, err := worktreeCredentialFill(env, filepath.Join(sb.WorkspaceDir, "app"), "https", "github.com", "acme/app.git")
		if i == 2 {
			if err == nil || strings.Contains(out, "password=") {
				t.Fatal("empty selection fell back to another account")
			}
			continue
		}
		want := []string{"first-selected", "second-selected"}[i]
		if err != nil || !strings.Contains(out, "password="+want) {
			t.Fatal("session did not use its selected account")
		}
		out, err = worktreeCredentialFill(env, filepath.Join(sb.WorkspaceDir, "app"), "https", "github.com", "acme/other.git")
		if err == nil || strings.Contains(out, "password=") {
			t.Fatal("repository-scoped token escaped its selected repository")
		}
		gh, err := os.ReadFile(filepath.Join(env["GH_CONFIG_DIR"], "hosts.yml"))
		if err != nil || !strings.Contains(string(gh), want) || !strings.Contains(string(gh), "user: grasp-selected") {
			t.Fatal("GitHub CLI configuration did not use the selected account")
		}
		gl, err := os.ReadFile(filepath.Join(env["GLAB_CONFIG_DIR"], "config.yml"))
		if err != nil || !strings.Contains(string(gl), want+"-gl") {
			t.Fatal("GitLab CLI configuration did not use the selected account")
		}
		info, err := os.Stat(filepath.Join(env["GH_CONFIG_DIR"], "hosts.yml"))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("CLI credential file is not private")
		}
	}
	first.Destroy(context.Background())
	if _, err := os.Stat(sessions[0]["GIT_CONFIG_GLOBAL"]); !os.IsNotExist(err) {
		t.Fatal("stopped session kept its credentials")
	}
	out, err := worktreeCredentialFill(sessions[1], filepath.Join(second.WorkspaceDir, "app"), "https", "github.com", "acme/app")
	if err != nil || !strings.Contains(out, "password=second-selected") {
		t.Fatal("stopping one session removed another session's credentials")
	}
}

func TestWorktreeGitCleanupOnBackendStartFailure(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	useWorktreeGitHelper(t)
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	var privateConfig string
	t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, env map[string]string, _ string) (int, func(), error) {
		privateConfig = env["GIT_CONFIG_GLOBAL"]
		return 0, nil, errors.New("backend failed")
	}))
	_, err := NewManager(nil, ManagerOptions{}).Create(context.Background(), Spec{
		Name: "start-failed", GitBindingsConfigured: true,
		GitCredentials: []GitCredential{{ID: "gh", Kind: RepoCredentialGitHubHTTPS, Scope: "https://github.com", Secret: "selected-token"}},
		Env:            map[string]string{BridgePasswordEnv: "pw", "AGENT_PROVIDER": "codex", envRunID: "run-start-failed"},
	})
	if err == nil || privateConfig == "" {
		t.Fatal("backend start failure was not exercised")
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(privateConfig))); !os.IsNotExist(err) {
		t.Fatal("failed backend left its credentials behind")
	}
}

func TestWorktreeLegacyGitIdentityUsesPrivateFiles(t *testing.T) {
	useWorktreeGitHelper(t)
	t.Setenv("GITHUB_TOKEN", "ambient-github-token")
	t.Setenv("GIT_SSH_COMMAND", "ambient-ssh")
	env := map[string]string{}
	cleanup, err := prepareWorktreeGit(context.Background(), Spec{
		Env:           map[string]string{"GITHUB_TOKEN": "legacy-selected", "GITHUB_URL": "https://git.example/path"},
		SSHPrivateKey: "legacy-private-key", SSHKnownHosts: "known-host-entry",
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out, err := worktreeCredentialFill(env, t.TempDir(), "https", "git.example", "team/app.git")
	if err != nil || !strings.Contains(out, "password=legacy-selected") {
		t.Fatal("legacy spec identity was not converted to a private catalog")
	}
	for _, value := range envList(env) {
		if strings.Contains(value, "legacy-selected") || strings.Contains(value, "ambient-github-token") || strings.Contains(value, "legacy-private-key") || strings.Contains(value, "ambient-ssh") {
			t.Fatal("legacy credentials remained in the backend environment")
		}
	}
	key := filepath.Join(filepath.Dir(env["GIT_CONFIG_GLOBAL"]), ".grasp", "git-credentials", "1.secret")
	if body, err := os.ReadFile(key); err != nil || !strings.Contains(string(body), "legacy-private-key") {
		t.Fatal("legacy SSH identity was not retained in private files")
	}
}

func worktreeCredentialFill(env map[string]string, dir, protocol, host, path string) (string, error) {
	cmd := exec.Command("git", "credential", "fill")
	cmd.Dir = dir
	cmd.Env = envList(env)
	cmd.Stdin = strings.NewReader("protocol=" + protocol + "\nhost=" + host + "\npath=" + path + "\n\n")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func envValue(env []string, key string) string {
	for _, item := range env {
		if value, ok := strings.CutPrefix(item, key+"="); ok {
			return value
		}
	}
	return ""
}

func TestLocalGitUsesPrivateHomeAndSSHSnapshot(t *testing.T) {
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(execution.EnvMode, execution.ModeLocal)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(hostHome, ".gitconfig"))
	t.Setenv("SSH_AUTH_SOCK", "/selected/ssh-agent.sock")
	t.Setenv("SSH_AGENT_PID", "123")
	t.Setenv("GITHUB_TOKEN", "unselected-github-token")
	t.Setenv("GIT_SSH_COMMAND", "unselected-ssh")
	hostSSH := filepath.Join(hostHome, ".ssh")
	if err := os.Mkdir(hostSSH, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		".gitconfig":       "[user]\nname = Host Author\nemail = host@example.com\n[alias]\nlocal-auth-probe = status\n",
		".ssh/id_ed25519":  "host-private-key",
		".ssh/known_hosts": "host-known-hosts",
		".ssh/config":      "Host local-auth-test\n HostName localhost\n IdentityFile ~/.ssh/id_ed25519\n UserKnownHostsFile ~/.ssh/known_hosts\n",
		"unrelated-secret": "must-not-be-staged",
	} {
		if err := os.WriteFile(filepath.Join(hostHome, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{}
	cleanup, err := prepareWorktreeGit(context.Background(), Spec{GitBindingsConfigured: true}, env)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	privateHome := env["HOME"]
	if privateHome == "" || privateHome == hostHome || envValue(envList(env), "HOME") != privateHome {
		t.Fatal("local Git process inherited the host HOME")
	}
	if _, err := os.Stat(filepath.Join(privateHome, "unrelated-secret")); !os.IsNotExist(err) {
		t.Fatal("local process staged unrelated host files")
	}
	for key, want := range map[string]string{"SSH_AUTH_SOCK": "/selected/ssh-agent.sock", "SSH_AGENT_PID": "123"} {
		if envValue(envList(env), key) != want {
			t.Fatalf("explicit local SSH agent was not forwarded: %s", key)
		}
	}
	if strings.Contains(strings.Join(envList(env), "\n"), "unselected-") {
		t.Fatal("local host Git inherited ambient tokens or SSH overrides")
	}
	cmd := exec.Command("git", "config", "--global", "--get", "alias.local-auth-probe")
	cmd.Env = envList(env)
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "status" {
		t.Fatal("local Git did not retain host configuration")
	}
	cmd = exec.Command("git", "var", "GIT_AUTHOR_IDENT")
	cmd.Env = envList(env)
	if out, err := cmd.Output(); err != nil || !strings.Contains(string(out), "Host Author <host@example.com>") {
		t.Fatalf("local Git did not retain the host author: %v", err)
	}
	if !strings.Contains(env["GIT_SSH_COMMAND"], filepath.Join(privateHome, ".ssh", "config")) {
		t.Fatal("local Git SSH transport did not select the private config")
	}
	// Check OpenSSH's resolved paths without making a network connection.
	sshProbe := exec.Command("sh", "-c", env["GIT_SSH_COMMAND"]+" -G local-auth-test")
	sshProbe.Env = envList(env)
	out, err := sshProbe.CombinedOutput()
	if err != nil {
		t.Fatalf("OpenSSH config probe failed: %v: %s", err, out)
	}
	resolved := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		if key == "identityfile" || key == "userknownhostsfile" {
			resolved[key] += value + "\n"
		}
	}
	if !strings.Contains(resolved["identityfile"], filepath.Join(hostSSH, "id_ed25519")) {
		t.Fatalf("OpenSSH did not use the host identity in place: %s", out)
	}
	if strings.TrimSpace(resolved["userknownhostsfile"]) != filepath.Join(privateHome, ".ssh", "known_hosts") {
		t.Fatalf("OpenSSH known_hosts can write back to the host: %s", out)
	}
	if _, err := os.Lstat(filepath.Join(privateHome, ".ssh", "id_ed25519")); !os.IsNotExist(err) {
		t.Fatal("local SSH private key was copied into the backend HOME")
	}
	privateKnownHosts := filepath.Join(privateHome, ".ssh", "known_hosts")
	if info, err := os.Lstat(privateKnownHosts); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatal("local known_hosts was not copied privately")
	}
	if err := os.WriteFile(privateKnownHosts, []byte("changed-by-ssh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(hostSSH, "known_hosts")); err != nil || string(body) != "host-known-hosts" {
		t.Fatal("local SSH snapshot changed host known_hosts")
	}
	cleanup()
	if _, err := os.Stat(privateHome); !os.IsNotExist(err) {
		t.Fatal("stopping the backend kept the local auth snapshot")
	}
}

func TestLocalProcessHomeStagesOnlySelectedProviderAuth(t *testing.T) {
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	hostData := t.TempDir()
	t.Setenv("XDG_DATA_HOME", hostData)
	if err := os.MkdirAll(filepath.Join(hostData, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostData, "opencode", "auth.json"), []byte("host-opencode-auth"), 0o600); err != nil {
		t.Fatal(err)
	}
	privateHome := t.TempDir()
	env := map[string]string{"AGENT_PROVIDER": "opencode", "CONFIG_ROOT": t.TempDir(), "GRASP_LOCAL_AUTH": "1"}
	prepareLocalProcessHome(privateHome, env, env["CONFIG_ROOT"], "")
	path := filepath.Join(env["XDG_DATA_HOME"], "opencode", "auth.json")
	if body, err := os.ReadFile(path); err != nil || string(body) != "host-opencode-auth" {
		t.Fatal("selected provider's XDG auth snapshot is missing")
	}
	if err := os.WriteFile(path, []byte("changed-by-cli"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(hostData, "opencode", "auth.json")); err != nil || string(body) != "host-opencode-auth" {
		t.Fatal("provider auth wrote back to the host")
	}
	cloudHome := t.TempDir()
	prepareLocalProcessHome(cloudHome, map[string]string{"AGENT_PROVIDER": "opencode"}, t.TempDir(), "")
	if _, err := os.Stat(filepath.Join(cloudHome, ".local", "share", "opencode", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("cloud binding received host native OpenCode auth")
	}
	otherHome := t.TempDir()
	prepareLocalProcessHome(otherHome, map[string]string{"AGENT_PROVIDER": "codex"}, t.TempDir(), "")
	if _, err := os.Stat(filepath.Join(otherHome, ".local", "share", "opencode", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("another provider received unrelated OpenCode auth")
	}
}

func TestLocalTraeHistorySurvivesBackendHomeReplacement(t *testing.T) {
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	owned := t.TempDir()
	stage := func() string {
		t.Helper()
		home := t.TempDir()
		prepareLocalProcessHome(home, map[string]string{"AGENT_PROVIDER": "trae"}, owned, owned)
		info, err := os.Lstat(filepath.Join(home, ".trae"))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("private Trae HOME did not select the owned config home")
		}
		return home
	}
	firstHome := stage()
	if err := os.WriteFile(filepath.Join(firstHome, ".trae", "native-history.json"), []byte("original-native-history"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(firstHome); err != nil {
		t.Fatal(err)
	}
	secondHome := stage()
	if body, err := os.ReadFile(filepath.Join(secondHome, ".trae", "native-history.json")); err != nil || string(body) != "original-native-history" {
		t.Fatal("backend HOME replacement lost Trae's native history")
	}
	hostTrae := filepath.Join(hostHome, ".trae")
	if err := os.Mkdir(hostTrae, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostTrae, "auth.json"), []byte("host-login"), 0o600); err != nil {
		t.Fatal(err)
	}
	unownedHome := t.TempDir()
	prepareLocalProcessHome(unownedHome, map[string]string{"AGENT_PROVIDER": "trae"}, hostTrae, hostTrae)
	info, err := os.Lstat(filepath.Join(unownedHome, ".trae"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("host Trae login directory was linked instead of copied")
	}
	if err := os.WriteFile(filepath.Join(unownedHome, ".trae", "auth.json"), []byte("changed-by-cli"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(hostTrae, "auth.json")); err != nil || string(body) != "host-login" {
		t.Fatal("Trae's staged HOME changed host auth")
	}
}

func TestLocalProcessFilesystemRootsIgnoreAgentEnvironment(t *testing.T) {
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	hostData := t.TempDir()
	t.Setenv("XDG_DATA_HOME", hostData)
	if err := os.MkdirAll(filepath.Join(hostData, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostData, "opencode", "auth.json"), []byte("native-opencode-login"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := t.TempDir()
	if err := os.WriteFile(filepath.Join(unrelated, "auth.json"), []byte("unrelated-login"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"codex", "trae", "opencode"} {
		t.Run(provider, func(t *testing.T) {
			owned := t.TempDir()
			if err := os.WriteFile(filepath.Join(owned, "auth.json"), []byte("selected-login"), 0o600); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			env := map[string]string{
				"AGENT_PROVIDER": provider, "GRASP_LOCAL_AUTH": "1",
				"CONFIG_ROOT": unrelated, "HOME": unrelated,
				"XDG_DATA_HOME": unrelated, "XDG_CONFIG_HOME": unrelated,
			}
			prepareLocalProcessHome(home, env, owned, owned)
			rel := "." + provider
			if provider == "opencode" {
				rel = ".config/opencode"
			}
			if body, err := os.ReadFile(filepath.Join(home, rel, "auth.json")); err != nil || string(body) != "selected-login" {
				t.Fatal("Agent environment replaced the server-owned CLI config source")
			}
			if provider == "opencode" {
				if body, err := os.ReadFile(filepath.Join(home, ".local", "share", "opencode", "auth.json")); err != nil || string(body) != "native-opencode-login" {
					t.Fatal("Agent environment redirected the native auth destination")
				}
			}
			if env["CONFIG_ROOT"] != owned || env["HOME"] != home || env["XDG_DATA_HOME"] != filepath.Join(home, ".local", "share") {
				t.Fatal("Agent environment replaced the private process layout")
			}
			if provider == "trae" {
				if target, err := os.Readlink(filepath.Join(home, ".trae")); err != nil || target != owned {
					t.Fatal("Trae's private link did not select the server-owned config home")
				}
			}
		})
	}
	if body, err := os.ReadFile(filepath.Join(unrelated, "auth.json")); err != nil || string(body) != "unrelated-login" {
		t.Fatal("Agent environment caused a write to an unrelated directory")
	}
	if _, err := os.Stat(filepath.Join(unrelated, "opencode")); !os.IsNotExist(err) {
		t.Fatal("Agent environment created auth directories outside the private HOME")
	}
}
