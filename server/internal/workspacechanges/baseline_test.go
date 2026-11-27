package workspacechanges

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func TestRunBaselineSurvivesIndependentClone(t *testing.T) {
	for _, tool := range []string{"git", "python3", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required by the sandbox", tool)
		}
	}
	ctx := t.Context()
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(dir, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const repoName = "仓库 [test]"
	remote, seed, first, second := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	git(remote, "init", "--bare", "-b", "main")
	git(seed, "init", "-b", "main")
	git(seed, "config", "user.name", "Baseline fixture")
	git(seed, "config", "user.email", "fixture@example.invalid")
	write(seed, "original run checkout\n")
	git(seed, "add", "README.md")
	git(seed, "commit", "-m", "baseline")
	firstHead := git(seed, "rev-parse", "HEAD")
	git(seed, "push", remote, "HEAD:refs/heads/main")
	git(first, "clone", "--no-local", remote, repoName)
	defer sandbox.SetExecHook(func(ctx context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Env, cmd.Stdin = env, stdin
		return cmd.CombinedOutput()
	})()

	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	scope := Scope{Kind: models.WorkspaceChangesRun, OwnerID: "run-baseline", Workspace: "sandbox-a", NodeID: "build",
		Repos: []models.SessionRepo{{RepoDirName: repoName}}}
	if err := db.Create(&models.Run{ID: scope.OwnerID, Repos: scope.Repos, WorkspaceChangesEnabled: true, StartedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	service := New(db, 0)
	service.Initialize(ctx, &sandbox.Sandbox{WorkspaceDir: first}, scope)
	var baseline models.WorkspaceChangeBaseline
	if err := service.scoped(db, scope).Take(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	tree := baseline.Trees[repoName]
	if tree.Head != firstHead || tree.Ref == firstHead || tree.Pack == "" || baseline.ArchiveID == "" {
		t.Fatalf("initial tree not archived: %+v", tree)
	}

	git(seed, "checkout", "--orphan", "replacement")
	write(seed, "unrelated remote checkout\n")
	git(seed, "add", "README.md")
	git(seed, "commit", "-m", "replacement")
	git(seed, "push", "--force", remote, "HEAD:refs/heads/main")
	git(second, "clone", "--no-local", remote, repoName)
	for _, folder := range []string{remote, first, seed} {
		if err := os.RemoveAll(folder); err != nil {
			t.Fatal(err)
		}
	}
	check := exec.CommandContext(ctx, "git", "-C", filepath.Join(second, repoName), "cat-file", "-e", tree.Ref)
	check.Env = env
	if err := check.Run(); err == nil {
		t.Fatal("new clone unexpectedly contains the original tree")
	}
	scope.Workspace, scope.NodeID = "sandbox-b", "verify"
	sb := &sandbox.Sandbox{WorkspaceDir: second}
	service.Initialize(ctx, sb, scope)
	before := service.Begin(ctx, sb, scope)
	repo := filepath.Join(second, repoName)
	indexPath := filepath.Join(repo, ".git", "index")
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	write(repo, "new work from the second sandbox\n")
	archived, err := service.record(ctx, sb, scope, "turn-b", before, false)
	if err != nil || archived == nil {
		t.Fatalf("cumulative archive: %+v (%v)", archived, err)
	}
	if repo := archived.Repos[0]; repo.BaseSHA != firstHead || repo.From != tree.Ref || repo.FileCount != 1 {
		t.Fatalf("comparison used the wrong tree: %+v", repo)
	}
	turn, err := service.List(ctx, scope, "", "turn-b", true)
	if err != nil || turn.Snapshot == nil {
		t.Fatalf("turn archive: %+v (%v)", turn, err)
	}
	for _, check := range []struct {
		snapshot *models.WorkspaceChangeSnapshot
		before   string
	}{{archived, "original run checkout\n"}, {turn.Snapshot, "unrelated remote checkout\n"}} {
		snap := check.snapshot
		file, err := service.File(ctx, scope.Kind, scope.OwnerID, snap.ID, snap.Repos[0].Files[0].ID)
		if err != nil || file.Before == nil || file.After == nil || *file.Before != check.before || *file.After != "new work from the second sandbox\n" {
			t.Fatalf("incorrect archived versions: %+v (%v)", file, err)
		}
	}
	if indexAfter, err := os.ReadFile(indexPath); err != nil || !bytes.Equal(indexBefore, indexAfter) {
		t.Fatalf("restoring the baseline changed the real index: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return DeleteOwner(tx, scope.Kind, scope.OwnerID) }); err != nil {
		t.Fatal(err)
	}
	if err := service.collectArchives(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, tree.Pack); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner deletion left the baseline pack: %v", err)
	}
}
