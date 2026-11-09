//go:build e2ereal

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The release container creates 0700 maintenance/control directories as root.
// The host runner cannot traverse those directories. Only after Compose has
// stopped, remove their contents in the matching image with one exact private
// fixture mount; do not relax the production directory permissions.
func removeRestartFixtureData(ctx context.Context, root, parent, image string) error {
	args, err := restartFixtureCleanupArgs(root, parent, image)
	if err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("remove container-owned fixture contents: %w: %s", err, out)
	}
	return os.Remove(root)
}

func restartFixtureCleanupArgs(root, parent, image string) ([]string, error) {
	root, parent = filepath.Clean(root), filepath.Clean(parent)
	if !filepath.IsAbs(root) || !filepath.IsAbs(parent) || filepath.Dir(root) != parent ||
		!strings.HasPrefix(filepath.Base(root), ".localdata-e2e-") || strings.Contains(root, ",") || image == "" {
		return nil, errors.New("refuse cleanup outside the owned restart fixture")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("refuse cleanup of a replaced restart fixture")
	}
	const script = `const fs=require('node:fs');for(const name of fs.readdirSync('/owned-restart-fixture'))fs.rmSync('/owned-restart-fixture/'+name,{recursive:true,force:true});`
	return []string{"run", "--rm", "--network", "none", "--read-only", "--user", "0:0", "--mount",
		"type=bind,source=" + root + ",target=/owned-restart-fixture", "--entrypoint", "node", image, "-e", script}, nil
}

func TestRestartFixtureCleanupRestrictsItsOnlyWritableMount(t *testing.T) {
	parent := t.TempDir()
	root, err := os.MkdirTemp(parent, ".localdata-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	args, err := restartFixtureCleanupArgs(root, parent, "grasp:fixture")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{"--network none", "--read-only", "--user 0:0", "type=bind,source=" + root + ",target=/owned-restart-fixture", "--entrypoint node grasp:fixture"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("cleanup is missing %q: %v", expected, args)
		}
	}
	if strings.Count(joined, "type=bind,") != 1 || strings.Contains(joined, "docker.sock") {
		t.Fatalf("cleanup exposes resources outside the fixture: %v", args)
	}
	for _, changed := range []struct{ root, parent, image string }{
		{parent, parent, "grasp:fixture"},
		{root, filepath.Dir(parent), "grasp:fixture"},
		{root, parent, ""},
		{".localdata-e2e-relative", parent, "grasp:fixture"},
	} {
		if _, err := restartFixtureCleanupArgs(changed.root, changed.parent, changed.image); err == nil {
			t.Fatalf("accepted an unowned cleanup: %#v", changed)
		}
	}
	link := filepath.Join(parent, ".localdata-e2e-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := restartFixtureCleanupArgs(link, parent, "grasp:fixture"); err == nil {
		t.Fatal("accepted a replaced fixture symlink")
	}
}

func TestRestartFixtureCleanupRemovesContainerPrivateDirectories(t *testing.T) {
	image := os.Getenv("GRASP_RESTART_CLEANUP_IMAGE")
	if image == "" {
		t.Skip("set GRASP_RESTART_CLEANUP_IMAGE for the real Docker ownership regression")
	}
	parent := t.TempDir()
	root, err := os.MkdirTemp(parent, ".localdata-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	const create = `const fs=require('node:fs');for(const n of ['execution-maintenance/fresh-install','execution-control/private']){fs.mkdirSync('/owned-restart-fixture/'+n,{recursive:true,mode:0o700});fs.writeFileSync('/owned-restart-fixture/'+n+'/receipt','owned synthetic receipt',{mode:0o600});}`
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := []string{"run", "--rm", "--network", "none", "--read-only", "--user", "0:0", "--mount", "type=bind,source=" + root + ",target=/owned-restart-fixture", "--entrypoint", "node", image, "-e", create}
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("create private root-owned fixture: %v: %s", err, out)
	}
	outside := filepath.Join(parent, "not-owned")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if err := removeRestartFixtureData(ctx, root, parent, image); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned fixture remains: %v", err)
	}
	if contents, err := os.ReadFile(outside); err != nil || string(contents) != "preserve" {
		t.Fatalf("cleanup followed an unrelated file: %q, %v", contents, err)
	}
}
