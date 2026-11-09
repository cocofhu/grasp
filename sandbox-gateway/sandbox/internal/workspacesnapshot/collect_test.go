package workspacesnapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"backend/internal/workspacejob"
)

func fixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Snapshot Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Snapshot Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture Git %v: %v %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

type capturedFixture struct {
	engine   *Engine
	jobs     *workspacejob.Service
	control  string
	receipt  Receipt
	manifest Manifest
	json     []byte
	err      error
}

func captureFixture(t *testing.T, source string, paths, ignored []string, limits Limits, mutate ...func(*Config)) capturedFixture {
	t.Helper()
	config := Config{Root: source, Limits: limits}
	for _, fn := range mutate {
		fn(&config)
	}
	e, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := e.NewSpec(paths, ignored)
	if err != nil {
		_ = e.Close()
		t.Fatal(err)
	}
	body, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	control := t.TempDir()
	executed := make(chan error, 1)
	jobs, err := workspacejob.New(workspacejob.Config{Root: control, Resource: "snapshot-source", Lineage: "lineage-original", MaxLease: time.Minute, MaxDuration: 2 * time.Minute, Executors: map[string]workspacejob.Executor{
		"capture_files": func(ctx context.Context, h *workspacejob.Handle, body json.RawMessage) (json.RawMessage, error) {
			value, err := e.capture(ctx, h, body)
			executed <- err
			return value, err
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobs.Close(context.Background()); _ = e.Close() })
	job, hash, err := workspacejob.Fingerprint(workspacejob.Spec{ID: "capture-original", Kind: "capture_files", Resource: "snapshot-source", Lineage: "lineage-original", Nonce: "capture-original", Deadline: time.Now().Add(90 * time.Second), Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Create(workspacejob.CreateRequest{Spec: job, SHA256: hash, Fence: workspacejob.Fence{Owner: "snapshot-owner", Generation: 1, ExpiresAt: time.Now().Add(55 * time.Second)}}); err != nil {
		t.Fatal(err)
	}
	result := capturedFixture{engine: e, jobs: jobs, control: control}
	select {
	case result.err = <-executed:
	case <-time.After(30 * time.Second):
		t.Fatal("bounded local capture did not finish")
	}
	if result.err != nil {
		return result
	}
	var receipt workspacejob.Receipt
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		receipt, err = jobs.Get(job.ID)
		if err != nil || receipt.State == "completed" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil || receipt.State != "completed" {
		t.Fatalf("capture did not publish exact control receipt: %+v %v", receipt, err)
	}
	if err := json.Unmarshal(receipt.Result, &result.receipt); err != nil {
		t.Fatal(err)
	}
	result.json, err = os.ReadFile(filepath.Join(control, "data", job.ID, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(result.json, &result.manifest); err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(result.json)
	if hex.EncodeToString(manifestHash[:]) != result.receipt.Manifest.SHA256 {
		t.Fatal("manifest is not the exact durable receipt bytes")
	}
	return result
}

func TestWorkspaceSnapshotCollectsNonGitIgnoredOptInAndExcludesPrivateState(t *testing.T) {
	source := t.TempDir()
	fixtureFile(t, source, ".gitignore", "cache/\n*.skip\n")
	fixtureFile(t, source, "keep.txt", "source bytes")
	fixtureFile(t, source, "cache/selected.skip", "explicit ignored bytes")
	fixtureFile(t, source, "cache/other.skip", "default ignored")
	fixtureFile(t, source, "root.skip", "default ignored")
	fixtureFile(t, source, ".codex/auth.json", "native authorization")
	fixtureFile(t, source, ".grasp/runtime/session.json", "runtime private state")
	fixtureFile(t, source, ".npmrc", "//example.test/:_authToken=private-token")
	if err := os.Symlink("keep.txt", filepath.Join(source, "relative-link")); err != nil {
		t.Fatal(err)
	}
	result := captureFixture(t, source, []string{"."}, []string{"cache/selected.skip"}, Limits{})
	if result.err != nil {
		t.Fatal(result.err)
	}
	paths := []string{}
	for _, entry := range result.manifest.Files {
		paths = append(paths, entry.Path)
	}
	for _, expected := range []string{".gitignore", "keep.txt", "relative-link", "cache/selected.skip"} {
		if !slices.Contains(paths, expected) {
			t.Fatalf("selected bytes missing %s: %v", expected, paths)
		}
	}
	for _, excluded := range []string{".codex/auth.json", ".grasp/runtime/session.json", ".npmrc", "cache/other.skip", "root.skip"} {
		if slices.Contains(paths, excluded) {
			t.Fatalf("private or non-selected ignored bytes captured %s; files=%v excluded=%+v", excluded, paths, result.manifest.Excluded)
		}
	}
	if result.manifest.Consistency != "verified_copy" || result.receipt.Consistency != "verified_copy" || len(result.manifest.Repositories) != 0 {
		t.Fatal("file copy fabricated atomic volume/Git/process semantics")
	}
}

func TestWorkspaceSnapshotCollectsDirtyStagedDeletedUntrackedMultiRepository(t *testing.T) {
	source := t.TempDir()
	for _, name := range []string{"app", "library"} {
		root := filepath.Join(source, name)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		fixtureGit(t, root, "init", "--template=", ".")
		fixtureFile(t, root, "tracked.txt", "base version")
		fixtureFile(t, root, "deleted.txt", "retained history")
		fixtureGit(t, root, "add", ".")
		fixtureGit(t, root, "commit", "-m", "fixture only")
		fixtureFile(t, root, "tracked.txt", "staged content")
		fixtureFile(t, root, "staged-new.txt", "staged, unreachable from HEAD")
		fixtureGit(t, root, "add", "tracked.txt", "staged-new.txt")
		fixtureFile(t, root, "tracked.txt", "unstaged content after staging")
		fixtureFile(t, root, "untracked.txt", "plain untracked bytes")
		if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
			t.Fatal(err)
		}
		fixtureGit(t, root, "config", "filter.should-not-run.clean", "touch ../unauthorized-filter")
	}
	before, err := os.ReadFile(filepath.Join(source, "app", ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	result := captureFixture(t, source, []string{"."}, nil, Limits{})
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(result.manifest.Repositories) != 2 {
		t.Fatal("multi-repository state lost", len(result.manifest.Repositories))
	}
	for _, repo := range result.manifest.Repositories {
		if repo.Pack == "" || repo.PackBytes == 0 || len(repo.Index) != 3 || repo.HEAD == "" {
			t.Fatalf("Git state was not materialized as pack+validated index: %+v", repo)
		}
	}
	after, err := os.ReadFile(filepath.Join(source, "app", ".git", "index"))
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only capture modified source index", err)
	}
	if _, err := os.Stat(filepath.Join(source, "unauthorized-filter")); !os.IsNotExist(err) {
		t.Fatal("capture executed source filter configuration")
	}
}
