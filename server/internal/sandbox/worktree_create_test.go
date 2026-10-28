package sandbox

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/execution"
)

func TestWorktreeCreateSkipsGateway(t *testing.T) {
	useWorktreeGitHelper(t)
	root := t.TempDir()
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", root)
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(name string) error {
		if name == "codex" || name == "claude" {
			return nil
		}
		return os.ErrNotExist
	}))
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		return 9, func() {}, nil
	}))

	repo := initWorktreeRepo(t)
	sha := gitWorktreeOut(t, repo, "rev-parse", "HEAD")
	m := NewManager(nil, ManagerOptions{})
	spec := Spec{
		Name: "node-a",
		Env: map[string]string{
			BridgePasswordEnv: "pw",
			"AGENT_PROVIDER":  "codex",
			"GRASP_RUN_ID":    "run-1",
			envLaunchKind:     execution.LaunchRun,
			"GIT_REPOS":       EncodeRepos([]RepoSpec{{Name: "app", URL: repo, Branch: "main"}}),
		},
	}
	sb, err := m.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if sb.Host != "127.0.0.1" || sb.WorkspaceDir == "" {
		t.Fatalf("sandbox=%+v", sb)
	}
	if gitWorktreeOut(t, filepath.Join(sb.WorkspaceDir, "app"), "rev-parse", "HEAD") != sha {
		t.Fatal("detached worktree is not at the branch HEAD")
	}
	again, err := m.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if again.WorkspaceDir != sb.WorkspaceDir {
		t.Fatal("nodes of one run did not share the directory")
	}
	other, err := m.Create(context.Background(), Spec{
		Name: "node-b",
		Env: map[string]string{
			BridgePasswordEnv: "pw",
			"AGENT_PROVIDER":  "codex",
			"GRASP_RUN_ID":    "run-2",
			envLaunchKind:     execution.LaunchRun,
			"GIT_REPOS":       spec.Env["GIT_REPOS"],
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if other.WorkspaceDir == sb.WorkspaceDir {
		t.Fatal("parallel runs shared a directory")
	}
	if err := sb.WriteFile(context.Background(), "app/note.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	body, err := again.ReadFile(context.Background(), "app/note.txt")
	if err != nil || string(body) != "hi" {
		t.Fatalf("read=%q err=%v", body, err)
	}
	sb.Destroy(context.Background())
	if _, err := os.Stat(sb.WorkspaceDir); err != nil {
		t.Fatal("run directory was deleted before the run ended")
	}
	execution.End("run-1")
	if _, err := os.Stat(sb.WorkspaceDir); !os.IsNotExist(err) {
		t.Fatal("run directory survived the end of the run")
	}
	if _, err := os.Stat(other.WorkspaceDir); err != nil {
		t.Fatal("the other run was deleted")
	}
	execution.End("run-2")
}

func TestWorktreeMissingCLIDoesNotCreateContainer(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return os.ErrNotExist }))
	called := false
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		called = true
		return 1, func() {}, nil
	}))
	m := NewManager(nil, ManagerOptions{})
	_, err := m.Create(context.Background(), Spec{Name: "missing", Env: map[string]string{
		BridgePasswordEnv: "pw",
		"AGENT_PROVIDER":  "claude_code",
		"GRASP_RUN_ID":    "run-missing",
		envLaunchKind:     execution.LaunchSolo,
	}})
	if err == nil || !strings.Contains(err.Error(), "claude") || !strings.Contains(err.Error(), "不会改用沙箱容器") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("backend started without a CLI")
	}
	entries, _ := os.ReadDir(os.Getenv("GRASP_WORKTREE_ROOT"))
	if len(entries) != 0 {
		t.Fatalf("launch directory created on failure: %v", entries)
	}
}

