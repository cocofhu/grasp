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
	"gorm.io/gorm/clause"
)

// A directory is a runtime-controlled basename beneath RestoreRoot. It is
// never a browser-provided path or evidence that the active workspace changed.
type workspaceRestoreReceipt struct {
	Version        int    `json:"version"`
	Directory      string `json:"directory"`
	ArchiveSHA256  string `json:"archiveSha256"`
	ManifestSHA256 string `json:"manifestSha256"`
	Files          int    `json:"files"`
	Bytes          int64  `json:"bytes"`
	Repositories   int    `json:"repositories"`
}

type workspaceRestoreCompletion struct {
	service   *WorkspaceRestoreService
	row       models.WorkspaceRestore
	job       models.WorkspaceSnapshotJob
	expected  workspaceRestoreReceipt
	plan      payloadhold.Plan
	validated bool
	native    sandbox.WorkspaceJobReceipt
}

func (s *WorkspaceRestoreService) restorePlan(ctx context.Context, job models.WorkspaceSnapshotJob) (*workspaceRestoreCompletion, error) {
	ctx, err := s.options.Prepare(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeJob(ctx, s.db.WithContext(ctx), job); err != nil {
		return nil, err
	}
	var row models.WorkspaceRestore
	if err := s.db.WithContext(ctx).Where("id = ? AND actor_id = ?", job.ID, job.ActorID).Take(&row).Error; err != nil {
		return nil, err
	}
	snapshot, specJSON, receiptJSON, identity, err := s.source(ctx, s.db.WithContext(ctx), row.ActorID, row.SnapshotID)
	if err != nil {
		return nil, err
	}
	if identity != row.SourceIdentity || specJSON != row.SourceSpec || receiptJSON != row.SourceReceipt {
		return nil, ErrWorkspaceJobConflict
	}
	var spec workspacefiles.Spec
	var receipt workspacefiles.Receipt
	if workspacefiles.Decode([]byte(specJSON), &spec) != nil || workspacefiles.Decode([]byte(receiptJSON), &receipt) != nil {
		return nil, ErrWorkspaceJobConflict
	}
	open := func(ref string, size int64) payloadhold.Opener {
		return func(ctx context.Context, offset int64) (io.ReadCloser, error) {
			if offset != 0 {
				return nil, ErrWorkspaceJobInvalid
			}
			return s.files.OpenBounded(ctx, ref, size)
		}
	}
	plan, err := s.holds.Stage(ctx, open(snapshot.ArchiveRef, receipt.Archive.Bytes), open(snapshot.ManifestRef, receipt.Manifest.Bytes), s.limits)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = plan.Close()
		}
	}()
	if _, err := workspacefiles.Verify(ctx, plan, spec, receipt); err != nil {
		return nil, err
	}
	manifest, err := plan.OpenManifest(ctx)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(manifest, receipt.Manifest.Bytes+1))
	err = errors.Join(err, manifest.Close())
	if err != nil {
		return nil, err
	}
	var value workspacefiles.Manifest
	if workspacefiles.Decode(data, &value) != nil {
		return nil, ErrWorkspaceJobConflict
	}
	keep = true
	return &workspaceRestoreCompletion{service: s, row: row, job: job, plan: plan, expected: workspaceRestoreReceipt{Version: 1, Directory: job.ID, ArchiveSHA256: receipt.Archive.SHA256, ManifestSHA256: receipt.Manifest.SHA256, Files: receipt.Files, Bytes: receipt.Bytes, Repositories: len(value.Repositories)}}, nil
}

func (s *WorkspaceRestoreService) Process(ctx context.Context, job models.WorkspaceSnapshotJob, remote WorkspaceJobRemote, fence func(context.Context) (sandbox.WorkspaceJobRenew, error)) (WorkspaceJobCompletion, error) {
	files, ok := remote.(WorkspaceFileRemote)
	if !ok {
		return nil, ErrWorkspaceJobUnsupported
	}
	caps, err := files.FileCapabilities(ctx)
	if err != nil || caps.Version != 1 || !caps.Restore || caps.PolicyVersion != workspacefiles.PolicyVersion || caps.Consistency != "verified_copy" || caps.MaxChunkBytes < sandbox.WorkspaceFileChunkBytes {
		return nil, errors.Join(ErrWorkspaceJobUnsupported, err)
	}
	completion, err := s.restorePlan(ctx, job)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = completion.Close()
		}
	}()
	summary, err := completion.plan.Summary()
	if err != nil {
		return nil, err
	}
	for _, part := range []struct {
		name string
		size int64
		open func(context.Context) (io.ReadSeekCloser, error)
	}{{"archive", summary.ArchiveBytes, completion.plan.OpenArchive}, {"manifest", summary.ManifestBytes, completion.plan.OpenManifest}} {
		reader, err := part.open(ctx)
		if err != nil {
			return nil, err
		}
		err = writeRestorePart(ctx, files, job.ID, part.name, part.size, reader, fence)
		err = errors.Join(err, reader.Close())
		if err != nil {
			return nil, err
		}
	}
	capability, err := fence(ctx)
	if err != nil {
		return nil, err
	}
	// Any ambiguous write/Finish leaves this job unknown. A future explicit
	// Reconcile only reads its original native receipt, never replays transfer.
	if err := files.FinishWorkspaceTransfer(ctx, job.ID, capability); err != nil {
		return nil, errors.Join(ErrWorkspaceJobUnknown, err)
	}
	keep = true
	return completion, nil
}

