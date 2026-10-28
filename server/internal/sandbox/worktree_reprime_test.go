package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/execution"
)

func TestWorktreeReprimeKeepsDirectoryAndStopsLatestBackend(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	var workspaces []string
	var environments []map[string]string
	var stopped []int
	t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, env map[string]string, workspace string) (int, func(), error) {
		index := len(stopped)
		stopped = append(stopped, 0)
		workspaces = append(workspaces, workspace)
		environments = append(environments, env)
		return 15000 + index, func() { stopped[index]++ }, nil
	}))
	repo := initWorktreeRepo(t)
	manager := NewManager(nil, ManagerOptions{})
	firstHome, nextHome := t.TempDir(), t.TempDir()
	source := Spec{Name: "local-solo", ConfigRoot: "/root/.codex", ConfigHome: firstHome, Env: map[string]string{
		BridgePasswordEnv: "bridge-pw", "AGENT_PROVIDER": "codex", "GRASP_RUN_ID": "same-local-task", envLaunchKind: execution.LaunchSolo,
		"GIT_REPOS": EncodeRepos([]RepoSpec{{Name: "app", URL: repo, Branch: "main"}}),
	}}
	sb, err := manager.Create(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()) })
	workspace := sb.WorkspaceDir
	if err := sb.WriteFile(context.Background(), "app/a.txt", []byte("uncommitted work")); err != nil {
		t.Fatal(err)
	}
	next := Spec{Name: source.Name, ConfigRoot: "/root/.claude", ConfigHome: nextHome, Env: map[string]string{
		BridgePasswordEnv: "bridge-pw", "AGENT_PROVIDER": "claude_code", "GRASP_RUN_ID": "same-local-task", envLaunchKind: execution.LaunchSolo,
	}}
	if err := manager.Reprime(context.Background(), sb, next, []string{"CODEX_HOME"}); err != nil {
		t.Fatal(err)
	}
	if sb.WorkspaceDir != workspace || len(workspaces) != 2 || workspaces[1] != workspace || sb.Port != 15001 || sb.ConfigRoot != nextHome {
		t.Fatalf("switch lost local identity: sb=%+v dirs=%v", sb, workspaces)
	}
	if stopped[0] != 1 || stopped[1] != 0 {
		t.Fatalf("wrong processes stopped: %v", stopped)
	}
	if environments[1]["CONFIG_ROOT"] != nextHome || environments[1]["WORKSPACE_DIR"] != workspace || environments[1]["CODEX_HOME"] != "" {
		t.Fatalf("target tool settings wrong: %+v", environments[1])
	}
	body, err := os.ReadFile(filepath.Join(workspace, "app", "a.txt"))
	if err != nil || string(body) != "uncommitted work" {
		t.Fatalf("lost edits: %q %v", body, err)
	}
	if gitWorktreeOut(t, filepath.Join(workspace, "app"), "status", "--porcelain") == "" {
		t.Fatal("uncommitted changes were reset")
	}
	// The launch owns the latest process after a switch, including engine End.
	execution.End("same-local-task")
	if stopped[1] != 1 {
		t.Fatalf("latest backend survived launch end: %v", stopped)
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ended worktree remains: %v", err)
	}
	sb.Destroy(context.Background())
	if stopped[0] != 1 || stopped[1] != 1 {
		t.Fatalf("shutdown was not idempotent: %v", stopped)
	}
}

func TestWorktreeReprimeFailedStartLeavesOriginalToolAndFiles(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	starts, stops := 0, 0
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		starts++
		if starts > 1 {
			return 0, nil, errors.New("new backend cannot start")
		}
		return 15500, func() { stops++ }, nil
	}))
	m := NewManager(nil, ManagerOptions{})
	spec := Spec{Name: "failed-switch", ConfigRoot: "/root/.codex", ConfigHome: t.TempDir(), Env: map[string]string{
		BridgePasswordEnv: "pw", "AGENT_PROVIDER": "codex", "GRASP_RUN_ID": "failed-switch", envLaunchKind: execution.LaunchSolo,
	}}
	sb, err := m.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()) })
	if err := sb.WriteFile(context.Background(), "progress.txt", []byte("keep progress")); err != nil {
		t.Fatal(err)
	}
	if err := m.Reprime(context.Background(), sb, spec, nil); err == nil {
		t.Fatal("expected startup failure")
	}
	if stops != 0 || sb.Port != 15500 {
		t.Fatal("failed replacement stopped old backend")
	}
	if body, err := sb.ReadFile(context.Background(), "progress.txt"); err != nil || string(body) != "keep progress" {
		t.Fatalf("lost task: %q %v", body, err)
	}
}

func TestWorktreeBackendDoesNotInheritHostAuthentication(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "ambient-unselected-key")
	t.Setenv("GRASP_MODEL_API_KEY", "ambient-generic-key")
	t.Setenv("GRASP_ARTIFACT_TOKEN", "ambient-other-task-token")
	env := strings.Join(envList(map[string]string{"GRASP_MODEL_API_KEY": "explicit-selected-key", "GRASP_ARTIFACT_TOKEN": "explicit-task-token"}), "\n")
	if strings.Contains(env, "ambient-") || !strings.Contains(env, "GRASP_MODEL_API_KEY=explicit-selected-key") || !strings.Contains(env, "GRASP_ARTIFACT_TOKEN=explicit-task-token") {
		t.Fatal("worktree backend inherited an unselected identity")
	}
}
