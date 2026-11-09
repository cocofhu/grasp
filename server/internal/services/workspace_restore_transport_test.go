package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type restoreChunkRemote struct {
	WorkspaceFileRemote
	write func(context.Context, string, sandbox.WorkspaceJobRenew, string, int64, []byte) error
}

func (r restoreChunkRemote) WriteWorkspaceChunk(ctx context.Context, id string, fence sandbox.WorkspaceJobRenew, part string, offset int64, body []byte) error {
	return r.write(ctx, id, fence, part, offset, body)
}

func TestWorkspaceRestoreChunkTransportIsBoundedAndNeverReplaysAmbiguousPUT(t *testing.T) {
	body := bytes.Repeat([]byte("native-binary\x00\xff"), 200000)
	var received []byte
	puts, fences := 0, 0
	fence := func(context.Context) (sandbox.WorkspaceJobRenew, error) {
		fences++
		return sandbox.WorkspaceJobRenew{}, nil
	}
	remote := restoreChunkRemote{write: func(_ context.Context, _ string, _ sandbox.WorkspaceJobRenew, part string, offset int64, data []byte) error {
		if part != "archive" || offset != int64(len(received)) || len(data) > 1<<20 {
			t.Fatal("unbounded or overlapping chunk")
		}
		puts++
		received = append(received, data...)
		return nil
	}}
	if err := writeRestorePart(t.Context(), remote, "job-original", "archive", int64(len(body)), bytes.NewReader(body), fence); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, received) || puts != 3 || fences != 3 {
		t.Fatal("streaming transfer skipped a chunk fence")
	}
	puts = 0
	remote.write = func(context.Context, string, sandbox.WorkspaceJobRenew, string, int64, []byte) error {
		puts++
		return io.ErrUnexpectedEOF
	}
	if err := writeRestorePart(t.Context(), remote, "job-original", "archive", int64(len(body)), bytes.NewReader(body), fence); !errors.Is(err, ErrWorkspaceJobUnknown) || puts != 1 {
		t.Fatalf("ambiguous PUT repeated: puts=%d err=%v", puts, err)
	}
}

