package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

type bindingOperationRemote struct {
	*restoreFileRemote
	bindingCaps             sandbox.WorkspaceBindingCapabilities
	view                    sandbox.WorkspaceBindingView
	operations              map[string]sandbox.WorkspaceBindingOperation
	heads                   map[string]sandbox.WorkspaceBindingHead
	lostACK, unknownJournal bool
	failBeforeIntent        bool
	headReadErr             error
	beforeSwitch            func()
	switches                int
}

func (r *bindingOperationRemote) WorkspaceBindingCapabilities(context.Context) (sandbox.WorkspaceBindingCapabilities, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bindingCaps, nil
}
func (r *bindingOperationRemote) WorkspaceBindingHead(context.Context) (sandbox.WorkspaceBindingView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.view, r.headReadErr
}
func (r *bindingOperationRemote) WorkspaceBindingOperation(_ context.Context, id string) (sandbox.WorkspaceBindingOperation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.operations[id]
	if !ok {
		return op, &sandbox.WorkspaceJobError{Status: 404}
	}
	return op, nil
}
func (r *bindingOperationRemote) Create(ctx context.Context, in sandbox.WorkspaceJobCreate) (sandbox.WorkspaceJobReceipt, error) {
	if !bindingJobKind(in.Spec.Kind) {
		return r.restoreFileRemote.Create(ctx, in)
	}
	if r.beforeSwitch != nil {
		r.beforeSwitch()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates++
	if r.failBeforeIntent {
		receipt := sandbox.WorkspaceJobReceipt{Version: 1, ID: in.Spec.ID, Kind: in.Spec.Kind, Resource: in.Spec.Resource, Lineage: in.Spec.Lineage, SHA256: in.SHA256, Generation: in.Fence.Generation, Deadline: in.Spec.Deadline, ExpiresAt: in.Fence.ExpiresAt, State: "failed", Error: "workspace consumers busy before intent"}
		r.jobs[receipt.ID] = receipt
		return receipt, nil
	}
	r.switches++
	var spec sandbox.WorkspaceBindingSpec
	if workspacefiles.Decode(in.Spec.Body, &spec) != nil || spec.Expected != r.view.Head.Reference {
		return sandbox.WorkspaceJobReceipt{}, ErrWorkspaceJobConflict
	}
	target := spec.Target
	if target.Kind == "binding" {
		target = r.heads[target.ID].Target
	}
	old := r.view.Head
	next := sandbox.WorkspaceBindingHead{Version: 1, Resource: in.Spec.Resource, Lineage: in.Spec.Lineage, Reference: sandbox.WorkspaceBindingReference{ID: spec.BindingID, Revision: old.Reference.Revision + 1}, Target: target, ParentID: old.Reference.ID}
	next.Reference.Fingerprint = nativeBindingFingerprint(next)
	result := sandbox.WorkspaceBindingResult{Version: 1, OperationID: in.Spec.ID, Previous: old, Active: next, ReturnSnapshotID: spec.ReturnSnapshotID, Consumers: []string{"agent", "capture", "code"}}
	now := time.Now().UTC()
	op := sandbox.WorkspaceBindingOperation{Version: 1, State: "committed", Result: &result, CreatedAt: now, CompletedAt: &now}
	op.Identity.ID = in.Spec.ID
	op.Identity.Kind = in.Spec.Kind
	op.Identity.Resource = in.Spec.Resource
	op.Identity.Lineage = in.Spec.Lineage
	op.Identity.SHA256 = in.SHA256
	op.Identity.Generation = in.Fence.Generation
	op.Identity.Deadline = in.Spec.Deadline
	r.operations[in.Spec.ID] = op
	r.heads[next.Reference.ID] = next
	r.view.Head = next
	native := sandbox.WorkspaceJobReceipt{Version: 1, ID: in.Spec.ID, Kind: in.Spec.Kind, Resource: in.Spec.Resource, Lineage: in.Spec.Lineage, SHA256: in.SHA256, Generation: in.Fence.Generation, Deadline: in.Spec.Deadline, ExpiresAt: in.Fence.ExpiresAt, State: "completed"}
	native.Result, _ = json.Marshal(result)
	if r.unknownJournal {
		native.State = "unknown"
		native.Result = nil
	}
	r.jobs[native.ID] = native
	if r.lostACK {
		return sandbox.WorkspaceJobReceipt{}, io.ErrUnexpectedEOF
	}
	return native, nil
}

type bindingOperationFixture struct {
	service  *WorkspaceBindingService
	restore  *WorkspaceRestoreService
	remote   *bindingOperationRemote
	input    WorkspaceBindingInput
	original *restoreTestFixture
}

func newBindingOperationFixture(t *testing.T) *bindingOperationFixture {
	t.Helper()
	f := restoreFixture(t)
	target := f.admit(t)
	if err := f.service.Jobs.RunOne(t.Context(), target.JobID); err != nil {
		t.Fatal(err)
	}
	db := f.service.db
	if err := db.AutoMigrate(&models.WorkspaceBindingHead{}, &models.WorkspaceBinding{}, &models.WorkspaceBindingOperation{}, &models.WorkspaceReturnPoint{}, &models.WorkspaceBindingUse{}); err != nil {
		t.Fatal(err)
	}
	var soloRow models.SoloSession
	if err := db.Take(&soloRow, "id = ?", f.input.SoloID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Sandbox{}).Where("id = ?", soloRow.SandboxID).Updates(map[string]any{"profile": "fixture-agent", "purpose": "solo", "thread_id": f.input.SoloID, "desired_state": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	head := sandbox.WorkspaceBindingHead{Version: 1, Resource: "resource-one", Lineage: "lineage-original", Reference: sandbox.WorkspaceBindingReference{ID: "original-native-binding", Revision: 1}, Target: sandbox.WorkspaceBindingSelector{Kind: "original"}}
	head.Reference.Fingerprint = nativeBindingFingerprint(head)
	remote := &bindingOperationRemote{restoreFileRemote: f.remote, bindingCaps: sandbox.WorkspaceBindingCapabilities{Version: 1, Resource: head.Resource, Lineage: head.Lineage, Activate: true, Return: true, DurableHead: true, PortOwnership: true, Consumers: []string{"agent", "capture", "code"}}, view: sandbox.WorkspaceBindingView{Head: head, Path: t.TempDir(), Ready: true}, operations: map[string]sandbox.WorkspaceBindingOperation{}, heads: map[string]sandbox.WorkspaceBindingHead{head.Reference.ID: head}}
	remote.caps.Jobs.Kinds = append(remote.caps.Jobs.Kinds, "activate_workspace", "return_workspace")
	callbacks := restoreTestCallbacks(f.revoked)
	options := WorkspaceBindingOptions{ExpectedLineage: head.Lineage, FileOperationsEnabled: true, Prepare: callbacks.Prepare, ReadScope: func(ctx context.Context, db *gorm.DB, actor, kind, id string) error {
		if kind != "solo" {
			return gorm.ErrRecordNotFound
		}
		var row models.SoloSession
		return workspaceCurrentRead(db.WithContext(ctx)).Select("id").Take(&row, "id = ? AND user_id = ?", id, actor).Error
	}, CurrentAuthorization: func(context.Context, string, string, string, uint) (string, error) {
		if f.revoked.Load() {
			return "", ErrWorkspaceJobAuthority
		}
		return strings.Repeat("e", 64), nil
	}, GuardTx: func(ctx context.Context, tx *gorm.DB, _ string, _ string, _ uint) error {
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside || ctx.Value(restoreTestPermitKey{}) != true || f.revoked.Load() {
			return ErrWorkspaceJobAuthority
		}
		return nil
	}, Remote: func(context.Context, uint) (WorkspaceBindingRemote, error) { return remote, nil }}
	service, err := NewWorkspaceBindingService(db, f.service.Jobs.sandbox, options)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.Resolve(t.Context(), "alice", f.input.SoloID)
	if err != nil {
		t.Fatal(err)
	}
	returned := captureBindingFixture(t, f.service.Pipeline, remote, current, "return-current-root")
	jobs := f.opts
	jobs.Remote = func(context.Context, uint) (WorkspaceJobRemote, error) { return remote, nil }
	jobs.LeaseTTL = 5 * time.Second
	jobs.RenewInterval = time.Second
	jobs.RemoteInterval = time.Second
	jobs.PrepareJobAuthorization = func(ctx context.Context, _ models.WorkspaceSnapshotJob) (context.Context, error) {
		return callbacks.Prepare(ctx)
	}
	jobs, err = service.JobOptions(jobs)
	if err != nil {
		t.Fatal(err)
	}
	callbacks.CurrentBinding = service.CurrentBinding
	restored, err := NewWorkspaceRestoreService(db, f.service.Jobs.sandbox, f.service.holds, f.service.files, f.service.limits, jobs, callbacks)
	if err != nil {
		t.Fatal(err)
	}
	solo := &SoloService{db: db, durable: chatsession.NewDurableStore(db)}
	if err := service.AttachOperations(restored, solo); err != nil {
		t.Fatal(err)
	}
	return &bindingOperationFixture{service: service, restore: restored, remote: remote, original: f, input: WorkspaceBindingInput{ClientRequestID: "activate-stable-request", SoloID: f.input.SoloID, Action: "activate", ExpectedBinding: current.Fingerprint, RestoreID: target.ID, ReturnSnapshotID: returned.ID, Duration: 20 * time.Second}}
}
func captureBindingFixture(t *testing.T, p *WorkspaceSnapshotPipeline, r *bindingOperationRemote, current WorkspaceActiveBinding, nonce string) models.WorkspaceSnapshotJob {
	t.Helper()
	r.mu.Lock()
	var manifest workspacefiles.Manifest
	if err := json.Unmarshal(r.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Binding = &current.Reference
	r.manifest, _ = json.Marshal(manifest)
	sum := sha256.Sum256(r.manifest)
	r.receipt.Manifest.SHA256 = hex.EncodeToString(sum[:])
	r.receipt.Manifest.Bytes = int64(len(r.manifest))
	r.mu.Unlock()
	capture, err := p.Capture(t.Context(), "alice", WorkspaceCaptureInput{ClientRequestID: nonce, ScopeKind: "solo", ScopeID: "solo-original", SandboxID: current.SandboxID, Paths: []string{"."}, Binding: &current.Reference, ExpectedBinding: current.Fingerprint, Duration: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Jobs.RunOne(t.Context(), capture.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.QueuePublication(t.Context(), "alice", capture.ID); err != nil {
		t.Fatal(err)
	}
	var snapshot models.WorkspaceSnapshot
	if err := p.db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.Jobs.RunOne(t.Context(), snapshot.PublicationJobID); err != nil {
		t.Fatal(err)
	}
	return capture
}

func TestWorkspaceBindingOperationNonceIsDurableAndNativeTerminalCompletionPublishesAllMetadata(t *testing.T) {
	f := newBindingOperationFixture(t)
	before := f.original.objects.gets.Load()
	op, err := f.service.Admit(t.Context(), "alice", f.input)
	if err != nil {
		t.Fatal(err)
	}
	if f.original.objects.gets.Load() != before || f.remote.switches != 0 {
		t.Fatal("admission performed object/native writes")
	}
	if err := f.restore.Jobs.RunOne(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.service.GetOperation(t.Context(), "alice", f.input.SoloID, op.ID)
	if err != nil || got.State != "activated" {
		t.Fatal(got, err)
	}
	var head models.WorkspaceBindingHead
	if err := f.service.db.Take(&head, "sandbox_id = ?", op.SandboxID).Error; err != nil || head.PendingOperationID != "" || head.LastOperationID != op.ID || head.BindingID != op.BindingID || head.Revision != 2 {
		t.Fatal(head, err)
	}
	var solo models.SoloSession
	if err := f.service.db.Take(&solo, "id = ?", op.SoloID).Error; err != nil || !solo.ConversationContextPending {
		t.Fatal(solo, err)
	}
	f.original.revoked.Store(true)
	duplicate, err := f.service.Admit(t.Context(), "alice", f.input)
	if err != nil || duplicate.ID != op.ID || duplicate.State != "activated" || f.remote.switches != 1 {
		t.Fatal("ACK recovery required a new permission or switch", duplicate, err)
	}
	changed := f.input
	changed.ReturnSnapshotID = "other-ready-snapshot"
	if _, err := f.service.Admit(t.Context(), "alice", changed); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("changed nonce accepted", err)
	}
	if _, err := f.service.GetOperation(t.Context(), "bob", op.SoloID, op.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross actor receipt leak", err)
	}
}
func TestWorkspaceBindingOperationLostACKAndUnknownNativeJournalOnlyExplicitlyReadOriginalReceipt(t *testing.T) {
	f := newBindingOperationFixture(t)
	f.remote.lostACK = true
	f.remote.unknownJournal = true
	op, err := f.service.Admit(t.Context(), "alice", f.input)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.restore.Jobs.RunOne(t.Context(), op.ID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal(err)
	}
	if _, err := f.service.Resolve(t.Context(), "alice", op.SoloID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal("unknown head opened to new work", err)
	}
	got, err := f.service.GetOperation(t.Context(), "alice", op.SoloID, op.ID)
	if err != nil || got.State != "unknown" {
		t.Fatal(got, err)
	}
	if f.remote.switches != 1 {
		t.Fatal(f.remote.switches)
	}
	got, err = f.service.Reconcile(t.Context(), "alice", op.SoloID, op.ID)
	if err != nil || got.State != "activated" || f.remote.switches != 1 {
		t.Fatal("reconcile replayed switch or lost committed proof", got, err)
	}
	native, _ := f.remote.Get(t.Context(), op.ID)
	if native.State != "unknown" {
		t.Fatal("reconcile rewrote runtime journal", native)
	}
}
func TestWorkspaceBindingOperationConcurrentAdmissionCreatesOneBarrier(t *testing.T) {
	f := newBindingOperationFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() { _, err := f.service.Admit(t.Context(), "alice", f.input); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := f.service.db.Model(&models.WorkspaceBindingOperation{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := f.service.db.Model(&models.WorkspaceSnapshotJob{}).Where("kind IN ?", []string{"activate_workspace", "return_workspace"}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestWorkspaceBindingReturnPublishesNewLogicalRevisionAndRetainsBothRootsAndSnapshots(t *testing.T) {
	f := newBindingOperationFixture(t)
	first, err := f.service.Admit(t.Context(), "alice", f.input)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.restore.Jobs.RunOne(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	current, err := f.service.Resolve(t.Context(), "alice", first.SoloID)
	if err != nil {
		t.Fatal(err)
	}
	leaving := captureBindingFixture(t, f.original.service.Pipeline, f.remote, current, "save-before-return")
	input := WorkspaceBindingInput{ClientRequestID: "return-stable-request", SoloID: first.SoloID, Action: "return", ExpectedBinding: current.Fingerprint, TargetBindingID: first.PreviousBindingID, ReturnSnapshotID: leaving.ID, Duration: 20 * time.Second}
	returned, err := f.service.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.restore.Jobs.RunOne(t.Context(), returned.ID); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.service.GetOperation(t.Context(), "alice", returned.SoloID, returned.ID)
	if err != nil || receipt.State != "returned" {
		t.Fatal(receipt, err)
	}
	view, err := f.service.CurrentView(t.Context(), "alice", returned.SoloID)
	if err != nil || view.Revision != 3 || view.ID == first.PreviousBindingID || view.ID != returned.BindingID {
		t.Fatal(view, err)
	}
	for _, root := range view.RetainedBindings {
		if root.ID == first.PreviousBindingID {
			t.Fatal("same physical original directory offered as a different Return", root)
		}
	}
	var points, roots int64
	if err := f.service.db.Model(&models.WorkspaceReturnPoint{}).Count(&points).Error; err != nil || points != 2 {
		t.Fatal(points, err)
	}
	if err := f.service.db.Model(&models.WorkspaceBinding{}).Count(&roots).Error; err != nil || roots != 3 {
		t.Fatal(roots, err)
	}
	if f.remote.switches != 2 {
		t.Fatal("Return replays extra native operations", f.remote.switches)
	}
}
func TestWorkspaceBindingAdmissionRejectsConnectionsOtherJobsAndQueuedConversationWithoutNativeSideEffects(t *testing.T) {
	for _, kind := range []string{"connection", "other_job", "queued_turn"} {
		t.Run(kind, func(t *testing.T) {
			f := newBindingOperationFixture(t)
			current, err := f.service.CurrentBinding(t.Context(), f.service.db, "alice", f.input.SoloID)
			if err != nil {
				t.Fatal(err)
			}
			sbid := current.SandboxID
			switch kind {
			case "connection":
				if err := f.service.db.Create(&models.WorkspaceBindingUse{ID: "live-ide-connection", SandboxID: sbid, State: "active", Kind: "ide", Owner: "other-server"}).Error; err != nil {
					t.Fatal(err)
				}
			case "other_job":
				if err := f.service.db.Create(&models.WorkspaceSnapshotJob{ID: "different-unknown-job", SandboxID: sbid, State: "unknown"}).Error; err != nil {
					t.Fatal(err)
				}
			case "queued_turn":
				scope := soloQueueScope(f.input.SoloID)
				intent := chatsession.QueueIntent{ID: "accepted-pending-turn", ActorID: "alice", ClientMessageID: "pending-user-message", Fingerprint: strings.Repeat("c", 64), Payload: []byte("explicit pending user message")}
				if _, err := f.service.operations.Load().solo.durable.Admit(t.Context(), scope, intent, 0, nil); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.service.Admit(t.Context(), "alice", f.input); err == nil {
				t.Fatal("switch crossed busy consumer", kind)
			}
			var count int64
			if err := f.service.db.Model(&models.WorkspaceBindingOperation{}).Count(&count).Error; err != nil || count != 0 || f.remote.switches != 0 {
				t.Fatal(count, err)
			}
		})
	}
}
func TestWorkspaceBindingFinalPublicationRejectsChangedSourceGrantEpochOrHeadAndKeepsOriginalBarrier(t *testing.T) {
	for _, kind := range []string{"source", "grant", "epoch", "native_result"} {
		t.Run(kind, func(t *testing.T) {
			f := newBindingOperationFixture(t)
			op, err := f.service.Admit(t.Context(), "alice", f.input)
			if err != nil {
				t.Fatal(err)
			}
			f.remote.beforeSwitch = func() {
				switch kind {
				case "source":
					if err := f.service.db.Table("workspace_snapshots").Where("id = ?", op.ReturnSnapshotID).Update("archive_ref", "changed-raw-ref").Error; err != nil {
						t.Fatal(err)
					}
				case "grant":
					f.original.revoked.Store(true)
				case "epoch":
					if err := f.service.db.Model(&models.SoloSession{}).Where("id = ?", op.SoloID).Update("lifecycle_epoch", op.LifecycleEpoch+1).Error; err != nil {
						t.Fatal(err)
					}
				case "native_result":
					f.remote.bindingCaps.PortOwnership = false
				}
			}
			if err := f.restore.Jobs.RunOne(t.Context(), op.ID); err == nil {
				t.Fatal("stale operation published", kind)
			}
			var head models.WorkspaceBindingHead
			if err := f.service.db.Take(&head, "sandbox_id = ?", op.SandboxID).Error; err != nil || head.BindingID != op.PreviousBindingID || head.PendingOperationID != op.ID || head.Revision != 1 {
				t.Fatal("lost original unconfirmed barrier", head, err)
			}
			got, err := f.service.GetOperation(t.Context(), "alice", op.SoloID, op.ID)
			if err != nil || got.State == "activated" || got.State == "returned" {
				t.Fatal(got, err)
			}
		})
	}
}

func TestWorkspaceBindingConcurrentDifferentNoncesCannotOpenTwoSwitchesForOneExpectedHead(t *testing.T) {
	f := newBindingOperationFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, nonce := range []string{"first-explicit-action", "second-explicit-action"} {
		go func(nonce string) {
			<-start
			input := f.input
			input.ClientRequestID = nonce
			_, err := f.service.Admit(t.Context(), "alice", input)
			results <- err
		}(nonce)
	}
	close(start)
	var success, failed int
	for range 2 {
		if err := <-results; err == nil {
			success++
		} else {
			failed++
		}
	}
	if success != 1 || failed != 1 {
		t.Fatal("concurrent stale head opened multiple switches", success, failed)
	}
	var count int64
	if err := f.service.db.Model(&models.WorkspaceBindingOperation{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestWorkspaceBindingExplicitReconcileClearsOnlyDefinitivelyUndispatchedFailure(t *testing.T) {
	f := newBindingOperationFixture(t)
	op, err := f.service.Admit(t.Context(), "alice", f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.remote.caps.Jobs.Kinds = []string{"capture_files", "read_snapshot", "restore_files"}
	if err := f.restore.Jobs.RunOne(t.Context(), op.ID); err == nil {
		t.Fatal("unsupported native operation dispatched")
	}
	var job models.WorkspaceSnapshotJob
	if err := f.service.db.Take(&job, "id = ?", op.ID).Error; err != nil || job.State != "failed" || job.DispatchedAt != nil {
		t.Fatal(job, err)
	}
	got, err := f.service.Reconcile(t.Context(), "alice", op.SoloID, op.ID)
	if err != nil || got.State != "failed" {
		t.Fatal(got, err)
	}
	var head models.WorkspaceBindingHead
	if err := f.service.db.Take(&head, "sandbox_id = ?", op.SandboxID).Error; err != nil || head.State != "ready" || head.PendingOperationID != "" || head.BindingID != op.PreviousBindingID || head.Revision != 1 || f.remote.switches != 0 {
		t.Fatal("no-dispatch reconciliation changed native root", head, err)
	}
	if _, err := f.service.Reconcile(t.Context(), "alice", op.SoloID, op.ID); err != nil {
		t.Fatal("original failure receipt reconciliation not idempotent", err)
	}
}

func TestWorkspaceBindingReconcileProvesNativeFailedBeforeIntentAndRejectsUnknownOrChangedHead(t *testing.T) {
	for _, kind := range []string{"unchanged", "unknown_intent", "different_head", "network_error"} {
		t.Run(kind, func(t *testing.T) {
			f := newBindingOperationFixture(t)
			f.remote.failBeforeIntent = true
			op, err := f.service.Admit(t.Context(), "alice", f.input)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.restore.Jobs.RunOne(t.Context(), op.ID); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unknown_intent":
				f.remote.operations[op.ID] = sandbox.WorkspaceBindingOperation{Version: 1, State: "unknown"}
			case "different_head":
				f.remote.view.Head.Reference.ID = "unexpected-native-head"
				f.remote.view.Head.Reference.Fingerprint = nativeBindingFingerprint(f.remote.view.Head)
			case "network_error":
				f.remote.headReadErr = io.ErrUnexpectedEOF
			}
			view, err := f.service.OperationView(t.Context(), "alice", op.SoloID, op.ID)
			if err != nil || !view.RequiresReconciliation || view.State != "failed" {
				t.Fatal("failure with root barrier omitted reconcile action", view, err)
			}
			_, err = f.service.Reconcile(t.Context(), "alice", op.SoloID, op.ID)
			if kind == "unchanged" && err != nil || kind != "unchanged" && err == nil {
				t.Fatal(kind, err)
			}
			var head models.WorkspaceBindingHead
			if err := f.service.db.Take(&head, "sandbox_id = ?", op.SandboxID).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "unchanged" && (head.PendingOperationID != "" || head.State != "ready") || kind != "unchanged" && head.PendingOperationID != op.ID || head.BindingID != op.PreviousBindingID || f.remote.switches != 0 {
				t.Fatal("reconciliation lost original head or replayed work", head, f.remote.switches)
			}
		})
	}
}