func writeRestorePart(ctx context.Context, remote WorkspaceFileRemote, id, part string, size int64, reader io.Reader, fence func(context.Context) (sandbox.WorkspaceJobRenew, error)) error {
	buffer := make([]byte, min(int64(sandbox.WorkspaceFileChunkBytes), size))
	for offset := int64(0); offset < size; {
		data := buffer[:min(int64(len(buffer)), size-offset)]
		if _, err := io.ReadFull(reader, data); err != nil {
			return err
		}
		backoff := 10 * time.Millisecond
		for {
			capability, err := fence(ctx)
			if err != nil {
				return err
			}
			err = remote.WriteWorkspaceChunk(ctx, id, capability, part, offset, data)
			if err == nil {
				break
			}
			var pending *sandbox.WorkspaceJobError
			if !errors.As(err, &pending) || pending.Status != http.StatusTooEarly {
				return errors.Join(ErrWorkspaceJobUnknown, err)
			}
			// 425 is a definitive not-yet-running response: no bytes accepted.
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return context.Cause(ctx)
			case <-timer.C:
			}
			backoff = min(backoff*2, time.Second)
		}
		offset += int64(len(data))
	}
	return nil
}

func (s *WorkspaceRestoreService) Recover(ctx context.Context, job models.WorkspaceSnapshotJob, native sandbox.WorkspaceJobReceipt) (WorkspaceJobCompletion, error) {
	completion, err := s.restorePlan(ctx, job)
	if err != nil {
		return nil, err
	}
	if err := completion.ValidateReceipt(native); err != nil {
		_ = completion.Close()
		return nil, err
	}
	return completion, nil
}

func (c *workspaceRestoreCompletion) ValidateReceipt(native sandbox.WorkspaceJobReceipt) error {
	c.validated = false
	var receipt workspaceRestoreReceipt
	if validateRuntimeJob(c.job, native) != nil || native.State != "completed" || workspacefiles.Decode(native.Result, &receipt) != nil || receipt != c.expected {
		return ErrWorkspaceJobUnknown
	}
	c.validated, c.native = true, native
	return nil
}

func (c *workspaceRestoreCompletion) PrepareTx(ctx context.Context, tx *gorm.DB) error {
	if !c.validated {
		return ErrWorkspaceJobUnknown
	}
	if err := workspaceRestoreTransaction(tx); err != nil {
		return err
	}
	if err := c.service.authorizeJob(ctx, tx, c.job); err != nil {
		return err
	}
	var locked models.WorkspaceRestore
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", c.row.ID).Take(&locked).Error; err != nil {
		return err
	}
	if locked.Fingerprint != c.row.Fingerprint || locked.SourceIdentity != c.row.SourceIdentity || locked.State != "queued" {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (c *workspaceRestoreCompletion) Commit(ctx context.Context, tx *gorm.DB) error {
	if !c.validated {
		return ErrWorkspaceJobUnknown
	}
	if err := workspaceRestoreTransaction(tx); err != nil {
		return err
	}
	if err := c.service.authorizeJob(ctx, tx, c.job); err != nil {
		return err
	}
	if c.service.completed != nil {
		if err := c.service.completed(ctx, tx, c.job, c.native); err != nil {
			return err
		}
	}
	updated := tx.Model(&models.WorkspaceRestore{}).Where("id = ? AND actor_id = ? AND job_id = ? AND fingerprint = ? AND source_identity = ? AND state = ?", c.row.ID, c.row.ActorID, c.job.ID, c.row.Fingerprint, c.row.SourceIdentity, "queued").Updates(map[string]any{"state": "restored", "destination": c.expected.Directory, "error": ""})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (c *workspaceRestoreCompletion) Close() error { return c.plan.Close() }