func TestWorktreeFolderAndSoloDelete(t *testing.T) {
	useWorktreeGitHelper(t)
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		return 3, func() {}, nil
	}))
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "plain.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, ManagerOptions{})
	sb, err := m.Create(context.Background(), Spec{Name: "solo", Env: map[string]string{
		BridgePasswordEnv: "pw",
		"AGENT_PROVIDER":  "opencode",
		"GRASP_RUN_ID":    "solo-9",
		envLaunchKind:     execution.LaunchSolo,
		"GIT_REPOS":       EncodeRepos([]RepoSpec{{Name: "docs", URL: src}}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sb.WorkspaceDir, "docs", "plain.txt")); err != nil {
		t.Fatal(err)
	}
	if exec.Command("git", "-C", filepath.Join(sb.WorkspaceDir, "docs"), "rev-parse", "--git-dir").Run() == nil {
		t.Fatal("plain folder became a git worktree")
	}
	sb.Destroy(context.Background())
	if _, err := os.Stat(sb.WorkspaceDir); !os.IsNotExist(err) {
		t.Fatal("solo directory was kept after the session ended")
	}
}

func TestWorktreePrivateRepoUsesSpecCredential(t *testing.T) {
	root := t.TempDir()
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", root)
	isolateWorktreeGit(t)
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	started := false
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		started = true
		return 9, func() {}, nil
	}))
	bare := bareWorktreeRepo(t)
	feature := gitWorktreeOut(t, bare, "rev-parse", "refs/heads/feature")
	const token = "secret-token"
	srv := authGitServer(t, bare, token)
	cloneURL := srv.URL + "/repo.git"
	m := NewManager(nil, ManagerOptions{})
	spec := Spec{
		Name:                  "priv",
		GitBindingsConfigured: true,
		GitCredentials: []GitCredential{{
			ID: "gh", Kind: RepoCredentialGitHubHTTPS, Scope: cloneURL, Secret: token,
		}},
		Env: map[string]string{
			BridgePasswordEnv: "pw",
			"AGENT_PROVIDER":  "codex",
			"GRASP_RUN_ID":    "run-priv",
			envLaunchKind:     execution.LaunchSolo,
			"GIT_REPOS":       EncodeRepos([]RepoSpec{{Name: "app", URL: cloneURL, Branch: "feature"}}),
			"GITHUB_TOKEN":    "wrong-token",
		},
	}
	sb, err := m.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("backend did not start")
	}
	path := filepath.Join(sb.WorkspaceDir, "app")
	if gitWorktreeOut(t, path, "rev-parse", "HEAD") != feature {
		t.Fatal("detached worktree is not at the selected branch HEAD")
	}
	if exec.Command("git", "-C", path, "symbolic-ref", "-q", "HEAD").Run() == nil {
		t.Fatal("worktree is still on a branch")
	}
	sb.Destroy(context.Background())
	if _, err := os.Stat(sb.WorkspaceDir); !os.IsNotExist(err) {
		t.Fatal("solo directory survived")
	}
}

func TestWorktreeRepoCredentialOverridesCatalog(t *testing.T) {
	root := t.TempDir()
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", root)
	isolateWorktreeGit(t)
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		return 9, func() {}, nil
	}))
	bare := bareWorktreeRepo(t)
	feature := gitWorktreeOut(t, bare, "rev-parse", "refs/heads/feature")
	const token = "repo-token"
	srv := authGitServer(t, bare, token)
	cloneURL := srv.URL + "/repo.git"
	m := NewManager(nil, ManagerOptions{})
	sb, err := m.Create(context.Background(), Spec{
		Name:                  "repo-cred",
		GitBindingsConfigured: true,
		GitCredentials: []GitCredential{
			{ID: "wrong", Kind: RepoCredentialGitHubHTTPS, Scope: srv.URL, Secret: "catalog-token"},
			{ID: "repo", Kind: RepoCredentialGitHubHTTPS, Scope: cloneURL, Secret: token},
		},
		Env: map[string]string{
			BridgePasswordEnv: "pw",
			"AGENT_PROVIDER":  "codex",
			"GRASP_RUN_ID":    "run-repo",
			envLaunchKind:     execution.LaunchRun,
			"GIT_REPOS":       EncodeRepos([]RepoSpec{{Name: "app", URL: cloneURL, Branch: "feature"}}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gitWorktreeOut(t, filepath.Join(sb.WorkspaceDir, "app"), "rev-parse", "HEAD") != feature {
		t.Fatal("repo credential was not used")
	}
	sb.Destroy(context.Background())
	execution.End("run-repo")
}

func TestWorktreeMissingCredentialDoesNotCreateContainer(t *testing.T) {
	root := t.TempDir()
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", root)
	isolateWorktreeGit(t)
	askpassDir := t.TempDir()
	askpassCalled := filepath.Join(askpassDir, "called")
	askpass := filepath.Join(askpassDir, "askpass")
	script := "#!/bin/sh\nprintf called >> " + quoteWorktreeGit(askpassCalled) + "\ncase \"$1\" in *Username*) printf x-access-token ;; *) printf secret-token ;; esac\n"
	if err := os.WriteFile(askpass, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", askpass)
	t.Setenv("SSH_ASKPASS", askpass)
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	started := false
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		started = true
		return 9, func() {}, nil
	}))
	bare := bareWorktreeRepo(t)
	srv := authGitServer(t, bare, "secret-token")
	cloneURL := srv.URL + "/repo.git"
	m := NewManager(nil, ManagerOptions{})
	_, err := m.Create(context.Background(), Spec{
		Name:                  "no-cred",
		GitBindingsConfigured: true,
		Env: map[string]string{
			BridgePasswordEnv: "pw",
			"AGENT_PROVIDER":  "codex",
			"GRASP_RUN_ID":    "run-nocred",
			envLaunchKind:     execution.LaunchSolo,
			"GIT_REPOS":       EncodeRepos([]RepoSpec{{Name: "app", URL: cloneURL, Branch: "feature"}}),
			"GITHUB_TOKEN":    "secret-token",
		},
	})
	if err == nil || strings.Contains(err.Error(), "gateway") {
		t.Fatalf("err=%v", err)
	}
	if started {
		t.Fatal("backend started without credentials")
	}
	if _, err := os.Stat(askpassCalled); !os.IsNotExist(err) {
		t.Fatal("an empty selection consulted the host's Git askpass identity")
	}
	if _, statErr := os.Stat(filepath.Join(root, "run-nocred")); !os.IsNotExist(statErr) {
		t.Fatal("launch directory created on failure")
	}
}

func TestSandboxModeStillRequiresGateway(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeSandbox)
	m := NewManager(nil, ManagerOptions{})
	_, err := m.Create(context.Background(), Spec{Env: map[string]string{BridgePasswordEnv: "pw"}})
	if err == nil || !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("err=%v", err)
	}
}

