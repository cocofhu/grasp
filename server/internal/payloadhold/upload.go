package payloadhold

import (
	"context"
	"errors"
	"math"
	"os"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/leasewatch"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Verified binds two full read-backs to the exact hold generation, immutable
// plan, execution lineage, storage instance and namespace. It is not serializable
// and cannot be reconstructed from request fields or persisted reference text.
type Verified struct {
	lease             *leaseData
	archive, manifest filestore.Verified
}

type Pinned struct {
	HoldID, SnapshotID            string
	ArchiveRef, ManifestRef       string
	ArchiveSHA256, ManifestSHA256 string
	ArchiveBytes, ManifestBytes   int64
}

func (m *Manager) ownerQuery(db *gorm.DB, lease Lease) *gorm.DB {
	l := lease.data
	return db.Model(&models.PayloadUploadHold{}).Where("id = ? AND owner = ? AND generation = ? AND plan_sha256 = ? AND lineage = ? AND storage_fingerprint = ? AND expires_at > ? AND deadline > ? AND state IN ?", l.id, l.owner, l.generation, l.plan, l.identity.Lineage, m.options.StorageFingerprint, time.Now().UTC(), time.Now().UTC(), []string{StateUploading, StateVerified})
}

func (m *Manager) readOwned(ctx context.Context, lease Lease) (models.PayloadUploadHold, error) {
	var row models.PayloadUploadHold
	if !m.validLease(lease) {
		return row, ErrInvalid
	}
	if err := m.ownerQuery(m.db.WithContext(ctx), lease).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, ErrOwnerLost
		}
		return row, err
	}
	_, err := m.validateRow(row, lease.data.identity)
	return row, err
}

func (m *Manager) renew(ctx context.Context, lease Lease) (time.Time, error) {
	until := minTime(time.Now().UTC().Add(m.options.LeaseTTL), lease.data.deadline)
	if !until.After(time.Now()) {
		return time.Time{}, ErrOwnerLost
	}
	err := m.publication(ctx, lease.data.identity, func(tx *gorm.DB) error {
		result := m.ownerQuery(tx, lease).Update("expires_at", until)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOwnerLost
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return until, nil
}

func (m *Manager) withLease(ctx context.Context, lease Lease, fn func(context.Context) (Verified, error)) (Verified, error) {
	if !m.validLease(lease) {
		return Verified{}, ErrInvalid
	}
	ctx, stopDeadline := context.WithDeadline(ctx, lease.data.deadline)
	defer stopDeadline()
	work, stopWork := context.WithCancelCause(ctx)
	defer stopWork(context.Canceled)
	watch, stopWatch := context.WithCancel(work)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		err := leasewatch.Maintain(watch, lease.data.expiresAt, m.options.RenewInterval, ErrOwnerLost, func(ctx context.Context) (time.Time, error) {
			return m.renew(ctx, lease)
		})
		if err != nil && watch.Err() == nil {
			stopWork(err)
		}
	}()
	verified, err := fn(work)
	if work.Err() != nil {
		err = errors.Join(err, context.Cause(work))
	}
	stopWatch()
	<-joined
	if err != nil {
		// This is status recording only. It never releases bytes or roots and
		// cannot make the failed generation eligible for an automatic replay.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = m.publication(recordCtx, lease.data.identity, func(tx *gorm.DB) error {
			return m.ownerQuery(tx, lease).Updates(map[string]any{"state": StateUnknown, "error": "upload or verification did not complete"}).Error
		})
		return Verified{}, err
	}
	return verified, nil
}