func TestWorkspaceRestoreChunkTransportStopsOnLostFenceAndDefinitivePendingDeadline(t *testing.T) {
	puts := 0
	remote := restoreChunkRemote{write: func(context.Context, string, sandbox.WorkspaceJobRenew, string, int64, []byte) error {
		puts++
		return nil
	}}
	if err := writeRestorePart(t.Context(), remote, "job-original", "manifest", 1, strings.NewReader("a"), func(context.Context) (sandbox.WorkspaceJobRenew, error) {
		return sandbox.WorkspaceJobRenew{}, ErrWorkspaceJobOwner
	}); !errors.Is(err, ErrWorkspaceJobOwner) || puts != 0 {
		t.Fatal("lost lease still sent a native PUT")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	remote.write = func(context.Context, string, sandbox.WorkspaceJobRenew, string, int64, []byte) error {
		puts++
		cancel()
		return &sandbox.WorkspaceJobError{Status: 425}
	}
	err := writeRestorePart(ctx, remote, "job-original", "manifest", 1, strings.NewReader("a"), func(context.Context) (sandbox.WorkspaceJobRenew, error) { return sandbox.WorkspaceJobRenew{}, nil })
	if !errors.Is(err, context.Canceled) || puts != 1 {
		t.Fatalf("pending receiver ignored deadline: puts=%d err=%v", puts, err)
	}
}

func TestWorkspaceRestoreNativeReceiptRequiresExactBasenameDigestsAndCounts(t *testing.T) {
	expected := workspaceRestoreReceipt{Version: 1, Directory: strings.Repeat("a", 64), ArchiveSHA256: strings.Repeat("b", 64), ManifestSHA256: strings.Repeat("c", 64), Files: 3, Bytes: 4096, Repositories: 1}
	job := models.WorkspaceSnapshotJob{ID: expected.Directory, Kind: "restore_files", RuntimeSHA256: strings.Repeat("d", 64), RuntimeResource: "owned-resource", Lineage: "original-lineage", RuntimeGeneration: 7, Deadline: time.Now().UTC().Add(time.Minute)}
	native := sandbox.WorkspaceJobReceipt{Version: 1, ID: job.ID, Kind: job.Kind, SHA256: job.RuntimeSHA256, Resource: job.RuntimeResource, Lineage: job.Lineage, Generation: job.RuntimeGeneration, Deadline: job.Deadline, State: "completed"}
	c := &workspaceRestoreCompletion{expected: expected, job: job}
	if err := c.Commit(t.Context(), nil); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal("unvalidated completion could publish SQL")
	}
	for _, mutate := range []func(*workspaceRestoreReceipt){func(r *workspaceRestoreReceipt) { r.Version = 2 }, func(r *workspaceRestoreReceipt) { r.Directory = "../active-workspace" }, func(r *workspaceRestoreReceipt) { r.ArchiveSHA256 = strings.Repeat("d", 64) }, func(r *workspaceRestoreReceipt) { r.ManifestSHA256 = strings.Repeat("d", 64) }, func(r *workspaceRestoreReceipt) { r.Files++ }, func(r *workspaceRestoreReceipt) { r.Bytes++ }, func(r *workspaceRestoreReceipt) { r.Repositories++ }} {
		changed := expected
		mutate(&changed)
		body, _ := json.Marshal(changed)
		value := native
		value.Result = body
		if err := c.ValidateReceipt(value); !errors.Is(err, ErrWorkspaceJobUnknown) || c.validated {
			t.Fatal("mismatching native result accepted")
		}
	}
	body, _ := json.Marshal(expected)
	native.Result = body
	for _, mutate := range []func(*sandbox.WorkspaceJobReceipt){func(r *sandbox.WorkspaceJobReceipt) { r.ID = "other-original-job" }, func(r *sandbox.WorkspaceJobReceipt) { r.Generation++ }, func(r *sandbox.WorkspaceJobReceipt) { r.Resource = "other-resource" }, func(r *sandbox.WorkspaceJobReceipt) { r.SHA256 = strings.Repeat("e", 64) }, func(r *sandbox.WorkspaceJobReceipt) { r.Deadline = r.Deadline.Add(time.Second) }} {
		value := native
		mutate(&value)
		if err := c.ValidateReceipt(value); !errors.Is(err, ErrWorkspaceJobUnknown) {
			t.Fatal("matching inner result hid changed native job identity")
		}
	}
	if err := c.ValidateReceipt(native); err != nil || !c.validated {
		t.Fatal("exact native receipt rejected")
	}
	if err := c.Commit(t.Context(), &gorm.DB{}); !errors.Is(err, ErrWorkspaceJobInvalid) {
		t.Fatal("autocommit completion bypassed final publication transaction")
	}
	native.State = "unknown"
	if err := c.ValidateReceipt(native); !errors.Is(err, ErrWorkspaceJobUnknown) || c.validated {
		t.Fatal("unknown observation reused prior completion proof")
	}
}

func TestWorkspaceRestoreWrapperPreservesOtherComponentsAndTransactionHooks(t *testing.T) {
	f := restoreFixture(t)
	options := f.opts
	options.Validate = map[string]func(json.RawMessage) error{"custom_component": func(json.RawMessage) error { return nil }}
	custom := f.opts.Processors["read_snapshot"]
	options.Processors = map[string]WorkspaceJobProcessor{"custom_component": custom}
	admitted, completed := 0, 0
	options.AdmittedTx = func(_ context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok || row.Kind != "restore_files" {
			return ErrWorkspaceJobInvalid
		}
		admitted++
		return nil
	}
	options.CompletedTx = func(_ context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob, native sandbox.WorkspaceJobReceipt) error {
		if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok || row.Kind != "restore_files" || native.State != "completed" {
			return ErrWorkspaceJobInvalid
		}
		completed++
		return nil
	}
	service, err := NewWorkspaceRestoreService(f.service.db, f.service.Jobs.sandbox, f.service.holds, f.service.files, f.service.limits, options, f.service.options)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := options.Validate["restore_files"]; exists || len(options.Processors) != 1 || service.Jobs.options.Processors["custom_component"] != custom || service.Jobs.options.Validate["custom_component"] == nil {
		t.Fatal("constructor mutated caller maps or dropped a component")
	}
	row, err := service.Admit(t.Context(), "alice", f.input)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Jobs.RunOne(t.Context(), row.JobID); err != nil {
		t.Fatal(err)
	}
	if admitted != 1 || completed != 1 {
		t.Fatalf("transaction hooks lost: admitted=%d completed=%d", admitted, completed)
	}
}

