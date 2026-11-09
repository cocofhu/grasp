package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
)

func runtimeSnapshotGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Snapshot Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Snapshot Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

// Build the real router fixture with:
// (cd sandbox-gateway/sandbox && go test -c -o /tmp/grasp-workspace-runtime-fixture ./internal/router)
// Then opt in with GRASP_WORKSPACE_SNAPSHOT_FIXTURE_BINARY pointing to that binary.
// This exercises the production HTTP client, collector, native Git, durable jobs,
// object read-back, SQL pin and the fresh-directory materializer across modules.
func TestWorkspaceSnapshotLiveRuntimePublicationAndRestore(t *testing.T) {
	binary := os.Getenv("GRASP_WORKSPACE_SNAPSHOT_FIXTURE_BINARY")
	if binary == "" {
		t.Skip("requires built cross-module native runtime fixture")
	}
	source, control, target, coord := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	repo := filepath.Join(source, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(root, name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runtimeSnapshotGit(t, repo, "init")
	write(repo, "tracked.txt", "committed base")
	write(repo, "deleted.txt", "deleted later")
	runtimeSnapshotGit(t, repo, "add", ".")
	runtimeSnapshotGit(t, repo, "commit", "-qm", "fixture base")
	head := runtimeSnapshotGit(t, repo, "rev-parse", "HEAD")
	write(repo, "tracked.txt", "dirty staged bytes")
	runtimeSnapshotGit(t, repo, "add", "tracked.txt")
	write(repo, "tracked.txt", "dirty working bytes")
	write(repo, "untracked.txt", "untracked bytes")
	if err := os.Remove(filepath.Join(repo, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	write(source, ".codex/auth.json", "private native token")
	write(source, "non-git/data.bin", strings.Repeat("\x00\xffbinary\x01", 200000))
	ready, stop := filepath.Join(coord, "ready.json"), filepath.Join(coord, "stop")
	log, err := os.Create(filepath.Join(coord, "runtime.log"))
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(t.Context(), binary, "-test.run=^TestWorkspaceSnapshotProtocolFixture$", "-test.v")
	child.Env = append(os.Environ(), "GRASP_WORKSPACE_SNAPSHOT_FIXTURE=1", "GRASP_SNAPSHOT_TEST_SOURCE="+source, "GRASP_SNAPSHOT_TEST_CONTROL="+control, "GRASP_SNAPSHOT_TEST_RESTORE="+target, "GRASP_SNAPSHOT_TEST_READY="+ready, "GRASP_SNAPSHOT_TEST_STOP="+stop)
	child.Stdout, child.Stderr = log, log
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(stop, []byte("stop"), 0600)
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = child.Process.Kill()
			<-done
		}
		_ = log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Log(string(data))
		}
	})
	var address struct {
		Address string `json:"address"`
	}
	until := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(ready)
		if err == nil && json.Unmarshal(data, &address) == nil && address.Address != "" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("native runtime fixture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	host, portText, err := net.SplitHostPort(address.Address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	client := sandbox.WorkspaceJobClient{Host: host, Port: port, Password: "snapshot-fixture-password", Timeout: 3 * time.Second}
	p, objects, _, _, sandboxID := publicationFixture(t, client, func(hold *payloadhold.Options, jobs *WorkspaceJobOptions) {
		// Native auth/fsync use real production lease durations. Separate unit
		// tests exercise multiple renewals and expiry at 300ms without bcrypt.
		hold.LeaseTTL, hold.RenewInterval = 45*time.Second, 15*time.Second
		jobs.LeaseTTL, jobs.RenewInterval, jobs.RemoteInterval = 45*time.Second, 15*time.Second, 5*time.Second
	})
	long := os.Getenv("GRASP_WORKSPACE_SNAPSHOT_NATIVE_LONG") == "1"
	var delayed atomic.Bool
	if long {
		objects.beforePut = func(ctx context.Context) error {
			if !delayed.CompareAndSwap(false, true) {
				return nil
			}
			timer := time.NewTimer(50 * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			case <-timer.C:
				return nil
			}
		}
	}
	request, disconnect := context.WithCancel(t.Context())
	capture, err := p.Capture(request, "alice", WorkspaceCaptureInput{ClientRequestID: "native-capture-request", ScopeKind: "solo", ScopeID: "solo-original", SandboxID: sandboxID, Paths: []string{"."}, Duration: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	disconnect()
	app, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- p.Run(app) }()
	stopped := false
	defer func() {
		if !stopped {
			cancel()
			<-done
		}
	}()
	var snapshot models.WorkspaceSnapshot
	deadline := time.Now().Add(15 * time.Second)
	if long {
		deadline = time.Now().Add(75 * time.Second)
	}
	for {
		if err := p.db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
			t.Fatal(err)
		}
		if snapshot.State == "ready" {
			break
		}
		if time.Now().After(deadline) {
			var jobs []models.WorkspaceSnapshotJob
			_ = p.db.Find(&jobs).Error
			t.Fatalf("native snapshot never became ready: snapshot=%+v jobs=%+v", snapshot, jobs)
		}
		time.Sleep(15 * time.Millisecond)
	}
	var hold models.PayloadUploadHold
	if err := p.db.Take(&hold, "snapshot_id = ?", snapshot.ID).Error; err != nil {
		t.Fatal(err)
	}
	if hold.State != payloadhold.StatePinned || snapshot.SourceGeneration == 0 || objects.puts.Load() != 2 || objects.gets.Load() < 2 {
		t.Fatal("native capture skipped publication proof")
	}
	var publication models.WorkspaceSnapshotJob
	if err := p.db.Take(&publication, "id = ?", snapshot.PublicationJobID).Error; err != nil {
		t.Fatal(err)
	}
	var read workspacefiles.ReadSpec
	if workspacefiles.Decode([]byte(publication.Body), &read) != nil {
		t.Fatal("invalid frozen read spec")
	}
	if snapshot.ArchiveSHA256 != read.Receipt.Archive.SHA256 || snapshot.ManifestSHA256 != read.Receipt.Manifest.SHA256 {
		t.Fatal("ready refs differ from native capture receipt")
	}
	cancel()
	<-done
	stopped = true
	// The current product binding remains untouched. Exercise the actual native
	// receiving/materializing path in a newly fenced physical operation only.
	operation, release, err := p.Jobs.sandbox.acquireSandboxOperation(t.Context(), sandboxID, "snapshot_native_restore_test")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var resource models.Sandbox
	if err := p.db.Take(&resource, sandboxID).Error; err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(struct {
		Receipt workspacefiles.Receipt `json:"receipt"`
	}{read.Receipt})
	if err != nil {
		t.Fatal(err)
	}
	body, err = canonicalWorkspaceJob(body)
	if err != nil {
		t.Fatal(err)
	}
	row := models.WorkspaceSnapshotJob{ID: "restore-native-0001", Kind: "restore_files", Nonce: "restore-native-nonce", Lineage: "lineage-original", Deadline: time.Now().UTC().Add(30 * time.Second), Body: string(body)}
	spec, hash, err := runtimeJobSpec(row, "resource-one")
	if err != nil {
		t.Fatal(err)
	}
	fence := sandbox.WorkspaceJobRenew{SHA256: hash, Lineage: row.Lineage, Fence: sandbox.WorkspaceJobFence{Owner: "native-restore-owner", Generation: resource.Generation, ExpiresAt: time.Now().Add(15 * time.Second)}}
	if _, err := client.Create(operation, sandbox.WorkspaceJobCreate{Spec: spec, SHA256: hash, Fence: fence.Fence}); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []struct {
		part, ref string
		size      int64
	}{{"archive", snapshot.ArchiveRef, read.Receipt.Archive.Bytes}, {"manifest", snapshot.ManifestRef, read.Receipt.Manifest.Bytes}} {
		file, err := p.files.OpenBounded(operation, artifact.ref, artifact.size)
		if err != nil {
			t.Fatal(err)
		}
		for offset := int64(0); offset < artifact.size; {
			data := make([]byte, min(int64(sandbox.WorkspaceFileChunkBytes), artifact.size-offset))
			if _, err := io.ReadFull(file, data); err != nil {
				t.Fatal(err)
			}
			for {
				err = client.WriteWorkspaceChunk(operation, row.ID, fence, artifact.part, offset, data)
				var pending *sandbox.WorkspaceJobError
				if errors.As(err, &pending) && pending.Status == 425 {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if offset == 0 {
				if err := client.WriteWorkspaceChunk(operation, row.ID, fence, artifact.part, offset, data); err != nil {
					t.Fatal("identical private chunk retry was not idempotent", err)
				}
			}
			offset += int64(len(data))
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.FinishWorkspaceTransfer(operation, row.ID, fence); err != nil {
		t.Fatal(err)
	}
	var native sandbox.WorkspaceJobReceipt
	for until := time.Now().Add(10 * time.Second); ; {
		native, err = client.Get(operation, row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if native.State == "completed" {
			break
		}
		if native.State == "failed" || native.State == "unknown" || time.Now().After(until) {
			t.Fatalf("native materializer failed: %+v", native)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var result struct {
		Directory      string `json:"directory"`
		ArchiveSHA256  string `json:"archiveSha256"`
		ManifestSHA256 string `json:"manifestSha256"`
	}
	if json.Unmarshal(native.Result, &result) != nil || result.Directory != row.ID || result.ArchiveSHA256 != snapshot.ArchiveSHA256 || result.ManifestSHA256 != snapshot.ManifestSHA256 {
		t.Fatal("materialized receipt mismatch")
	}
	destination := filepath.Join(target, result.Directory)
	for name, want := range map[string]string{"tracked.txt": "dirty working bytes", "untracked.txt": "untracked bytes"} {
		got, err := os.ReadFile(filepath.Join(destination, "repo", name))
		if err != nil || string(got) != want {
			t.Fatal("materializer lost source working state", name, err)
		}
	}
	if runtimeSnapshotGit(t, filepath.Join(destination, "repo"), "rev-parse", "HEAD") != head || runtimeSnapshotGit(t, filepath.Join(destination, "repo"), "show", ":tracked.txt") != "dirty staged bytes" {
		t.Fatal("Git HEAD/index was not materialized")
	}
	for _, name := range []string{"repo/deleted.txt", ".codex/auth.json"} {
		if _, err := os.Stat(filepath.Join(destination, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("private/deleted source was recreated", name, err)
		}
	}
	want, err := os.ReadFile(filepath.Join(source, "non-git/data.bin"))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(destination, "non-git/data.bin"))
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatal("binary materialization changed bytes", err)
	}
	write(destination, "later-edit.txt", "later user edit")
	if _, err := client.Create(operation, sandbox.WorkspaceJobCreate{Spec: spec, SHA256: hash, Fence: fence.Fence}); err != nil {
		t.Fatal(err)
	}
	later, _ := os.ReadFile(filepath.Join(destination, "later-edit.txt"))
	sourceBytes, _ := os.ReadFile(filepath.Join(repo, "tracked.txt"))
	if string(later) != "later user edit" || string(sourceBytes) != "dirty working bytes" {
		t.Fatal("native retry overwrote later edits or original source")
	}
	proof := sha256.Sum256(actual)
	t.Logf("real authenticated runtime capture→GET→PUT/read-back→SQL pin+ready→fresh restore passed; archive=%s binarySHA=%s sourceGeneration=%d publicationGeneration=%d long=%v", snapshot.ArchiveSHA256, hex.EncodeToString(proof[:]), snapshot.SourceGeneration, publication.RuntimeGeneration, long)
}