func (m *Manager) recordPart(ctx context.Context, lease Lease, proof filestore.Verified, archive bool, row models.PayloadUploadHold) error {
	ref, err := m.files.VerifiedRef(proof)
	if err != nil {
		return err
	}
	column, verifiedColumn, expected, size := "manifest_ref", "manifest_verified_at", rawRef(filestore.JSON, row.ManifestSHA256), row.ManifestBytes
	if archive {
		column, verifiedColumn, expected, size = "archive_ref", "archive_verified_at", rawRef(filestore.Binary, row.ArchiveSHA256), row.ArchiveBytes
	}
	if ref != expected || proof.Size() != size {
		return ErrConflict
	}
	return m.publication(ctx, lease.data.identity, func(tx *gorm.DB) error {
		result := m.ownerQuery(tx, lease).Where("("+column+" = '' OR "+column+" IS NULL OR "+column+" = ?)", ref).Updates(map[string]any{column: ref, verifiedColumn: time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOwnerLost
		}
		return nil
	})
}

func (m *Manager) verifyPart(ctx context.Context, lease Lease, row models.PayloadUploadHold, archive bool, limit int64) (filestore.Verified, error) {
	ref, encoding := rawRef(filestore.JSON, row.ManifestSHA256), filestore.JSON
	if archive {
		ref, encoding = rawRef(filestore.Binary, row.ArchiveSHA256), filestore.Binary
	}
	proof, err := m.files.VerifyBounded(ctx, ref, encoding, limit)
	if err != nil {
		return filestore.Verified{}, err
	}
	if err := m.recordPart(ctx, lease, proof, archive, row); err != nil {
		return filestore.Verified{}, err
	}
	return proof, nil
}

func (m *Manager) uploadPart(ctx context.Context, lease Lease, row models.PayloadUploadHold, part stagedPart, archive bool, limit int64) (filestore.Verified, error) {
	file, err := os.Open(part.path)
	if err != nil {
		return filestore.Verified{}, err
	}
	defer func() { _ = file.Close() }()
	encoding := filestore.JSON
	if archive {
		encoding = filestore.Binary
	}
	prepared, err := m.files.PrepareBounded(ctx, file, encoding, limit)
	if err != nil {
		return filestore.Verified{}, err
	}
	ref, err := m.files.PreparedRef(prepared)
	if err != nil {
		return filestore.Verified{}, err
	}
	if ref != rawRef(encoding, part.sha256) || prepared.Size() != part.size {
		return filestore.Verified{}, ErrConflict
	}
	return m.verifyPart(ctx, lease, row, archive, limit)
}

func (m *Manager) finishVerified(ctx context.Context, lease Lease, archive, manifest filestore.Verified) (Verified, error) {
	row, err := m.readOwned(ctx, lease)
	if err != nil {
		return Verified{}, err
	}
	if row.ArchiveRef == "" || row.ManifestRef == "" {
		return Verified{}, ErrInvalid
	}
	err = m.publication(ctx, lease.data.identity, func(tx *gorm.DB) error {
		result := m.ownerQuery(tx, lease).Where("archive_ref = ? AND manifest_ref = ?", archive.Ref(), manifest.Ref()).Update("state", StateVerified)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOwnerLost
		}
		return nil
	})
	if err != nil {
		return Verified{}, err
	}
	return Verified{lease: lease.data, archive: archive, manifest: manifest}, nil
}

// UploadAndVerify uses a hold exactly once. No cursor, transaction or metadata
// lock is held while uploading or fully reading back either immutable object.
func (m *Manager) UploadAndVerify(ctx context.Context, lease Lease, plan Plan) (Verified, error) {
	if err := m.outsideTransaction(); err != nil {
		return Verified{}, err
	}
	if !m.validLease(lease) {
		return Verified{}, ErrInvalid
	}
	if err := m.verifyAuthority(ctx, lease.data.identity); err != nil {
		return Verified{}, err
	}
	a, b, limits, err := plan.details(m)
	if err != nil || planHash(lease.data.identity, m.options.StorageFingerprint, a, b, limits) != lease.data.plan {
		return Verified{}, ErrConflict
	}
	if !lease.data.started.CompareAndSwap(false, true) {
		return Verified{}, ErrReconcile
	}
	return m.withLease(ctx, lease, func(ctx context.Context) (Verified, error) {
		row, err := m.readOwned(ctx, lease)
		if err != nil {
			return Verified{}, err
		}
		archive, err := m.uploadPart(ctx, lease, row, a, true, limits.MaxArchiveBytes)
		if err != nil {
			return Verified{}, err
		}
		manifest, err := m.uploadPart(ctx, lease, row, b, false, limits.MaxManifestBytes)
		if err != nil {
			return Verified{}, err
		}
		return m.finishVerified(ctx, lease, archive, manifest)
	})
}