func TestWorkspaceRestoreRejectsSourceScopeAccessAndTypedRawRefBeforeAnyIO(t *testing.T) {
	for _, kind := range []string{"scope_permission", "wrong_encoding", "wrong_digest"} {
		t.Run(kind, func(t *testing.T) {
			f := restoreFixture(t)
			before := f.objects.gets.Load()
			if kind == "scope_permission" {
				f.service.options.AuthorizeSnapshot = func(context.Context, *gorm.DB, string, models.WorkspaceSnapshot) error { return gorm.ErrRecordNotFound }
			} else {
				value := "fileref:v1/json/" + f.remote.receipt.Archive.SHA256
				if kind == "wrong_digest" {
					value = "fileref:v1/binary/" + strings.Repeat("f", 64)
				}
				if err := f.service.db.Table("workspace_snapshots").Where("id = ?", f.input.SnapshotID).Update("archive_ref", value).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.service.Admit(t.Context(), "alice", f.input); err == nil {
				t.Fatal("unauthorized or mismatched source accepted")
			}
			var count int64
			if err := f.service.db.Model(&models.WorkspaceRestore{}).Count(&count).Error; err != nil || count != 0 || f.objects.gets.Load() != before || f.remote.writes != 0 {
				t.Fatal("rejected metadata intent touched storage/native files")
			}
		})
	}
}

func TestWorkspaceRestoreSnapshotIDIsNotReducedToEqualArtifactBytes(t *testing.T) {
	f := restoreFixture(t)
	original := f.admit(t)
	columns := "actor_id,scope_kind,scope_id,run_id,state_run_id,node_id,iteration,run_attempt,provider_attempt,source_sandbox_id,source_generation,job_id,publication_job_id,lineage,state,policy_version,archive_ref,manifest_ref,archive_sha256,manifest_sha256,bytes,entries,verified_at"
	cloneID := "another-verified-ready-snapshot"
	if err := f.service.db.Exec("INSERT INTO workspace_snapshots (id,"+columns+") SELECT ?,"+columns+" FROM workspace_snapshots WHERE id = ?", cloneID, original.SnapshotID).Error; err != nil {
		t.Fatal(err)
	}
	changed := f.input
	changed.SnapshotID = cloneID
	if _, err := f.service.Admit(t.Context(), "alice", changed); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("same artifact bytes hid changed source ID")
	}
	changed.ClientRequestID = "different-explicit-request"
	if _, err := f.service.Admit(t.Context(), "alice", changed); err != nil {
		t.Fatal("valid equal-byte snapshot was not independently authorized", err)
	}
}

func TestWorkspaceRestoreCompletedHookFailureRollsBackFinalReceipt(t *testing.T) {
	f := restoreFixture(t)
	f.service.completed = func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob, sandbox.WorkspaceJobReceipt) error {
		return errors.New("required metadata hook refused")
	}
	row := f.admit(t)
	if err := f.service.Jobs.RunOne(t.Context(), row.JobID); err == nil {
		t.Fatal("failed metadata hook still published restored")
	}
	var persisted models.WorkspaceRestore
	if err := f.service.db.Take(&persisted, "id = ?", row.ID).Error; err != nil || persisted.State != "queued" || persisted.Destination != "" {
		t.Fatal("final restore publication was not atomic")
	}
}

func TestWorkspaceRestoreRejectsClientIdentitySeparatorAndNonFiniteDuration(t *testing.T) {
	f := restoreFixture(t)
	for _, change := range []func(*WorkspaceRestoreInput){func(in *WorkspaceRestoreInput) { in.SoloID += "\x00other" }, func(in *WorkspaceRestoreInput) { in.Duration = 0 }, func(in *WorkspaceRestoreInput) { in.Duration = 24 * time.Hour }, func(in *WorkspaceRestoreInput) { in.ExpectedBinding = "" }} {
		input := f.input
		change(&input)
		if _, err := f.service.Admit(t.Context(), "alice", input); !errors.Is(err, ErrWorkspaceJobInvalid) {
			t.Fatal("unbounded or ambiguous restore intent accepted")
		}
	}
}
