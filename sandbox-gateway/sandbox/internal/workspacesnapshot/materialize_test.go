package workspacesnapshot

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/workspacejob"
)

func materializeFixture(t *testing.T, captured capturedFixture, parentPath, name string, wrap ...func(io.ReadSeeker) io.ReadSeeker) (RestoreReceipt, error) {
	t.Helper()
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close() }()
	archive, err := os.Open(filepath.Join(captured.control, "data", "capture-original", ArchiveName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	var reader io.ReadSeeker = archive
	for _, fn := range wrap {
		reader = fn(reader)
	}
	type completion struct {
		receipt RestoreReceipt
		err     error
	}
	done := make(chan completion, 1)
	jobs, err := workspacejob.New(workspacejob.Config{Root: t.TempDir(), Resource: "snapshot-target", Lineage: "lineage-original", MaxLease: time.Minute, MaxDuration: 2 * time.Minute, Executors: map[string]workspacejob.Executor{
		"restore_files": func(ctx context.Context, h *workspacejob.Handle, _ json.RawMessage) (json.RawMessage, error) {
			result, err := captured.engine.Materialize(ctx, h, parent, name, reader, captured.json, captured.receipt)
			done <- completion{result, err}
			if err != nil {
				return nil, err
			}
			return json.Marshal(result)
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = jobs.Close(context.Background()) }()
	spec, hash, err := workspacejob.Fingerprint(workspacejob.Spec{ID: "restore-original", Kind: "restore_files", Resource: "snapshot-target", Lineage: "lineage-original", Nonce: "restore-original", Deadline: time.Now().Add(90 * time.Second), Body: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Create(workspacejob.CreateRequest{Spec: spec, SHA256: hash, Fence: workspacejob.Fence{Owner: "restore-owner", Generation: 7, ExpiresAt: time.Now().Add(55 * time.Second)}}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		return result.receipt, result.err
	case <-time.After(30 * time.Second):
		t.Fatal("bounded local materialization did not finish")
		return RestoreReceipt{}, context.DeadlineExceeded
	}
}

func TestWorkspaceSnapshotMaterializeRebuildsGitAndPreservesSourceAndLaterTargetEdits(t *testing.T) {
	source := t.TempDir()
	fixtureGit(t, source, "init", "--template=", ".")
	fixtureFile(t, source, "tracked.txt", "base")
	fixtureFile(t, source, "deleted.txt", "base deleted")
	fixtureGit(t, source, "add", ".")
	fixtureGit(t, source, "commit", "-m", "base fixture")
	head := fixtureGit(t, source, "rev-parse", "HEAD")
	fixtureFile(t, source, "tracked.txt", "staged bytes")
	fixtureFile(t, source, "staged-new.txt", "unreachable staged blob")
	fixtureGit(t, source, "add", "tracked.txt", "staged-new.txt")
	fixtureFile(t, source, "tracked.txt", "unstaged bytes")
	fixtureFile(t, source, "untracked.txt", "untracked bytes")
	if err := os.Remove(filepath.Join(source, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, source, "config", "filter.private.clean", "touch must-not-execute")
	index := fixtureGit(t, source, "ls-files", "-v", "--stage", "-z")
	captured := captureFixture(t, source, []string{"."}, nil, Limits{})
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	parent := t.TempDir()
	result, err := materializeFixture(t, captured, parent, "restored")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "restored")
	if result.Repositories != 1 || result.Directory != "restored" || result.ArchiveSHA256 != captured.receipt.Archive.SHA256 {
		t.Fatal(result)
	}
	if got := fixtureGit(t, target, "rev-parse", "HEAD"); got != head {
		t.Fatal("committed history/branch changed", got, head)
	}
	if got := fixtureGit(t, target, "ls-files", "-v", "--stage", "-z"); got != index {
		t.Fatalf("validated index was not reconstructed: %q != %q", got, index)
	}
	if got := fixtureGit(t, target, "show", ":staged-new.txt"); got != "unreachable staged blob" {
		t.Fatal("staged blob not reachable from HEAD was omitted", got)
	}
	for name, expected := range map[string]string{"tracked.txt": "unstaged bytes", "staged-new.txt": "unreachable staged blob", "untracked.txt": "untracked bytes"} {
		data, err := os.ReadFile(filepath.Join(target, name))
		if err != nil || string(data) != expected {
			t.Fatal(name, string(data), err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "deleted.txt")); !os.IsNotExist(err) {
		t.Fatal("Git checkout overlaid deleted working file")
	}
	config, err := os.ReadFile(filepath.Join(target, ".git", "config"))
	if err != nil || strings.Contains(string(config), "filter") || strings.Contains(string(config), "credential") {
		t.Fatal("source authorization/filter configuration copied into target")
	}
	fixtureFile(t, target, "tracked.txt", "later user edits")
	if _, err := materializeFixture(t, captured, parent, "restored"); !os.IsExist(err) {
		t.Fatal("existing target was not refused", err)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "tracked.txt")); string(data) != "later user edits" {
		t.Fatal("retry overlaid later edits")
	}
	if data, _ := os.ReadFile(filepath.Join(source, "tracked.txt")); string(data) != "unstaged bytes" || fixtureGit(t, source, "rev-parse", "HEAD") != head {
		t.Fatal("materialization modified source")
	}
}

func TestWorkspaceSnapshotLinkedWorktreeIsMaterializedAsStandaloneGit(t *testing.T) {
	source := t.TempDir()
	main := filepath.Join(source, "main")
	if err := os.Mkdir(main, 0700); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, main, "init", "--template=", ".")
	fixtureFile(t, main, "tracked.txt", "committed fixture")
	fixtureGit(t, main, "add", ".")
	fixtureGit(t, main, "commit", "-m", "base fixture")
	linked := filepath.Join(source, "linked")
	fixtureGit(t, main, "worktree", "add", "-b", "linked-fixture", linked)
	fixtureFile(t, linked, "tracked.txt", "dirty linked bytes")
	fixtureFile(t, linked, "untracked.txt", "new linked bytes")
	captured := captureFixture(t, source, []string{"linked"}, nil, Limits{})
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	if len(captured.manifest.Repositories) != 1 || !captured.manifest.Repositories[0].Linked {
		t.Fatal("linked source was not deliberately represented")
	}
	parent := t.TempDir()
	if _, err := materializeFixture(t, captured, parent, "restored"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "restored", "linked")
	info, err := os.Stat(filepath.Join(target, ".git"))
	if err != nil || !info.IsDir() {
		t.Fatal("copied a stale external .git pointer", err)
	}
	if got := fixtureGit(t, target, "symbolic-ref", "HEAD"); got != "refs/heads/linked-fixture" {
		t.Fatal(got)
	}
	if got := fixtureGit(t, target, "show", "HEAD:tracked.txt"); got != "committed fixture" {
		t.Fatal("linked object history was not copied", got)
	}
	if data, err := os.ReadFile(filepath.Join(target, "tracked.txt")); err != nil || string(data) != "dirty linked bytes" {
		t.Fatal(string(data), err)
	}
	if _, err := os.Stat(filepath.Join(target, ".git", "commondir")); !os.IsNotExist(err) {
		t.Fatal("restored repository still depends on source common Git directory")
	}
	outside := captureFixture(t, linked, []string{"."}, nil, Limits{})
	if outside.err == nil {
		t.Fatal("unknown external common Git directory was traversed")
	}
	if data, _ := os.ReadFile(filepath.Join(linked, "tracked.txt")); string(data) != "dirty linked bytes" {
		t.Fatal("unsupported source was not retained")
	}
}
