package execution

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPrepareSharesLaunchAndDeletesOnEnd(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	t.Cleanup(ResetForTest)

	repo := initRepo(t)
	feature := gitOut(t, repo, "rev-parse", "refs/heads/feature")
	src := []Source{{Name: "app", URL: repo, Branch: "feature"}}

	first, err := Prepare("run-a", src)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Prepare("run-a", src)
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root {
		t.Fatalf("same launch diverged: %s %s", first.Root, second.Root)
	}
	if len(first.Repos) != 1 || first.Repos[0].Kind != "worktree" {
		t.Fatalf("repos=%+v", first.Repos)
	}
	if gitOut(t, first.Repos[0].Path, "rev-parse", "HEAD") != feature {
		t.Fatal("worktree is not at the selected branch HEAD")
	}
	if err := exec.Command("git", "-C", first.Repos[0].Path, "symbolic-ref", "-q", "HEAD").Run(); err == nil {
		t.Fatal("worktree is still on a branch")
	}
	other, err := Prepare("run-b", src)
	if err != nil {
		t.Fatal(err)
	}
	if other.Root == first.Root {
		t.Fatal("parallel launches shared a directory")
	}
	if _, err := os.Stat(first.Root); err != nil {
		t.Fatal("directory removed while the launch is in progress")
	}
	Release("run-a", false)
	if _, err := os.Stat(first.Root); err != nil {
		t.Fatal("releasing one node removed the shared directory")
	}
	End("run-a")
	if _, err := os.Stat(first.Root); !os.IsNotExist(err) {
		t.Fatalf("ended launch still present: %v", err)
	}
	if _, err := os.Stat(other.Root); err != nil {
		t.Fatal("ending one launch removed the other")
	}
	mainHead := gitOut(t, repo, "rev-parse", "HEAD")
	if mainHead == "" {
		t.Fatal("source repo was damaged")
	}
	End("run-b")
	if _, err := Prepare("run-a", src); err == nil {
		t.Fatal("ended launch was recreated")
	}
}

func TestPrepareFolderWhenNotGit(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "note.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Prepare("solo-1", []Source{{Name: "notes", URL: src}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 1 || got.Repos[0].Kind != "folder" {
		t.Fatalf("repos=%+v", got.Repos)
	}
	if exec.Command("git", "-C", got.Repos[0].Path, "rev-parse", "--is-inside-work-tree").Run() == nil {
		t.Fatal("plain folder was turned into a worktree")
	}
	body, err := os.ReadFile(filepath.Join(got.Repos[0].Path, "note.txt"))
	if err != nil || string(body) != "keep" {
		t.Fatalf("copy=%q err=%v", body, err)
	}
	if err := os.WriteFile(filepath.Join(got.Repos[0].Path, "note.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(filepath.Join(src, "note.txt"))
	if err != nil || string(orig) != "keep" {
		t.Fatalf("source changed: %q %v", orig, err)
	}
	Release("solo-1", true)
	if _, err := os.Stat(got.Root); !os.IsNotExist(err) {
		t.Fatal("solo directory survived release")
	}
}

func TestParallelLaunchesDiffer(t *testing.T) {
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	ResetForTest()
	var wg sync.WaitGroup
	roots := make([]string, 2)
	errs := make([]error, 2)
	for i, id := range []string{"p-1", "p-2"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			got, err := Prepare(id, nil)
			errs[i] = err
			if err == nil {
				roots[i] = got.Root
			}
		}(i, id)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if roots[0] == "" || roots[0] == roots[1] {
		t.Fatalf("roots=%v", roots)
	}
	End("p-1")
	End("p-2")
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-m", "main")
	gitRun(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "feature")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
