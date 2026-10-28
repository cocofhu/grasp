package execution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellDirUsesSingleRepo(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRASP_WORKTREE_ROOT", root)
	launch := filepath.Join(root, "run-1")
	if err := os.MkdirAll(filepath.Join(launch, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ShellDir("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(launch, "demo") {
		t.Fatalf("shell dir = %s", got)
	}
}

func TestShellDirStaysAtRootWithSeveralRepos(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRASP_WORKTREE_ROOT", root)
	launch := filepath.Join(root, "run-2")
	for _, name := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(launch, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ShellDir("run-2")
	if err != nil {
		t.Fatal(err)
	}
	if got != launch {
		t.Fatalf("shell dir = %s", got)
	}
}
