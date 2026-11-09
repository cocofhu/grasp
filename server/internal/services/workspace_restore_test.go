package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type restoreFileRemote struct {
	*snapshotFileRemote
	parts                   map[string]map[string][]byte
	writes, restoreFinishes int
	lostRestoreFinish       bool
	beforeRestoreFinish     func()
}

func (r *restoreFileRemote) WriteWorkspaceChunk(_ context.Context, id string, fence sandbox.WorkspaceJobRenew, part string, offset int64, body []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[id]
	if job.Kind != "restore_files" || job.State != "running" || job.Generation != fence.Fence.Generation || !job.ExpiresAt.After(time.Now()) || len(body) > sandbox.WorkspaceFileChunkBytes {
		return ErrWorkspaceJobOwner
	}
	if r.parts[id] == nil {
		r.parts[id] = map[string][]byte{}
	}
	if int64(len(r.parts[id][part])) != offset {
		return ErrWorkspaceJobConflict
	}
	r.parts[id][part] = append(r.parts[id][part], body...)
	r.writes++
	return nil
}

func (r *restoreFileRemote) FinishWorkspaceTransfer(ctx context.Context, id string, fence sandbox.WorkspaceJobRenew) error {
	r.mu.Lock()
	kind := r.jobs[id].Kind
	r.mu.Unlock()
	if kind != "restore_files" {
		return r.snapshotFileRemote.FinishWorkspaceTransfer(ctx, id, fence)
	}
	if r.beforeRestoreFinish != nil {
		r.beforeRestoreFinish()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !bytes.Equal(r.parts[id]["archive"], r.archive) || !bytes.Equal(r.parts[id]["manifest"], r.manifest) {
		return errors.New("materializer received wrong immutable bytes")
	}
	r.restoreFinishes++
	native := r.jobs[id]
	native.State = "completed"
	native.Result, _ = json.Marshal(workspaceRestoreReceipt{Version: 1, Directory: id, ArchiveSHA256: r.receipt.Archive.SHA256, ManifestSHA256: r.receipt.Manifest.SHA256, Files: r.receipt.Files, Bytes: r.receipt.Bytes, Repositories: 0})
	r.jobs[id] = native
	if r.lostRestoreFinish {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type restoreTestPermitKey struct{}
type restoreTestFixture struct {
	service *WorkspaceRestoreService
	remote  *restoreFileRemote
	objects *snapshotObjects
	opts    WorkspaceJobOptions
	input   WorkspaceRestoreInput
	revoked *atomic.Bool
}

func restoreTestBinding(ctx context.Context, tx *gorm.DB, actor, id string) (WorkspaceRestoreBinding, error) {
	var solo models.SoloSession
	q := tx.WithContext(ctx).Select("id", "sandbox_id", "lifecycle_epoch", "current_configuration_id").Where("id = ? AND user_id = ?", id, actor)
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&solo).Error; err != nil {
		return WorkspaceRestoreBinding{}, err
	}
	return WorkspaceRestoreBinding{SandboxID: solo.SandboxID, Fingerprint: fmt.Sprintf("binding-%d-%d-%s", solo.SandboxID, solo.LifecycleEpoch, solo.CurrentConfigurationID)}, nil
}

func restoreTestCallbacks(revoked *atomic.Bool) WorkspaceRestoreOptions {
	return WorkspaceRestoreOptions{CurrentBinding: restoreTestBinding, AuthorizeSnapshot: func(ctx context.Context, tx *gorm.DB, actor string, snapshot models.WorkspaceSnapshot) error {
		if revoked.Load() || ctx.Value(restoreTestPermitKey{}) != true {
			return ErrWorkspaceJobAuthority
		}
		var owner models.SoloSession
		return tx.WithContext(ctx).Select("id").Where("id = ? AND user_id = ?", snapshot.ScopeID, actor).Take(&owner).Error
	}, Prepare: func(ctx context.Context) (context.Context, error) {
		if revoked.Load() {
			return ctx, ErrWorkspaceJobAuthority
		}
		return context.WithValue(ctx, restoreTestPermitKey{}, true), nil
	}, GuardTx: func(ctx context.Context, tx *gorm.DB, _ models.WorkspaceRestore) error {
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside || ctx.Value(restoreTestPermitKey{}) != true || revoked.Load() {
			return ErrWorkspaceJobAuthority
		}
		return tx.Exec("SELECT 1").Error
	}}

}

func restoreFixture(t *testing.T) *restoreTestFixture {
	t.Helper()
	r := &restoreFileRemote{snapshotFileRemote: snapshotArchiveFixture(t), parts: map[string]map[string][]byte{}}
	p, objects, _, revoked, sandboxID := publicationFixture(t, r)
	capture, publication := beginPublication(t, p, sandboxID)
	if err := p.Jobs.RunOne(t.Context(), publication.ID); err != nil {
		t.Fatal(err)
	}
	solo := models.SoloSession{ID: "solo-original", UserID: "alice", SandboxID: sandboxID, Status: models.SoloStatusRunning, LifecycleEpoch: 7, LifecycleAttemptID: "native-original", CurrentConfigurationID: "config-original"}
	if err := p.db.Create(&solo).Error; err != nil {
		t.Fatal(err)
	}
	jobs := p.Jobs.options
	jobs.Authorize, jobs.AdmittedTx, jobs.CompletedTx = p.authorize, nil, nil
	options := restoreTestCallbacks(revoked)
	s, err := NewWorkspaceRestoreService(p.db, p.Jobs.sandbox, p.holds, p.files, p.limits, jobs, options)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := restoreTestBinding(t.Context(), p.db, "alice", solo.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &restoreTestFixture{service: s, remote: r, objects: objects, opts: jobs, revoked: revoked, input: WorkspaceRestoreInput{ClientRequestID: "restore-original-request", SoloID: solo.ID, SnapshotID: capture.ID, ExpectedBinding: binding.Fingerprint, Duration: 20 * time.Second}}
}

func (f *restoreTestFixture) admit(t *testing.T) models.WorkspaceRestore {
	t.Helper()
	row, err := f.service.Admit(t.Context(), "alice", f.input)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestWorkspaceRestoreAdmissionIsDurableConcurrentAndDoesNotReadObjects(t *testing.T) {
	f := restoreFixture(t)
	second, err := NewWorkspaceRestoreService(f.service.db, f.service.Jobs.sandbox, f.service.holds, f.service.files, f.service.limits, f.opts, f.service.options)
	if err != nil {
		t.Fatal(err)
	}
	before := f.objects.gets.Load()
	var wg sync.WaitGroup
	errors := make(chan error, 16)
	for i := range 16 {
		wg.Go(func() {
			s := f.service
			if i%2 != 0 {
				s = second
			}
			row, err := s.Admit(t.Context(), "alice", f.input)
			if err == nil && (row.ID != row.JobID || row.State != "queued" || row.Destination != "") {
				err = fmt.Errorf("invalid durable restore receipt: %+v", row)
			}
			errors <- err
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range []any{&models.WorkspaceRestore{}, &models.WorkspaceSnapshotJob{}} {
		var count int64
		q := f.service.db.Model(model)
		if _, job := model.(*models.WorkspaceSnapshotJob); job {
			q = q.Where("kind = ?", "restore_files")
		}
		if err := q.Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("admission duplicated metadata: count=%d err=%v", count, err)
		}
	}
	if f.objects.gets.Load() != before || f.remote.writes != 0 || f.remote.restoreFinishes != 0 {
		t.Fatal("admission performed artifact/native I/O")
	}
}

func TestWorkspaceRestoreOriginalRequestIncludesSnapshotAndBindingAndReceiptSurvivesChange(t *testing.T) {
	f := restoreFixture(t)
	original := f.admit(t)
	for _, mutate := range []func(*WorkspaceRestoreInput){func(in *WorkspaceRestoreInput) { in.SnapshotID = "another-same-byte-snapshot" }, func(in *WorkspaceRestoreInput) { in.ExpectedBinding += "-different" }, func(in *WorkspaceRestoreInput) { in.Duration += time.Second }} {
		changed := f.input
		mutate(&changed)
		if _, err := f.service.Admit(t.Context(), "alice", changed); !errors.Is(err, ErrWorkspaceJobConflict) {
			t.Fatalf("changed original input was accepted: %v", err)
		}
	}
	f.revoked.Store(true)
	if err := f.service.db.Model(&models.SoloSession{}).Where("id = ?", original.SoloID).Update("sandbox_id", 9001).Error; err != nil {
		t.Fatal(err)
	}
	before := f.objects.gets.Load()
	for _, op := range []func() (models.WorkspaceRestore, error){func() (models.WorkspaceRestore, error) { return f.service.Get(t.Context(), "alice", original.ID) }, func() (models.WorkspaceRestore, error) { return f.service.Admit(t.Context(), "alice", f.input) }} {
		row, err := op()
		if err != nil || row.ID != original.ID || row.TargetSandboxID != original.TargetSandboxID {
			t.Fatalf("lost accepted receipt after changed binding/grant: %+v %v", row, err)
		}
	}
	if f.objects.gets.Load() != before {
		t.Fatal("receipt lookup opened storage")
	}
	if _, err := f.service.Get(t.Context(), "bob", original.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-actor receipt visible: %v", err)
	}
}

func TestWorkspaceRestoreRejectsGenericJobAdmissionAndRequiresAllAuthorizationHooks(t *testing.T) {
	f := restoreFixture(t)
	body, _ := json.Marshal(workspaceRestoreSpec{f.remote.receipt})
	_, err := f.service.Jobs.Admit(t.Context(), "alice", WorkspaceJobInput{ClientRequestID: "bypass-restore-service", ScopeKind: "solo", ScopeID: f.input.SoloID, SandboxID: 1, Kind: "restore_files", Body: body, Duration: time.Second})
	if !errors.Is(err, ErrWorkspaceJobInvalid) {
		t.Fatalf("generic job bypass admitted: %v", err)
	}
	for _, mutate := range []func(*WorkspaceRestoreOptions){func(o *WorkspaceRestoreOptions) { o.CurrentBinding = nil }, func(o *WorkspaceRestoreOptions) { o.AuthorizeSnapshot = nil }, func(o *WorkspaceRestoreOptions) { o.Prepare = nil }, func(o *WorkspaceRestoreOptions) { o.GuardTx = nil }} {
		opts := f.service.options
		mutate(&opts)
		if _, err := NewWorkspaceRestoreService(f.service.db, f.service.Jobs.sandbox, f.service.holds, f.service.files, f.service.limits, f.opts, opts); !errors.Is(err, ErrWorkspaceJobAuthority) {
			t.Fatalf("missing authorization hook allowed: %v", err)
		}
	}
}

func TestWorkspaceRestoreMaterializesOnlyAfterFullSourceVerificationAndNativeReceipt(t *testing.T) {
	f := restoreFixture(t)
	row := f.admit(t)
	puts := f.objects.puts.Load()
	if err := f.service.Jobs.RunOne(t.Context(), row.JobID); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Get(t.Context(), "alice", row.ID)
	if err != nil || result.State != "restored" || result.Destination != row.JobID {
		t.Fatalf("missing confirmed materialization: %+v %v", result, err)
	}
	var solo models.SoloSession
	if err := f.service.db.Take(&solo, "id = ?", row.SoloID).Error; err != nil {
		t.Fatal(err)
	}
	if solo.SandboxID != row.TargetSandboxID || solo.LifecycleEpoch != row.LifecycleEpoch || solo.CurrentConfigurationID != "config-original" || f.remote.restoreFinishes != 1 || f.remote.writes != 2 || f.objects.puts.Load() != puts {
		t.Fatal("copy changed active Solo or skipped exact immutable transfer")
	}
	if retry, err := f.service.Admit(t.Context(), "alice", f.input); err != nil || retry.ID != row.ID || retry.Destination != row.JobID || f.remote.restoreFinishes != 1 {
		t.Fatalf("retry re-executed restored operation: %+v %v", retry, err)
	}
}

func TestWorkspaceRestoreLostFinishACKOnlyExplicitlyReadsOriginalNativeReceipt(t *testing.T) {
	f := restoreFixture(t)
	f.remote.lostRestoreFinish = true
	row := f.admit(t)
	if err := f.service.Jobs.RunOne(t.Context(), row.JobID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatalf("lost Finish ACK was not unknown: %v", err)
	}
	unknown, err := f.service.Get(t.Context(), "alice", row.ID)
	if err != nil || unknown.State != "unknown" || unknown.Destination != "" {
		t.Fatalf("unknown operation claimed restored: %+v %v", unknown, err)
	}
	writes, finishes, creates := f.remote.writes, f.remote.restoreFinishes, f.remote.creates
	if err := f.service.Jobs.RunOne(t.Context(), row.JobID); !errors.Is(err, ErrWorkspaceJobOwner) {
		t.Fatalf("unknown restore automatically replayed: %v", err)
	}
	restarted, err := NewWorkspaceRestoreService(f.service.db, f.service.Jobs.sandbox, f.service.holds, f.service.files, f.service.limits, f.opts, f.service.options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := restarted.Reconcile(t.Context(), "alice", row.ID)
	if err != nil || result.State != "restored" || result.Destination != row.JobID {
		t.Fatalf("original native receipt did not reconcile: %+v %v", result, err)
	}
	if f.remote.writes != writes || f.remote.restoreFinishes != finishes || f.remote.creates != creates {
		t.Fatal("read-only reconciliation replayed native side effects")
	}
}

func TestWorkspaceRestoreRevokedSourceOrChangedTargetDoesNotDispatch(t *testing.T) {
	for _, kind := range []string{"source_grant", "target_binding"} {
		t.Run(kind, func(t *testing.T) {
			f := restoreFixture(t)
			row := f.admit(t)
			if kind == "source_grant" {
				f.revoked.Store(true)
			} else if err := f.service.db.Model(&models.SoloSession{}).Where("id = ?", row.SoloID).Update("current_configuration_id", "different-config").Error; err != nil {
				t.Fatal(err)
			}
			creates := f.remote.creates
			if err := f.service.Jobs.RunOne(t.Context(), row.JobID); err == nil {
				t.Fatal("invalid authorization dispatched restore")
			}
			if f.remote.creates != creates || f.remote.writes != 0 || f.remote.restoreFinishes != 0 {
				t.Fatal("rejected operation touched native materializer")
			}
		})
	}
}

func TestWorkspaceRestoreFinalFenceRejectsTargetOrSourceChanges(t *testing.T) {
	for _, kind := range []string{"target", "source", "target_model", "source_ref", "source_grant"} {
		t.Run(kind, func(t *testing.T) {
			f := restoreFixture(t)
			row := f.admit(t)
			f.remote.beforeRestoreFinish = func() {
				var err error
				switch kind {
				case "target":
					err = f.service.db.Model(&models.SoloSession{}).Where("id = ?", row.SoloID).Update("lifecycle_epoch", row.LifecycleEpoch+1).Error
				case "target_model":
					err = f.service.db.Model(&models.SoloSession{}).Where("id = ?", row.SoloID).Update("current_configuration_id", "new-active-route").Error
				case "source_ref":
					err = f.service.db.Table("workspace_snapshots").Where("id = ?", row.SnapshotID).Update("archive_ref", "fileref:v1/json/"+f.remote.receipt.Archive.SHA256).Error
				case "source_grant":
					f.revoked.Store(true)
				default:
					err = f.service.db.Table("workspace_snapshots").Where("id = ?", row.SnapshotID).Update("state", "captured").Error
				}
				if err != nil {
					t.Error(err)
				}
			}
			if err := f.service.Jobs.RunOne(t.Context(), row.JobID); err == nil {
				t.Fatal("stale final publication accepted")
			}
			result, err := f.service.Get(t.Context(), "alice", row.ID)
			if err != nil || result.State == "restored" || result.Destination != "" || f.remote.restoreFinishes != 1 {
				t.Fatalf("native copy falsely published under changed metadata: %+v %v", result, err)
			}
		})
	}
}

func TestWorkspaceRestoreCorruptObjectCannotReachMaterializer(t *testing.T) {
	f := restoreFixture(t)
	row := f.admit(t)
	f.objects.mu.Lock()
	for key, value := range f.objects.objects {
		body := bytes.Clone(value)
		body[0] ^= 1
		f.objects.objects[key] = body
	}
	f.objects.mu.Unlock()
	if err := f.service.Jobs.RunOne(t.Context(), row.JobID); err == nil {
		t.Fatal("corrupt source object accepted")
	}
	if f.remote.writes != 0 || f.remote.restoreFinishes != 0 {
		t.Fatal("corrupt source was partially transferred before full verification")
	}
}

func TestWorkspaceRestoreUnknownReconcileRejectsMismatchedNativeResultWithoutReplay(t *testing.T) {
	f := restoreFixture(t)
	f.remote.lostRestoreFinish = true
	row := f.admit(t)
	if err := f.service.Jobs.RunOne(t.Context(), row.JobID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal(err)
	}
	f.remote.mu.Lock()
	native := f.remote.jobs[row.JobID]
	var receipt workspaceRestoreReceipt
	if err := json.Unmarshal(native.Result, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.Directory = "other-job-directory"
	native.Result, _ = json.Marshal(receipt)
	f.remote.jobs[row.JobID] = native
	writes, finishes, creates := f.remote.writes, f.remote.restoreFinishes, f.remote.creates
	f.remote.mu.Unlock()
	if _, err := f.service.Reconcile(t.Context(), "alice", row.ID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatalf("mismatched original native result accepted: %v", err)
	}
	result, err := f.service.Get(t.Context(), "alice", row.ID)
	if err != nil || result.State != "unknown" || result.Destination != "" || f.remote.writes != writes || f.remote.restoreFinishes != finishes || f.remote.creates != creates {
		t.Fatal("failed read-only reconciliation mutated native files or claimed restored")
	}
}