func authGitServer(t *testing.T, bare, token string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "x-access-token" || pass != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		serveWorktreeGit(w, r, filepath.Dir(bare))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serveWorktreeGit(w http.ResponseWriter, r *http.Request, projectRoot string) {
	cmd := exec.Command("git", "http-backend")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GIT_PROJECT_ROOT=" + projectRoot,
		"GIT_HTTP_EXPORT_ALL=1",
		"REQUEST_METHOD=" + r.Method,
		"PATH_INFO=" + r.URL.Path,
		"QUERY_STRING=" + r.URL.RawQuery,
		"CONTENT_TYPE=" + r.Header.Get("Content-Type"),
		"CONTENT_LENGTH=" + strconv.FormatInt(r.ContentLength, 10),
	}
	cmd.Stdin = r.Body
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		http.Error(w, "git http-backend", http.StatusBadGateway)
		return
	}
	raw := stdout.Bytes()
	head, body, ok := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !ok {
		head, body, ok = bytes.Cut(raw, []byte("\n\n"))
	}
	if !ok {
		http.Error(w, "bad cgi", http.StatusBadGateway)
		return
	}
	status := http.StatusOK
	for _, line := range bytes.Split(head, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		key, val, found := bytes.Cut(line, []byte(":"))
		if !found {
			continue
		}
		name := string(key)
		value := strings.TrimSpace(string(val))
		if strings.EqualFold(name, "Status") {
			fields := strings.Fields(value)
			if len(fields) > 0 {
				if code, err := strconv.Atoi(fields[0]); err == nil {
					status = code
				}
			}
			continue
		}
		w.Header().Add(name, value)
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewReader(body))
}

func bareWorktreeRepo(t *testing.T) string {
	t.Helper()
	src := initWorktreeRepo(t)
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitWorktreeRun(t, src, "checkout", "-b", "feature")
	gitWorktreeRun(t, src, "commit", "-am", "feature")
	bare := filepath.Join(t.TempDir(), "repo.git")
	cmd := exec.Command("git", "clone", "--bare", src, bare)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bare clone: %v: %s", err, out)
	}
	gitWorktreeRun(t, bare, "update-server-info")
	return bare
}

func isolateWorktreeGit(t *testing.T) {
	t.Helper()
	useWorktreeGitHelper(t)
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "none"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(dir, "none-system"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_ASKPASS", "/bin/false")
	t.Setenv("SSH_ASKPASS", "/bin/false")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func initWorktreeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitWorktreeRun(t, dir, "init", "-b", "main")
	gitWorktreeRun(t, dir, "config", "user.email", "t@example.com")
	gitWorktreeRun(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitWorktreeRun(t, dir, "add", "a.txt")
	gitWorktreeRun(t, dir, "commit", "-m", "main")
	return dir
}

func gitWorktreeRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitWorktreeOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
