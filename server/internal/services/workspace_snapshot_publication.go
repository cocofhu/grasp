package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

func snapshotHoldIdentity(row models.WorkspaceSnapshotJob, snapshotID string) payloadhold.Identity {
	return payloadhold.Identity{ScopeKind: row.ScopeKind, ScopeID: row.ScopeID, ActorID: row.ActorID, OperationID: row.ID, SnapshotID: snapshotID, Lineage: row.Lineage, Deadline: row.Deadline}
}

func (p *WorkspaceSnapshotPipeline) Process(ctx context.Context, row models.WorkspaceSnapshotJob, remote WorkspaceJobRemote, fence func(context.Context) (sandbox.WorkspaceJobRenew, error)) (WorkspaceJobCompletion, error) {
	snapshot, capture, spec, err := p.publicationSource(ctx, p.db.WithContext(ctx), row)
	if err != nil {
		return nil, err
	}
	files, ok := remote.(WorkspaceFileRemote)
	if !ok {
		return nil, ErrWorkspaceJobUnsupported
	}
	open := func(part string, size int64) payloadhold.Opener {
		return func(ctx context.Context, offset int64) (io.ReadCloser, error) {
			if offset < 0 || offset > size {
				return nil, ErrWorkspaceJobInvalid
			}
			work, cancel := context.WithCancel(ctx)
			return &workspaceArtifactReader{ctx: work, cancel: cancel, remote: files, id: row.ID, part: part, offset: offset, size: size, fence: fence}, nil
		}
	}
	plan, err := p.holds.Stage(ctx, open("archive", spec.Receipt.Archive.Bytes), open("manifest", spec.Receipt.Manifest.Bytes), p.limits)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = plan.Close()
		}
	}()
	var expected workspacefiles.Spec
	if workspacefiles.Decode([]byte(capture.Body), &expected) != nil {
		return nil, ErrWorkspaceJobConflict
	}
	proof, err := workspacefiles.Verify(ctx, plan, expected, spec.Receipt)
	if err != nil {
		return nil, err
	}
	lease, err := p.holds.BeginHold(ctx, snapshotHoldIdentity(row, snapshot.ID), plan)
	if err != nil {
		return nil, err
	}
	verified, err := p.holds.UploadAndVerify(ctx, lease, plan)
	if err != nil {
		return nil, err
	}
	capability, err := fence(ctx)
	if err != nil {
		return nil, err
	}
	// An ambiguous Finish is never automatically repeated. The verified hold
	// survives; explicit Reconcile may GET the exact original native receipt.
	if err := files.FinishWorkspaceTransfer(ctx, row.ID, capability); err != nil {
		return nil, errors.Join(ErrWorkspaceJobUnknown, err)
	}
	keep = true
	return &workspaceSnapshotPublication{pipeline: p, row: row, snapshot: snapshot, receipt: spec.Receipt, proof: proof, plan: plan, lease: lease, verified: verified}, nil
}

func (p *WorkspaceSnapshotPipeline) Recover(ctx context.Context, row models.WorkspaceSnapshotJob, native sandbox.WorkspaceJobReceipt) (WorkspaceJobCompletion, error) {
	snapshot, capture, spec, err := p.publicationSource(ctx, p.db.WithContext(ctx), row)
	if err != nil {
		return nil, err
	}
	var result workspacefiles.Receipt
	if native.State != "completed" || workspacefiles.Decode(native.Result, &result) != nil || result != spec.Receipt {
		return nil, ErrWorkspaceJobUnknown
	}
	id := snapshotHoldIdentity(row, snapshot.ID)
	held, err := p.holds.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, ErrWorkspaceJobInvalid
	}
	lease, verified, err := p.holds.RecoverHold(ctx, id, deadline)
	if err != nil {
		return nil, err
	}
	open := func(ref string, limit int64) payloadhold.Opener {
		return func(ctx context.Context, offset int64) (io.ReadCloser, error) {
			file, err := p.files.OpenBounded(ctx, ref, limit)
			if err != nil {
				return nil, err
			}
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				_ = file.Close()
				return nil, err
			}
			return file, nil
		}
	}
	plan, err := p.holds.Stage(ctx, open(held.ArchiveRef, p.limits.MaxArchiveBytes), open(held.ManifestRef, p.limits.MaxManifestBytes), p.limits)
	if err != nil {
		return nil, err
	}
	var expected workspacefiles.Spec
	if workspacefiles.Decode([]byte(capture.Body), &expected) != nil {
		_ = plan.Close()
		return nil, ErrWorkspaceJobConflict
	}
	proof, err := workspacefiles.Verify(ctx, plan, expected, spec.Receipt)
	if err != nil {
		_ = plan.Close()
		return nil, err
	}
	return &workspaceSnapshotPublication{pipeline: p, row: row, snapshot: snapshot, receipt: spec.Receipt, proof: proof, plan: plan, lease: lease, verified: verified}, nil
}

