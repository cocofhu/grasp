package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/execution"
)

func TestWorktreeCreateSkipsGateway(t *testing.T) {
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

func TestSandboxModeStillRequiresGateway(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeSandbox)
	m := NewManager(nil, ManagerOptions{})
	_, err := m.Create(context.Background(), Spec{Env: map[string]string{BridgePasswordEnv: "pw"}})
	if err == nil || !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("err=%v", err)
	}
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