// RecoverHold is an explicit, authorized read-only reconciliation. It may
// acquire an expired generation and verify exactly its two planned objects;
// it never PUTs, changes the plan, or restarts/renews a native sandbox job.
// A new bounded deadline applies only to this storage reconciliation.
func (m *Manager) RecoverHold(ctx context.Context, id Identity, deadline time.Time) (Lease, Verified, error) {
	if err := m.outsideTransaction(); err != nil {
		return Lease{}, Verified{}, err
	}
	id = canonicalIdentity(id)
	if err := m.verifyAuthority(ctx, id); err != nil {
		return Lease{}, Verified{}, err
	}
	now := time.Now().UTC()
	deadline = deadline.UTC().Truncate(time.Millisecond)
	if !deadline.After(now) || deadline.Sub(now) > m.options.MaxDuration {
		return Lease{}, Verified{}, ErrInvalid
	}
	var row models.PayloadUploadHold
	var limits Limits
	err := m.publication(ctx, id, func(tx *gorm.DB) error {
		if err := m.options.Authorize(tx.Statement.Context, tx, id); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row, "id = ?", HoldID(id)).Error; err != nil {
			return err
		}
		var err error
		limits, err = m.validateRow(row, id)
		if err != nil {
			return err
		}
		if row.State == StatePinned {
			return ErrPinned
		}
		now := time.Now().UTC()
		if row.ExpiresAt.After(now) {
			return ErrBusy
		}
		if row.Generation == math.MaxUint64 || !deadline.After(now) {
			return ErrOwnerLost
		}
		row.Owner, row.Generation = uuid.NewString(), row.Generation+1
		row.Deadline, row.ExpiresAt = deadline, minTime(now.Add(m.options.LeaseTTL), deadline)
		row.State, row.Error = StateUploading, ""
		return tx.Model(&row).Updates(map[string]any{"owner": row.Owner, "generation": row.Generation, "deadline": row.Deadline, "expires_at": row.ExpiresAt, "state": row.State, "error": ""}).Error
	})
	if err != nil {
		return Lease{}, Verified{}, err
	}
	lease := leaseFor(m, row)
	lease.data.started.Store(true)
	verified, err := m.withLease(ctx, lease, func(ctx context.Context) (Verified, error) {
		archive, err := m.verifyPart(ctx, lease, row, true, limits.MaxArchiveBytes)
		if err != nil {
			return Verified{}, err
		}
		manifest, err := m.verifyPart(ctx, lease, row, false, limits.MaxManifestBytes)
		if err != nil {
			return Verified{}, err
		}
		return m.finishVerified(ctx, lease, archive, manifest)
	})
	return lease, verified, err
}

// CommitPinTx performs pure SQL after service authorization, physical Sandbox
// and source-job fencing. Its caller MUST publish snapshot.ready and final job
// state in this same outer transaction, returning any failed CAS to roll back
// the pin. It performs SQL checks without reading object storage.
func (m *Manager) CommitPinTx(tx *gorm.DB, lease Lease, verified Verified) (Pinned, error) {
	if !m.validLease(lease) || verified.lease != lease.data {
		return Pinned{}, ErrInvalid
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return Pinned{}, ErrTransaction
	}
	originalDB, err := m.db.DB()
	if err != nil {
		return Pinned{}, err
	}
	transactionDB, err := tx.DB()
	if err != nil || originalDB != transactionDB {
		return Pinned{}, ErrInvalid
	}
	bound, err := filestore.FromDB(tx)
	if err != nil || bound != m.files {
		return Pinned{}, ErrInvalid
	}
	archiveRef, err := m.files.VerifiedRef(verified.archive)
	if err != nil {
		return Pinned{}, err
	}
	manifestRef, err := m.files.VerifiedRef(verified.manifest)
	if err != nil {
		return Pinned{}, err
	}
	if err := m.options.Authorize(tx.Statement.Context, tx, lease.data.identity); err != nil {
		return Pinned{}, err
	}
	var row models.PayloadUploadHold
	if err := m.ownerQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), lease).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Pinned{}, ErrOwnerLost
		}
		return Pinned{}, err
	}
	if _, err := m.validateRow(row, lease.data.identity); err != nil {
		return Pinned{}, err
	}
	if row.State != StateVerified || row.ArchiveRef != archiveRef || row.ManifestRef != manifestRef || verified.archive.Size() != row.ArchiveBytes || verified.manifest.Size() != row.ManifestBytes {
		return Pinned{}, ErrConflict
	}
	// Recheck after any SQL lock wait. This fences proofs first presented after
	// shutdown; the caller's runtime drain still owns already-entered SQL TXs.
	if _, err := m.files.VerifiedRef(verified.archive); err != nil {
		return Pinned{}, err
	}
	result := m.ownerQuery(tx, lease).Where("state = ?", StateVerified).Updates(map[string]any{"state": StatePinned, "pinned_at": time.Now().UTC()})
	if result.Error != nil {
		return Pinned{}, result.Error
	}
	if result.RowsAffected != 1 {
		return Pinned{}, ErrOwnerLost
	}
	return Pinned{HoldID: row.ID, SnapshotID: row.SnapshotID, ArchiveRef: archiveRef, ManifestRef: manifestRef, ArchiveSHA256: row.ArchiveSHA256, ManifestSHA256: row.ManifestSHA256, ArchiveBytes: row.ArchiveBytes, ManifestBytes: row.ManifestBytes}, nil
}
