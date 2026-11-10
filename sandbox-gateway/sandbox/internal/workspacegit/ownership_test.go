package workspacegit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func foreignRepository(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("creating a real foreign-UID fixture requires chown; exercised by the root native suite")
	}
	if err := filepath.WalkDir(dir, func(name string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(name, 31415, 31415)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCodeTransferForeignUIDTrustIsExactAndDoesNotChangeGitConfiguration(t *testing.T) {
	t.Setenv("SUDO_UID", "")
	root, target := t.TempDir(), t.TempDir()
	repo := fixtureRepo(t, root, "repo")
	neighbor := fixtureRepo(t, root, "neighbor")
	commit := nativeGit(t, repo, "rev-parse", "HEAD")
	config, err := os.ReadFile(filepath.Join(repo, ".git/config"))
	if err != nil {
		t.Fatal(err)
	}
	foreignRepository(t, repo)
	foreignRepository(t, neighbor)
	ctx := context.Background()
	if _, err := command(ctx, repo, nil, "rev-parse", "HEAD"); err == nil {
		t.Fatal("fixture does not exercise Git's dubious ownership rejection")
	}
	service := &Service{Root: root}
	inventory, err := service.Inventory(ctx)
	if err != nil || len(inventory.Repositories) != 2 || !inventory.Repositories[0].Supported || !inventory.Repositories[1].Supported {
		t.Fatalf("authorized foreign repositories unavailable: %+v, %v", inventory, err)
	}
	var archive bytes.Buffer
	if _, err := service.Export(ctx, []Selection{{Path: "repo", Commit: commit}}, &archive); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	receipt, err := (&Service{Root: target}).Import(ctx, "foreign-owner", hex.EncodeToString(digest[:]), bytes.NewReader(archive.Bytes()))
	if err != nil || len(receipt.Repositories) != 1 || receipt.Repositories[0].Commit != commit {
		t.Fatalf("foreign repository round trip failed: %+v, %v", receipt, err)
	}
	if _, err := command(ctx, neighbor, nil, "rev-parse", "HEAD"); err == nil {
		t.Fatal("a source ownership exception escaped its one command")
	}
	unchanged, err := os.ReadFile(filepath.Join(repo, ".git/config"))
	if err != nil || !bytes.Equal(config, unchanged) {
		t.Fatalf("preview/export changed repository configuration: %v", err)
	}
}

func TestCodeTransferSourceTrustStaysOnPinnedDirectoryAfterPathReplacement(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	repo := fixtureRepo(t, root, "repo")
	other := fixtureRepo(t, outside, "other")
	if err := os.WriteFile(filepath.Join(other, "tracked.txt"), []byte("unrelated repository"), 0600); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, other, "add", ".")
	nativeGit(t, other, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-qm", "neighbor")
	want := nativeGit(t, repo, "rev-parse", "HEAD")
	service := &Service{Root: root}
	dir, err := service.repo("repo")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	if err := os.Rename(repo, filepath.Join(root, "renamed")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, repo); err != nil {
		t.Fatal(err)
	}
	got, err := sourceCommand(context.Background(), dir, nil, nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(got)) != want {
		t.Fatalf("command was redirected through a replaced pathname: %q, %v", got, err)
	}
	if replaced, err := service.repo("repo"); err == nil {
		_ = replaced.Close()
		t.Fatal("a later source selection escaped OpenRoot through the replacement symlink")
	}
}

func TestCodeTransferSourceTrustRejectsWildcardSafeDirectory(t *testing.T) {
	root := t.TempDir()
	fixtureRepo(t, root, "*")
	dir, err := (&Service{Root: root}).repo("*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	if _, err := sourceCommand(context.Background(), dir, nil, nil, "rev-parse", "HEAD"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a wildcard path could expand Git trust: %v", err)
	}
}
