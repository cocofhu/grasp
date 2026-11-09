package workspacegit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func nativeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, b, err)
	}
	return strings.TrimSpace(string(b))
}
func fixtureRepo(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, dir, "add", ".")
	nativeGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "first")
	return dir
}

func TestCodeTransferPreservesSourcesAndDirtyTargetAndImportsUnpushedCommits(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	first := fixtureRepo(t, source, "one")
	second := fixtureRepo(t, source, "two")
	old := nativeGit(t, first, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(first, "tracked.txt"), []byte("new committed change"), 0600); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, first, "add", ".")
	nativeGit(t, first, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "unpublished")
	sha := nativeGit(t, first, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(first, "tracked.txt"), []byte("source dirty data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "untracked.bin"), []byte{0, 1, 2, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing-dirty.txt"), []byte("target unsaved"), 0600); err != nil {
		t.Fatal(err)
	}
	before := nativeGit(t, first, "show-ref")
	indexBefore, err := os.ReadFile(filepath.Join(first, ".git/index"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Root: source}
	inventory, err := s.Inventory(context.Background())
	if err != nil || len(inventory.Repositories) != 2 || inventory.WorkingChanges || inventory.NonGit {
		t.Fatalf("wrong capability inventory: %+v %v", inventory, err)
	}
	var archive bytes.Buffer
	manifest, err := s.Export(context.Background(), []Selection{{Path: "one", Commit: sha}, {Path: "two", Commit: nativeGit(t, second, "rev-parse", "HEAD")}}, &archive)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Repositories[0].Dirty || !manifest.Repositories[0].Untracked {
		t.Fatal("working data exclusions not advertised")
	}
	digest := sha256.Sum256(archive.Bytes())
	hash := hex.EncodeToString(digest[:])
	dest := &Service{Root: target}
	receipt, err := dest.Import(context.Background(), "request-1", hash, bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Directory != ".grasp-code-imports/request-1" {
		t.Fatal(receipt)
	}
	imported := filepath.Join(target, receipt.Directory, "one")
	if got := nativeGit(t, imported, "rev-parse", "HEAD"); got != sha {
		t.Fatalf("selected commit changed: %s", got)
	}
	if got := nativeGit(t, imported, "show", old+":tracked.txt"); got != "original" {
		t.Fatal("unpublished history did not transfer")
	}
	if got, err := os.ReadFile(filepath.Join(imported, "tracked.txt")); err != nil || string(got) != "new committed change" {
		t.Fatalf("wrong committed data: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(imported, "untracked.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("untracked file was silently imported")
	}
	if got := nativeGit(t, first, "show-ref"); got != before {
		t.Fatal("source Git refs changed")
	}
	if got, err := os.ReadFile(filepath.Join(first, ".git/index")); err != nil || !bytes.Equal(got, indexBefore) {
		t.Fatal("read-only preview refreshed the source index")
	}
	if got, _ := os.ReadFile(filepath.Join(first, "tracked.txt")); string(got) != "source dirty data" {
		t.Fatal("source dirty file changed")
	}
	if got, _ := os.ReadFile(filepath.Join(target, "existing-dirty.txt")); string(got) != "target unsaved" {
		t.Fatal("target working data changed")
	}
	if err := os.WriteFile(filepath.Join(imported, "tracked.txt"), []byte("edits after import"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.Import(context.Background(), "request-1", hash, bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(imported, "tracked.txt")); string(got) != "edits after import" {
		t.Fatal("retry overwrote later edits")
	}
}

func TestCodeTransferPreservesPartialImportAndRejectsWrongDigest(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	repo := fixtureRepo(t, source, "repo")
	s := &Service{Root: source}
	var archive bytes.Buffer
	if _, err := s.Export(context.Background(), []Selection{{Path: "repo", Commit: nativeGit(t, repo, "rev-parse", "HEAD")}}, &archive); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(archive.Bytes())
	digest := hex.EncodeToString(hash[:])
	dest := &Service{Root: target}
	if _, err := dest.Import(context.Background(), "wrong-digest", strings.Repeat("0", 64), bytes.NewReader(archive.Bytes())); err == nil {
		t.Fatal("corrupt archive was accepted")
	}
	if _, err := os.Stat(filepath.Join(target, ImportDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unverified archive touched target")
	}
	partial := filepath.Join(target, ImportDirectory, "partial")
	if err := os.MkdirAll(partial, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "unsaved.txt"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := dest.Import(context.Background(), "partial", digest, bytes.NewReader(archive.Bytes())); !errors.Is(err, ErrConflict) {
		t.Fatalf("partial import was reused: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(partial, "unsaved.txt")); string(got) != "retained" {
		t.Fatal("partial destination was modified")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dest.Import(ctx, "cancelled", digest, bytes.NewReader(archive.Bytes())); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled lease reached import")
	}
}

func TestCodeTransferDisclosesLFSExternalMetadataAndFiltersAsUnsupported(t *testing.T) {
	reasons := map[string]string{"lfs": "repository_lfs", "metadata_link": "repository_unsafe_metadata", "filter": "repository_custom_configuration", "worktree": "repository_external_worktree", "include": "repository_custom_configuration", "alternates": "repository_external_objects", "shallow": "repository_shallow", "grafts": "repository_rewritten_history", "submodules": "repository_submodules"}
	for test, reason := range reasons {
		t.Run(test, func(t *testing.T) {
			root := t.TempDir()
			repo := fixtureRepo(t, root, "repo")
			switch test {
			case "lfs":
				if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("version https://git-lfs.github.com/spec/v1\noid sha256:0123\nsize 100000000"), 0600); err != nil {
					t.Fatal(err)
				}
				nativeGit(t, repo, "add", ".")
				nativeGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "lfs pointer")
			case "metadata_link":
				if err := os.Symlink(t.TempDir(), filepath.Join(repo, ".git/linked")); err != nil {
					t.Fatal(err)
				}
			case "filter":
				nativeGit(t, repo, "config", "filter.custom.clean", "false")
			case "worktree":
				nativeGit(t, repo, "config", "core.worktree", t.TempDir())
			case "include":
				nativeGit(t, repo, "config", "include.path", "/tmp/external-config")
			case "alternates":
				if err := os.WriteFile(filepath.Join(repo, ".git/objects/info/alternates"), []byte(t.TempDir()), 0600); err != nil {
					t.Fatal(err)
				}
			case "shallow", "grafts":
				name := ".git/shallow"
				if test == "grafts" {
					name = ".git/info/grafts"
				}
				if err := os.WriteFile(filepath.Join(repo, name), []byte(nativeGit(t, repo, "rev-parse", "HEAD")+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "submodules":
				nativeGit(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+nativeGit(t, repo, "rev-parse", "HEAD")+",nested")
				nativeGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "synthetic submodule")
			}
			s := &Service{Root: root}
			if _, err := s.inspect(context.Background(), "repo", ""); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("unsupported repository accepted: %v", err)
			}
			inventory, err := s.Inventory(context.Background())
			if err != nil || len(inventory.Repositories) != 1 || inventory.Repositories[0].Supported {
				t.Fatalf("capability inventory overclaimed: %+v %v", inventory, err)
			}
			if inventory.Repositories[0].Reason != reason {
				t.Fatalf("missing safe capability reason: got %q want %q", inventory.Repositories[0].Reason, reason)
			}
		})
	}
}

func TestCodeTransferRootRepositoryImportsSelectedHistoricalCommitWithoutCheckoutOfParent(t *testing.T) {
	source := fixtureRepo(t, t.TempDir(), "source")
	target := fixtureRepo(t, t.TempDir(), "target")
	selected := nativeGit(t, source, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("later source version"), 0600); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, source, "add", ".")
	nativeGit(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "later")
	head := nativeGit(t, source, "rev-parse", "HEAD")
	targetHead := nativeGit(t, target, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(target, "tracked.txt"), []byte("unsaved target code"), 0600); err != nil {
		t.Fatal(err)
	}
	s, dest := &Service{Root: source}, &Service{Root: target}
	var archive bytes.Buffer
	if _, err := s.Export(context.Background(), []Selection{{Path: ".", Commit: selected}}, &archive); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(archive.Bytes())
	receipt, err := dest.Import(context.Background(), "historical", hex.EncodeToString(hash[:]), bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeGit(t, filepath.Join(target, receipt.Directory, "repo-1"), "rev-parse", "HEAD"); got != selected {
		t.Fatal("historical selection was replaced by latest source HEAD")
	}
	if nativeGit(t, source, "rev-parse", "HEAD") != head || nativeGit(t, target, "rev-parse", "HEAD") != targetHead {
		t.Fatal("source or parent target was checked out")
	}
	if data, _ := os.ReadFile(filepath.Join(target, "tracked.txt")); string(data) != "unsaved target code" {
		t.Fatal("parent dirty code was overwritten")
	}
}

func TestCodeTransferRejectsOversizedBinaryWithoutTruncation(t *testing.T) {
	root := t.TempDir()
	repo := fixtureRepo(t, root, "repo")
	f, err := os.OpenFile(filepath.Join(repo, "binary.bin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	nativeGit(t, repo, "add", ".")
	nativeGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "large binary")
	var archive bytes.Buffer
	_, err = (&Service{Root: root}).Export(context.Background(), []Selection{{Path: "repo", Commit: nativeGit(t, repo, "rev-parse", "HEAD")}}, &archive)
	if !errors.Is(err, ErrLimit) || archive.Len() != 0 {
		t.Fatalf("oversized file was silently truncated or published: %d %v", archive.Len(), err)
	}
	inventory, err := (&Service{Root: root}).Inventory(context.Background())
	if err != nil || len(inventory.Repositories) != 1 || inventory.Repositories[0].Reason != "repository_limit" {
		t.Fatalf("oversized commit not explained during inventory: %+v %v", inventory, err)
	}
}

func TestRepositoryCapabilityReasonNeverExposesHostErrors(t *testing.T) {
	if got := repositoryReason(errors.New("git failed at /private/host with sensitive diagnostic")); got != "repository_requires_snapshot" {
		t.Fatal(got)
	}
	if got := repositoryReason(ErrUnsafe); got != "repository_unsafe_metadata" {
		t.Fatal(got)
	}
}

func TestCodeTransferCancellationStopsGitHelperProcesses(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "helper-pid")
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nsleep 30 &\necho $! > \"$GRASP_TEST_HELPER_PID\"\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("GRASP_TEST_HELPER_PID", marker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := command(ctx, dir, nil, "fetch"); finished <- err }()
	deadline := time.Now().Add(time.Second)
	var pid int
	for pid == 0 && time.Now().Before(deadline) {
		if data, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if pid == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if pid == 0 {
		t.Fatal("test helper did not start")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled command succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled command did not exit")
	}
	// A killed child can briefly remain a zombie until init reaps it. It must
	// already be unable to execute or write to an imported repository.
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err == nil {
		fields := strings.Fields(string(data))
		if len(fields) < 3 || fields[2] != "Z" {
			t.Fatalf("Git helper survived command cancellation: %s", data)
		}
	} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ESRCH) {
		t.Fatal(err)
	}
}

func TestCodeTransferRejectsEscapeLinksNonGitAndIncompleteDestinations(t *testing.T) {
	root := t.TempDir()
	repo := fixtureRepo(t, root, "repo")
	s := &Service{Root: root}
	for _, value := range []string{"../repo", "/tmp", "repo/nested", "repo\\other"} {
		if _, err := s.repo(value); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("unsafe path accepted: %q %v", value, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.inspect(context.Background(), "empty", ""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("non-Git advertised as commit import: %v", err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, repo, "add", ".")
	nativeGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "unsafe link")
	if _, err := s.inspect(context.Background(), "repo", ""); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("unsafe committed symlink accepted: %v", err)
	}
	if _, err := s.Import(context.Background(), "../escape", strings.Repeat("0", 64), bytes.NewReader(nil)); !errors.Is(err, ErrUnsafe) {
		t.Fatal("unsafe import identity accepted")
	}
}