type workspaceSnapshotPublication struct {
	pipeline *WorkspaceSnapshotPipeline
	row      models.WorkspaceSnapshotJob
	snapshot models.WorkspaceSnapshot
	receipt  workspacefiles.Receipt
	proof    workspacefiles.Proof
	plan     payloadhold.Plan
	lease    payloadhold.Lease
	verified payloadhold.Verified
}

func (c *workspaceSnapshotPublication) ValidateReceipt(native sandbox.WorkspaceJobReceipt) error {
	var receipt workspacefiles.Receipt
	if native.State != "completed" || workspacefiles.Decode(native.Result, &receipt) != nil || receipt != c.receipt {
		return ErrWorkspaceJobUnknown
	}
	return nil
}

func (c *workspaceSnapshotPublication) Commit(ctx context.Context, tx *gorm.DB) error {
	if err := c.pipeline.authorizeJob(ctx, tx, c.row); err != nil {
		return err
	}
	snapshot, _, _, err := c.pipeline.publicationSource(ctx, tx, c.row)
	if err != nil {
		return err
	}
	if snapshot.State != "publishing" || snapshot.PublicationJobID != c.row.ID {
		return ErrWorkspaceJobConflict
	}
	pin, err := c.pipeline.holds.CommitPinTx(tx, c.lease, c.verified)
	if err != nil {
		return err
	}
	if pin.SnapshotID != snapshot.ID || !c.proof.Matches(pin) {
		return workspacefiles.ErrIntegrity
	}
	entries, bytes := c.proof.Counts()
	now := time.Now().UTC()
	updated := tx.Model(&models.WorkspaceSnapshot{}).Where("id = ? AND publication_job_id = ? AND state = ? AND lineage = ?", snapshot.ID, c.row.ID, "publishing", c.row.Lineage).Updates(map[string]any{"state": "ready", "archive_ref": pin.ArchiveRef, "manifest_ref": pin.ManifestRef, "archive_sha256": pin.ArchiveSHA256, "manifest_sha256": pin.ManifestSHA256, "bytes": bytes, "entries": int64(entries), "verified_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	return nil
}
func (c *workspaceSnapshotPublication) Close() error { return c.plan.Close() }

type workspaceArtifactReader struct {
	ctx          context.Context
	cancel       context.CancelFunc
	remote       WorkspaceFileRemote
	id, part     string
	offset, size int64
	buffer       []byte
	fence        func(context.Context) (sandbox.WorkspaceJobRenew, error)
}

func (r *workspaceArtifactReader) Close() error { r.cancel(); return nil }
func (r *workspaceArtifactReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if len(r.buffer) == 0 {
		if r.offset == r.size {
			return 0, io.EOF
		}
		backoff := 25 * time.Millisecond
		for {
			fence, err := r.fence(r.ctx)
			if err != nil {
				return 0, err
			}
			data, last, err := r.remote.ReadWorkspaceChunk(r.ctx, r.id, fence, r.part, r.offset, int(min(int64(sandbox.WorkspaceFileChunkBytes), r.size-r.offset)))
			if err == nil {
				if len(data) == 0 || int64(len(data)) > r.size-r.offset || last != (r.offset+int64(len(data)) == r.size) {
					return 0, workspacefiles.ErrIntegrity
				}
				r.buffer = data
				break
			}
			var failure *sandbox.WorkspaceJobError
			if errors.As(err, &failure) && failure.Status != http.StatusTooEarly && failure.Status < 500 {
				return 0, err
			}
			timer := time.NewTimer(backoff)
			select {
			case <-r.ctx.Done():
				timer.Stop()
				return 0, context.Cause(r.ctx)
			case <-timer.C:
			}
			backoff = min(backoff*2, time.Second)
		}
	}
	n := copy(buffer, r.buffer)
	r.buffer = r.buffer[n:]
	r.offset += int64(n)
	return n, nil
}
