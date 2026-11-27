package sandbox

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceTreesPreserveIndexAndPreview(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required by the sandbox image")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "review")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		result, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, result)
		}
		return strings.TrimSpace(string(result))
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, path), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Unix(1_700_000_000, 0)
	setMtime := func(path string) {
		t.Helper()
		if err := os.Chtimes(filepath.Join(repo, path), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "test@example.invalid")
	git("config", "user.name", "Review Fixture")
	git("config", "core.trustctime", "false")
	write(".gitignore", "*.scratch\n")
	write("README.md", "# Start\n")
	write("old file.txt", "same content\n")
	write("removed.md", "# Removed\n")
	write("00-large.txt", strings.Repeat("a", 512*1024+1))
	write("tab\tline\n.txt", "old\n")
	setMtime("00-large.txt")
	setMtime("tab\tline\n.txt")
	git("add", ".")
	git("commit", "-m", "base")
	initial := git("rev-parse", "HEAD")
	defer SetExecHook(func(ctx context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "python3", "-")
		cmd.Stdin = stdin
		return cmd.CombinedOutput()
	})()
	sb := &Sandbox{WorkspaceDir: root}
	capture := func() WorkspaceTree {
		t.Helper()
		idx := filepath.Join(repo, ".git", "index")
		before, err := os.ReadFile(idx)
		if err != nil {
			t.Fatal(err)
		}
		trees, err := sb.CaptureWorkspaceTrees(ctx, []WorkspaceTreeQuery{{Directory: "review"}})
		if err != nil || trees[0].Unavailable || trees[0].Ref == "" {
			t.Fatalf("capture: %+v %v", trees, err)
		}
		after, err := os.ReadFile(idx)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("real Git index was modified")
		}
		return trees[0]
	}
	base := capture()
	if base.Ref != initial || base.Branch != "main" {
		t.Fatalf("clean baseline not portable: %+v", base)
	}
	write("README.md", "# Start\n\nBefore this turn.\n")
	write("tracked.scratch", "forced tracked file\n")
	git("add", "-f", "tracked.scratch")
	turn := capture()
	write("README.md", "# Review\n\nBefore this turn.\nOne new line.\n")
	git("commit", "-m", "during turn", "--", "README.md")
	if err := os.Rename(filepath.Join(repo, "old file.txt"), filepath.Join(repo, "新名字.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "removed.md")); err != nil {
		t.Fatal(err)
	}
	write("untracked.txt", "new file\n")
	write("ignored.scratch", "excluded\n")
	write("image.bin", "\x00\x01")
	write("00-large.txt", strings.Repeat("b", 512*1024+1))
	write("tab\tline\n.txt", "new\n")
	// The unchanged size and mtime match the cached entries. Git relies on
	// the index's own timestamp to recheck the file contents in this case.
	setMtime("00-large.txt")
	setMtime("tab\tline\n.txt")
	setMtime(".git/index")
	end := capture()
	write("README.md", "# Later edit\n")
	reports, err := sb.CompareWorkspaceTrees(ctx, []WorkspaceTreeQuery{
		{Directory: "review", From: base.Ref, To: end.Ref},
		{Directory: "review", From: turn.Ref, To: end.Ref},
		{Directory: "..", From: turn.Ref, To: end.Ref},
	})
	if err != nil || reports[0].Unavailable || reports[1].Unavailable || !reports[2].Unavailable {
		t.Fatalf("comparison %v, error %v", reports, err)
	}
	if reports[0].FileCount != 8 || reports[1].FileCount != 7 || reports[0].Added-reports[1].Added != 3 {
		t.Fatalf("incorrect cumulative and turn counts: %+v", reports)
	}
	files := map[string]WorkspaceTreeFile{}
	for _, file := range reports[1].Files {
		files[file.Path] = file
	}
	if readme := files["README.md"]; readme.After == nil || readme.Patch == nil ||
		strings.Contains(string(*readme.After), "Later edit") || !strings.Contains(string(*readme.After), "One new line.") {
		t.Fatalf("inconsistent preview: %+v", readme)
	}
	if removed := files["removed.md"]; removed.Before == nil || string(*removed.Before) != "# Removed\n" || removed.After != nil {
		t.Fatalf("missing deleted file version: %+v", removed)
	}
	if rename := files["新名字.txt"]; rename.Status != "R" || rename.PreviousPath != "old file.txt" || rename.Added == nil || *rename.Added != 0 {
		t.Fatalf("rename not preserved: %+v", rename)
	}
	if binary := files["image.bin"]; binary.Added != nil || binary.After != nil {
		t.Fatalf("binary file advertised a line count/preview: %+v", binary)
	}
	if large := files["00-large.txt"]; large.Added == nil || *large.Added != 1 || large.Patch != nil || large.After != nil || large.Before != nil {
		t.Fatalf("oversized file missing its metadata or included an oversized payload: %+v", large)
	}
	if unusual := files["tab\tline\n.txt"]; unusual.Before == nil || unusual.After == nil || string(*unusual.Before) != "old\n" || string(*unusual.After) != "new\n" {
		t.Fatalf("could not read filename containing separators: %+v", unusual)
	}
}
