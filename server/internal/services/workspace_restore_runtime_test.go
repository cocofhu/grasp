package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// This transport drops the success observed by the product service only after
// the real native journal proves completion. Reconcile must GET that receipt;
// no native PUT, Finish or Create may be replayed after this injected ACK loss.
type restoreNativeTransport struct {
	sandbox.WorkspaceJobClient
	dropFinish                      atomic.Bool
	creates, writes, finishes, gets atomic.Int32
}

func (r *restoreNativeTransport) Create(ctx context.Context, in sandbox.WorkspaceJobCreate) (sandbox.WorkspaceJobReceipt, error) {
	r.creates.Add(1)
	return r.WorkspaceJobClient.Create(ctx, in)
}
func (r *restoreNativeTransport) Get(ctx context.Context, id string) (sandbox.WorkspaceJobReceipt, error) {
	r.gets.Add(1)
	return r.WorkspaceJobClient.Get(ctx, id)
}
func (r *restoreNativeTransport) WriteWorkspaceChunk(ctx context.Context, id string, fence sandbox.WorkspaceJobRenew, part string, offset int64, body []byte) error {
	r.writes.Add(1)
	return r.WorkspaceJobClient.WriteWorkspaceChunk(ctx, id, fence, part, offset, body)
}
func (r *restoreNativeTransport) FinishWorkspaceTransfer(ctx context.Context, id string, fence sandbox.WorkspaceJobRenew) error {
	r.finishes.Add(1)
	if err := r.WorkspaceJobClient.FinishWorkspaceTransfer(ctx, id, fence); err != nil {
		return err
	}
	if !r.dropFinish.Load() {
		return nil
	}
	for {
		value, err := r.WorkspaceJobClient.Get(ctx, id)
		if err != nil {
			return err
		}
		if value.State == "completed" {
			return io.ErrUnexpectedEOF
		}
		if value.State == "failed" || value.State == "cancelled" || value.State == "unknown" {
			return ErrWorkspaceJobUnknown
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestWorkspaceRestoreLiveNativeProductReceiptAndLostACK(t *testing.T) {
	binary := os.Getenv("GRASP_WORKSPACE_SNAPSHOT_FIXTURE_BINARY")
	if binary == "" {
		t.Skip("requires cross-module native runtime fixture")
	}
	source, control, target, coord := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	write := func(root, name string, body []byte) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(source, "repo/tracked.txt", []byte("committed base"))
	repo := filepath.Join(source, "repo")
	runtimeSnapshotGit(t, repo, "init")
	runtimeSnapshotGit(t, repo, "add", ".")
	runtimeSnapshotGit(t, repo, "commit", "-qm", "native restore fixture")
	head := runtimeSnapshotGit(t, repo, "rev-parse", "HEAD")
	write(source, "repo/tracked.txt", []byte("staged snapshot bytes"))
	runtimeSnapshotGit(t, repo, "add", "tracked.txt")
	write(source, "repo/tracked.txt", []byte("dirty snapshot bytes"))
	write(source, "repo/untracked.txt", []byte("untracked snapshot bytes"))
	data := make([]byte, 2<<20)
	_, _ = rand.NewChaCha8([32]byte{1, 2, 3}).Read(data)
	write(source, "assets/random.bin", data)
	write(target, "current.txt", []byte("existing target edit"))
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
			content, _ := os.ReadFile(log.Name())
			t.Log(string(content))
		}
	})
	var address struct {
		Address string `json:"address"`
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		body, err := os.ReadFile(ready)
		if err == nil && json.Unmarshal(body, &address) == nil && address.Address != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real restore runtime fixture did not start")
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
	remote := &restoreNativeTransport{WorkspaceJobClient: sandbox.WorkspaceJobClient{Host: host, Port: port, Password: "snapshot-fixture-password", Timeout: 3 * time.Second}}
	p, _, _, revoked, sandboxID := publicationFixture(t, remote, func(hold *payloadhold.Options, jobs *WorkspaceJobOptions) {
		hold.LeaseTTL, hold.RenewInterval = 45*time.Second, 15*time.Second
		jobs.LeaseTTL, jobs.RenewInterval, jobs.RemoteInterval = 45*time.Second, 15*time.Second, 5*time.Second
	})
	capture, publication := beginPublication(t, p, sandboxID)
	if err := p.Jobs.RunOne(t.Context(), publication.ID); err != nil {
		t.Fatal(err)
	}
	solo := models.SoloSession{ID: "solo-original", UserID: "alice", Status: models.SoloStatusRunning, SandboxID: sandboxID, LifecycleEpoch: 7, LifecycleAttemptID: "native-original", CurrentConfigurationID: "config-original"}
	if err := p.db.Create(&solo).Error; err != nil {
		t.Fatal(err)
	}
	jobs := p.Jobs.options
	jobs.Authorize, jobs.AdmittedTx, jobs.CompletedTx = p.authorize, nil, nil
	service, err := NewWorkspaceRestoreService(p.db, p.Jobs.sandbox, p.holds, p.files, p.limits, jobs, restoreTestCallbacks(revoked))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := restoreTestBinding(t.Context(), p.db, "alice", solo.ID)
	if err != nil {
		t.Fatal(err)
	}
	// These later edits are outside the immutable capture and must survive copy.
	write(source, "repo/tracked.txt", []byte("later active source edit"))
	write(source, "repo/after-capture.txt", []byte("later source-only file"))
	status := runtimeSnapshotGit(t, repo, "status", "--porcelain=v1")
	indexBefore, err := os.ReadFile(filepath.Join(repo, ".git/index"))
	if err != nil {
		t.Fatal(err)
	}
	remote.dropFinish.Store(true)
	input := WorkspaceRestoreInput{ClientRequestID: "native-product-restore", SoloID: solo.ID, SnapshotID: capture.ID, ExpectedBinding: binding.Fingerprint, Duration: 30 * time.Second}
	receipt, err := service.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Jobs.RunOne(t.Context(), receipt.JobID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatalf("discarded Finish ACK was not unknown: %v", err)
	}
	unknown, err := service.Get(t.Context(), "alice", receipt.ID)
	if err != nil || unknown.State != "unknown" || unknown.Destination != "" {
		t.Fatalf("unknown materialization claimed successful: %+v %v", unknown, err)
	}
	creates, writes, finishes := remote.creates.Load(), remote.writes.Load(), remote.finishes.Load()
	confirmed, err := service.Reconcile(t.Context(), "alice", receipt.ID)
	if err != nil || confirmed.State != "restored" || confirmed.Destination != receipt.JobID {
		t.Fatalf("real original native receipt not reconciled: %+v %v", confirmed, err)
	}
	if remote.creates.Load() != creates || remote.writes.Load() != writes || remote.finishes.Load() != finishes {
		t.Fatal("reconciliation repeated a native side effect")
	}
	destination := filepath.Join(target, confirmed.Destination)
	for name, expected := range map[string][]byte{"repo/tracked.txt": []byte("dirty snapshot bytes"), "repo/untracked.txt": []byte("untracked snapshot bytes"), "assets/random.bin": data} {
		actual, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("restored bytes mismatch: %s %v", name, err)
		}
	}
	if got := runtimeSnapshotGit(t, filepath.Join(destination, "repo"), "rev-parse", "HEAD"); got != head {
		t.Fatal("native restore lost unpushed Git history")
	}
	if got := runtimeSnapshotGit(t, filepath.Join(destination, "repo"), "show", ":tracked.txt"); got != "staged snapshot bytes" {
		t.Fatal("native restore lost index state")
	}
	indexAfter, err := os.ReadFile(filepath.Join(repo, ".git/index"))
	if err != nil || !bytes.Equal(indexBefore, indexAfter) || runtimeSnapshotGit(t, repo, "status", "--porcelain=v1") != status {
		t.Fatal("restore modified active source Git state")
	}
	for root, expected := range map[string]string{source: "later active source edit", target: "existing target edit"} {
		name := "current.txt"
		if root == source {
			name = "repo/tracked.txt"
		}
		value, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(value) != expected {
			t.Fatal("restore overwrote source/current directory")
		}
	}
	write(destination, "repo/after-restore.txt", []byte("later restored copy edit"))
	if prior, err := service.Admit(t.Context(), "alice", input); err != nil || prior.ID != receipt.ID || prior.State != "restored" {
		t.Fatalf("lost accepted receipt: %+v %v", prior, err)
	}
	if value, err := os.ReadFile(filepath.Join(destination, "repo/after-restore.txt")); err != nil || string(value) != "later restored copy edit" || remote.finishes.Load() != finishes {
		t.Fatal("receipt retry overwrote later target edits")
	}
	var current models.SoloSession
	if err := p.db.Take(&current, "id = ?", solo.ID).Error; err != nil || current.SandboxID != sandboxID || current.LifecycleEpoch != solo.LifecycleEpoch || current.CurrentConfigurationID != solo.CurrentConfigurationID {
		t.Fatal("materialization falsely activated another workspace")
	}
}
