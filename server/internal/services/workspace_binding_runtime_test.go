package services

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func TestWorkspaceBindingLiveRuntimeCaptureRestoreActivateRestartAndReturn(t *testing.T) {
	binary := os.Getenv("GRASP_WORKSPACE_BINDING_FIXTURE_BINARY")
	if binary == "" {
		t.Skip("requires built native binding router fixture")
	}
	source, control, restores := t.TempDir(), t.TempDir(), t.TempDir()
	for _, root := range []string{control, restores} {
		if err := os.Chmod(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(root, name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(source, "marker", "saved original bytes")
	runtimeSnapshotGit(t, source, "init")
	runtimeSnapshotGit(t, source, "add", "marker")
	runtimeSnapshotGit(t, source, "commit", "-qm", "retained local commit")
	gitHead := runtimeSnapshotGit(t, source, "rev-parse", "HEAD")
	write(source, "binary.bin", strings.Repeat("\x00\xff", 1000))
	var stopNative func()
	start := func() sandbox.WorkspaceJobClient {
		t.Helper()
		coord := t.TempDir()
		ready, stop := filepath.Join(coord, "ready"), filepath.Join(coord, "stop")
		log, err := os.Create(filepath.Join(coord, "runtime.log"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "-test.run=^TestWorkspaceBindingProtocolFixture$", "-test.v")
		cmd.Env = append(os.Environ(), "GRASP_WORKSPACE_BINDING_FIXTURE=1", "GRASP_SNAPSHOT_TEST_SOURCE="+source, "GRASP_SNAPSHOT_TEST_CONTROL="+control, "GRASP_SNAPSHOT_TEST_RESTORE="+restores, "GRASP_SNAPSHOT_TEST_READY="+ready, "GRASP_SNAPSHOT_TEST_STOP="+stop)
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		ended := false
		stopNative = func() {
			if ended {
				return
			}
			ended = true
			_ = os.WriteFile(stop, nil, 0600)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					data, _ := os.ReadFile(log.Name())
					t.Errorf("native fixture: %v %s", err, data)
				}
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				t.Error("native fixture did not exit")
			}
			_ = log.Close()
		}
		t.Cleanup(stopNative)
		until := time.Now().Add(5 * time.Second)
		var address struct {
			Address string `json:"address"`
		}
		for {
			data, _ := os.ReadFile(ready)
			if json.Unmarshal(data, &address) == nil && address.Address != "" {
				break
			}
			if time.Now().After(until) {
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("native did not start: %s", data)
			}
			time.Sleep(10 * time.Millisecond)
		}
		host, portText, err := net.SplitHostPort(address.Address)
		if err != nil {
			t.Fatal(err)
		}
		port, _ := strconv.Atoi(portText)
		return sandbox.WorkspaceJobClient{Host: host, Port: port, Password: "snapshot-fixture-password", Timeout: 3 * time.Second}
	}
	client := start()
	base, _, _, revoked, sbid := publicationFixture(t, client, func(holds *payloadhold.Options, jobs *WorkspaceJobOptions) {
		holds.LeaseTTL, holds.RenewInterval = 45*time.Second, 15*time.Second
		jobs.LeaseTTL, jobs.RenewInterval, jobs.RemoteInterval = 45*time.Second, 15*time.Second, 5*time.Second
	})
	db := base.db
	if err := db.AutoMigrate(&models.WorkspaceBindingHead{}, &models.WorkspaceBinding{}, &models.WorkspaceBindingOperation{}, &models.WorkspaceReturnPoint{}, &models.WorkspaceBindingUse{}); err != nil {
		t.Fatal(err)
	}
	solo := models.SoloSession{ID: "solo-original", UserID: "alice", SandboxID: sbid, Status: models.SoloStatusRunning, LifecycleEpoch: 7, LifecycleAttemptID: "native-original", CurrentConfigurationID: "config-original"}
	if err := db.Create(&solo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Sandbox{}).Where("id = ?", sbid).Updates(map[string]any{"profile": "fixture-agent", "purpose": "solo", "thread_id": solo.ID, "desired_state": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	compose := func() (*WorkspaceBindingService, *WorkspaceRestoreService) {
		t.Helper()
		callbacks := restoreTestCallbacks(revoked)
		bindings, err := NewWorkspaceBindingService(db, base.Jobs.sandbox, WorkspaceBindingOptions{ExpectedLineage: "lineage-original", FileOperationsEnabled: true, Prepare: callbacks.Prepare, ReadScope: func(ctx context.Context, db *gorm.DB, actor, kind, id string) error {
			if kind != "solo" {
				return gorm.ErrRecordNotFound
			}
			var row models.SoloSession
			return workspaceCurrentRead(db.WithContext(ctx)).Select("id").Take(&row, "id = ? AND user_id = ?", id, actor).Error
		}, CurrentAuthorization: func(context.Context, string, string, string, uint) (string, error) {
			return strings.Repeat("e", 64), nil
		}, GuardTx: func(ctx context.Context, tx *gorm.DB, _ string, _ string, _ uint) error {
			if ctx.Value(restoreTestPermitKey{}) != true {
				return ErrWorkspaceJobAuthority
			}
			return nil
		}, Remote: func(context.Context, uint) (WorkspaceBindingRemote, error) { return client, nil }})
		if err != nil {
			t.Fatal(err)
		}
		jobs := base.Jobs.options
		jobs.Authorize = base.authorize
		jobs.AdmittedTx, jobs.CompletedTx = nil, nil
		jobs.Remote = func(context.Context, uint) (WorkspaceJobRemote, error) { return client, nil }
		jobs.PrepareJobAuthorization = func(ctx context.Context, _ models.WorkspaceSnapshotJob) (context.Context, error) {
			return callbacks.Prepare(ctx)
		}
		jobs, err = bindings.JobOptions(jobs)
		if err != nil {
			t.Fatal(err)
		}
		callbacks.CurrentBinding = bindings.CurrentBinding
		restored, err := NewWorkspaceRestoreService(db, base.Jobs.sandbox, base.holds, base.files, base.limits, jobs, callbacks)
		if err != nil {
			t.Fatal(err)
		}
		if err := bindings.AttachOperations(restored, &SoloService{db: db, durable: chatsession.NewDurableStore(db)}); err != nil {
			t.Fatal(err)
		}
		return bindings, restored
	}
	bindings, service := compose()
	current, err := bindings.Resolve(t.Context(), "alice", solo.ID)
	if err != nil {
		t.Fatal(err)
	}
	capture := func(nonce string, current WorkspaceActiveBinding) models.WorkspaceSnapshotJob {
		t.Helper()
		job, err := service.Pipeline.Capture(t.Context(), "alice", WorkspaceCaptureInput{ClientRequestID: nonce, ScopeKind: "solo", ScopeID: solo.ID, SandboxID: sbid, Paths: []string{"."}, Binding: &current.Reference, ExpectedBinding: current.Fingerprint, Duration: 90 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Jobs.RunOne(t.Context(), job.ID); err != nil {
			t.Fatal(err)
		}
		if err := service.Pipeline.QueuePublication(t.Context(), "alice", job.ID); err != nil {
			t.Fatal(err)
		}
		var row models.WorkspaceSnapshot
		if err := db.Take(&row, "id = ?", job.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := service.Jobs.RunOne(t.Context(), row.PublicationJobID); err != nil {
			t.Fatal(err)
		}
		return job
	}
	saved := capture("native-save-original", current)
	restored, err := service.Admit(t.Context(), "alice", WorkspaceRestoreInput{ClientRequestID: "native-isolated-copy", SoloID: solo.ID, SnapshotID: saved.ID, ExpectedBinding: current.Fingerprint, Duration: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Jobs.RunOne(t.Context(), restored.ID); err != nil {
		t.Fatal(err)
	}
	write(source, "marker", "later original edits preserved")
	returnCopy := capture("native-save-before-activate", current)
	input := WorkspaceBindingInput{ClientRequestID: "native-activate-explicit", SoloID: solo.ID, Action: "activate", RestoreID: restored.ID, ExpectedBinding: current.Fingerprint, ReturnSnapshotID: returnCopy.ID, Duration: 90 * time.Second}
	activated, err := bindings.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Jobs.RunOne(t.Context(), activated.ID); err != nil {
		t.Fatal(err)
	}
	active, err := client.WorkspaceBindingHead(t.Context())
	if err != nil || active.Head.Target.Kind != "restore" || active.Path != filepath.Join(restores, restored.JobID) {
		t.Fatal(active, err)
	}
	data, err := os.ReadFile(filepath.Join(active.Path, "marker"))
	if err != nil || string(data) != "saved original bytes" {
		t.Fatal(string(data), err)
	}
	data, err = os.ReadFile(filepath.Join(source, "marker"))
	if err != nil || string(data) != "later original edits preserved" {
		t.Fatal("original overwritten", string(data), err)
	}
	if runtimeSnapshotGit(t, active.Path, "rev-parse", "HEAD") != gitHead {
		t.Fatal("native activation lost Git history")
	}
	write(active.Path, "later.txt", "edits made after activation")
	// A real backend process exit/restart retains the same native head, operation
	// receipt and Return roots. This is not a provider RAM-resume assertion.
	stopNative()
	client = start()
	bindings, service = compose()
	originalReceipt, err := bindings.Admit(t.Context(), "alice", input)
	if err != nil || originalReceipt.ID != activated.ID || originalReceipt.State != "activated" {
		t.Fatal(originalReceipt, err)
	}
	afterRestart, err := client.WorkspaceBindingHead(t.Context())
	if err != nil || afterRestart.Head != active.Head || afterRestart.Path != active.Path {
		t.Fatal("persistent canonical head lost at restart", afterRestart, err)
	}
	current, err = bindings.Resolve(t.Context(), "alice", solo.ID)
	if err != nil {
		t.Fatal(err)
	}
	leaving := capture("native-save-before-return", current)
	back, err := bindings.Admit(t.Context(), "alice", WorkspaceBindingInput{ClientRequestID: "native-return-explicit", SoloID: solo.ID, Action: "return", TargetBindingID: activated.PreviousBindingID, ExpectedBinding: current.Fingerprint, ReturnSnapshotID: leaving.ID, Duration: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Jobs.RunOne(t.Context(), back.ID); err != nil {
		t.Fatal(err)
	}
	returned, err := client.WorkspaceBindingHead(t.Context())
	if err != nil || returned.Head.Target.Kind != "original" || returned.Path != source || returned.Head.Reference.Revision != 3 {
		t.Fatal(returned, err)
	}
	data, err = os.ReadFile(filepath.Join(active.Path, "later.txt"))
	if err != nil || string(data) != "edits made after activation" {
		t.Fatal("Return destroyed other root", string(data), err)
	}
	data, err = os.ReadFile(filepath.Join(source, "marker"))
	if err != nil || string(data) != "later original edits preserved" {
		t.Fatal("Return overwrote original", string(data), err)
	}
	t.Log("real collector -> object publication -> isolated native materializer -> explicit canonical Activate -> backend restart -> capture active root -> explicit Return passed; model requests=0; process memory restoration not claimed")
}
