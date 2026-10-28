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
	useWorktreeGitHelper(t)
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
	useWorktreeGitHelper(t)
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

func TestWorktreeReprimeRetainsSelectedGitCredentialsAndCleansReplacedCatalogs(t *testing.T) {
	useWorktreeGitHelper(t)
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	t.Setenv("GH_TOKEN", "ambient-git-token")
	t.Setenv("OPENAI_API_KEY", "ambient-model-key")
	t.Setenv("GRASP_ARTIFACT_TOKEN", "ambient-task-token")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "!printf 'username=ambient\\npassword=ambient-git-token\\n'")
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	var environments []map[string]string
	stopped := map[int]int{}
	failNext := false
	t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, env map[string]string, _ string) (int, func(), error) {
		index := len(environments)
		environments = append(environments, env)
		if failNext {
			failNext = false
			return 0, nil, errors.New("replacement failed")
		}
		return 17000 + index, func() { stopped[index]++ }, nil
	}))
	makeSpec := func(backend, selected string) Spec {
		return Spec{Name: "git-reprime", ConfigRoot: "/root/.codex", ConfigHome: t.TempDir(), GitBindingsConfigured: true,
			GitCredentials: []GitCredential{{ID: selected, Kind: RepoCredentialGitHubHTTPS, Scope: "https://github.com/acme/app", Secret: selected}},
			Env: map[string]string{BridgePasswordEnv: "pw", "AGENT_PROVIDER": backend, envRunID: "git-reprime", envLaunchKind: execution.LaunchSolo,
				"GH_TOKEN": "spec-unselected-token", "GIT_SSH_COMMAND": "unselected-ssh", "GIT_CONFIG_PARAMETERS": "unselected-config",
				"GRASP_MODEL_API_KEY": "selected-model-key", "GRASP_ARTIFACT_TOKEN": "selected-task-token"}}
	}
	manager := NewManager(nil, ManagerOptions{})
	first, next := makeSpec("codex", "first-selected"), makeSpec("claude_code", "second-selected")
	sb, err := manager.Create(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()) })
	workspace := sb.WorkspaceDir
	if err := sb.WriteFile(context.Background(), "progress.txt", []byte("keep task progress")); err != nil {
		t.Fatal(err)
	}
	checkSelected := func(want string) {
		t.Helper()
		out, err := sb.ExecScript(context.Background(), 0, "sh", "printf 'protocol=https\\nhost=github.com\\npath=acme/app.git\\n\\n' | git credential fill")
		if err != nil || !strings.Contains(out, "password="+want) || strings.Contains(out, "ambient-") {
			t.Fatal("local data plane did not retain the currently selected Git account")
		}
		if sb.WorkspaceDir != workspace {
			t.Fatal("tool replacement moved the workspace")
		}
		if body, err := sb.ReadFile(context.Background(), "progress.txt"); err != nil || string(body) != "keep task progress" {
			t.Fatal("tool replacement lost task progress")
		}
	}
	checkSelected("first-selected")
	if err := manager.Reprime(context.Background(), sb, next, nil); err != nil {
		t.Fatal(err)
	}
	checkSelected("second-selected")
	if _, err := os.Stat(environments[0]["GIT_CONFIG_GLOBAL"]); !os.IsNotExist(err) {
		t.Fatal("replaced backend kept its private credential files")
	}
	failNext = true
	if err := manager.Reprime(context.Background(), sb, makeSpec("codex", "failed-selected"), nil); err == nil {
		t.Fatal("failed replacement was accepted")
	}
	checkSelected("second-selected")
	if _, err := os.Stat(environments[2]["GIT_CONFIG_GLOBAL"]); !os.IsNotExist(err) {
		t.Fatal("failed replacement leaked its credential files")
	}
	// A higher-level readiness failure restores the earlier route through the
	// same re-prime method. Its newly installed catalog must become authoritative.
	if err := manager.Reprime(context.Background(), sb, first, nil); err != nil {
		t.Fatal(err)
	}
	checkSelected("first-selected")
	if _, err := os.Stat(environments[1]["GIT_CONFIG_GLOBAL"]); !os.IsNotExist(err) {
		t.Fatal("rollback left the superseded account installed")
	}
	for _, env := range environments {
		if env["GH_TOKEN"] != "" || env["GIT_SSH_COMMAND"] == "unselected-ssh" || env["GIT_CONFIG_PARAMETERS"] != "" {
			t.Fatal("a re-prime inherited an unselected Git authorization")
		}
		wire := strings.Join(envList(env), "\n")
		if strings.Contains(wire, "ambient-") || strings.Contains(wire, "spec-unselected-token") || !strings.Contains(wire, "GRASP_MODEL_API_KEY=selected-model-key") || !strings.Contains(wire, "GRASP_ARTIFACT_TOKEN=selected-task-token") {
			t.Fatal("selected model/task environment was lost or host authentication leaked")
		}
	}
	execution.End("git-reprime")
	if _, err := os.Stat(environments[3]["GIT_CONFIG_GLOBAL"]); !os.IsNotExist(err) {
		t.Fatal("ending the launch kept the active catalog")
	}
	if stopped[0] != 1 || stopped[1] != 1 || stopped[3] != 1 {
		t.Fatalf("backend shutdown did not follow replacements: %+v", stopped)
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
