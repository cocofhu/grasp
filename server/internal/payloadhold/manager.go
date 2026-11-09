package payloadhold

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalid     = errors.New("invalid payload upload intent or proof")
	ErrConflict    = errors.New("payload upload identity has different immutable contents")
	ErrBusy        = errors.New("payload upload still has a live owner")
	ErrReconcile   = errors.New("payload upload requires explicit reconciliation")
	ErrOwnerLost   = errors.New("payload upload owner lost")
	ErrLimit       = errors.New("payload upload exceeds configured limits or reservation")
	ErrAuthority   = errors.New("payload upload execution lineage is unavailable or changed")
	ErrTransaction = errors.New("payload upload I/O requires an outer transaction-free handle")
	ErrPinned      = errors.New("payload upload is already pinned")
)

const (
	StateUploading = "uploading"
	StateVerified  = "verified"
	StateUnknown   = "unknown"
	StatePinned    = "pinned"
)

// Authority must verify a currently valid external lineage. There is no SQL,
// startup-random or permissive default. Storage proof alone cannot authorize
// sandbox execution or make a restored database safe to resume.
type Authority interface {
	Verify(context.Context, string) error
}

type Identity struct {
	ScopeKind   string    `json:"scopeKind"`
	ScopeID     string    `json:"scopeId"`
	ActorID     string    `json:"actorId"`
	OperationID string    `json:"operationId"`
	SnapshotID  string    `json:"snapshotId"`
	Lineage     string    `json:"lineage"`
	Deadline    time.Time `json:"deadline"`
}

// Options keeps storage budgets separate from workspace restore/retention
// quotas. Authorize is required, metadata-only, and invoked before hold locks;
// the service must additionally fence the physical Sandbox and source job in
// the final publication transaction before calling CommitPinTx.
type Options struct {
	StorageFingerprint    string
	StagingDir            string
	Limits                Limits
	MaxReservedBytes      int64
	MaxScopeReservedBytes int64
	LeaseTTL              time.Duration
	RenewInterval         time.Duration
	MaxDuration           time.Duration
	Authority             Authority
	Authorize             func(context.Context, *gorm.DB, Identity) error
}

type Manager struct {
	db      *gorm.DB
	files   *filestore.Store
	options Options
}

func New(db *gorm.DB, files *filestore.Store, options Options) (*Manager, error) {
	if db == nil || files == nil || options.Authority == nil || options.Authorize == nil || !validDigest(options.StorageFingerprint) {
		return nil, ErrInvalid
	}
	if options.Limits == (Limits{}) {
		options.Limits = DefaultLimits()
	}
	if !options.Limits.valid(options.Limits) {
		return nil, ErrLimit
	}
	if options.MaxReservedBytes == 0 {
		options.MaxReservedBytes = 32 << 30
	}
	if options.MaxScopeReservedBytes == 0 {
		options.MaxScopeReservedBytes = 8 << 30
	}
	if options.MaxReservedBytes < 1 || options.MaxScopeReservedBytes < 1 || options.MaxScopeReservedBytes > options.MaxReservedBytes {
		return nil, ErrLimit
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 45 * time.Second
	}
	if options.RenewInterval <= 0 {
		options.RenewInterval = options.LeaseTTL / 3
	}
	if options.RenewInterval >= options.LeaseTTL {
		return nil, ErrInvalid
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 30 * time.Minute
	}
	if options.StagingDir == "" {
		options.StagingDir = filepath.Join(files.Root, "snapshot-stage")
	}
	m := &Manager{db: db, files: files, options: options}
	if err := m.outsideTransaction(); err != nil {
		return nil, err
	}
	bound, err := filestore.FromDB(db)
	if err != nil || bound != files {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(options.StagingDir, 0o700); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) outsideTransaction() error {
	if _, inside := m.db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return ErrTransaction
	}
	return nil
}

func canonicalIdentity(id Identity) Identity {
	id.Deadline = id.Deadline.UTC().Truncate(time.Millisecond)
	return id
}

func validName(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func (id Identity) valid() bool {
	return (id.ScopeKind == "solo" || id.ScopeKind == "run") && validName(id.ScopeID, 191) && validName(id.ActorID, 191) && validName(id.OperationID, 128) && validName(id.SnapshotID, 64) && validName(id.Lineage, 128) && !id.Deadline.IsZero()
}

// HoldID is an idempotency identity, never an authorization capability.
func HoldID(id Identity) string {
	body, _ := json.Marshal([]string{id.ScopeKind, id.ScopeID, id.ActorID, id.OperationID})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	body, err := hex.DecodeString(value)
	return err == nil && len(body) == sha256.Size && value == strings.ToLower(value)
}

func rawRef(encoding, digest string) string { return filestore.Prefix + encoding + "/" + digest }

func planHash(id Identity, namespace string, archive, manifest stagedPart, limits Limits) string {
	body, _ := json.Marshal(struct {
		Version   int      `json:"version"`
		Identity  Identity `json:"identity"`
		Namespace string   `json:"namespace"`
		Archive   string   `json:"archive"`
		Bytes     int64    `json:"bytes"`
		Manifest  string   `json:"manifest"`
		JSONBytes int64    `json:"jsonBytes"`
		Limits    Limits   `json:"limits"`
	}{1, id, namespace, archive.sha256, archive.size, manifest.sha256, manifest.size, limits})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func identityOf(row models.PayloadUploadHold) Identity {
	return canonicalIdentity(Identity{ScopeKind: row.ScopeKind, ScopeID: row.ScopeID, ActorID: row.ActorID, OperationID: row.OperationID, SnapshotID: row.SnapshotID, Lineage: row.Lineage, Deadline: row.IntentDeadline})
}

func (m *Manager) validateRow(row models.PayloadUploadHold, id Identity) (Limits, error) {
	var limits Limits
	if row.ID != HoldID(id) || identityOf(row) != id || row.StorageFingerprint != m.options.StorageFingerprint || json.Unmarshal([]byte(row.Limits), &limits) != nil || !limits.valid(m.options.Limits) || !validDigest(row.ArchiveSHA256) || !validDigest(row.ManifestSHA256) || row.ArchiveBytes < 1 || row.ArchiveBytes > limits.MaxArchiveBytes || row.ManifestBytes < 1 || row.ManifestBytes > limits.MaxManifestBytes || row.ReservedBytes != row.ArchiveBytes+row.ManifestBytes {
		return limits, ErrConflict
	}
	a, b := stagedPart{sha256: row.ArchiveSHA256, size: row.ArchiveBytes}, stagedPart{sha256: row.ManifestSHA256, size: row.ManifestBytes}
	if row.PlanSHA256 != planHash(id, row.StorageFingerprint, a, b, limits) || row.ArchiveRef != "" && row.ArchiveRef != rawRef(filestore.Binary, row.ArchiveSHA256) || row.ManifestRef != "" && row.ManifestRef != rawRef(filestore.JSON, row.ManifestSHA256) {
		return limits, ErrConflict
	}
	if row.State != StateUploading && row.State != StateVerified && row.State != StateUnknown && row.State != StatePinned || row.ArchiveRef != "" && row.ArchiveVerifiedAt == nil || row.ManifestRef != "" && row.ManifestVerifiedAt == nil || (row.State == StateVerified || row.State == StatePinned) && (row.ArchiveRef == "" || row.ManifestRef == "") || row.State == StatePinned && row.PinnedAt == nil {
		return limits, ErrConflict
	}
	return limits, nil
}

func (m *Manager) verifyAuthority(ctx context.Context, id Identity) error {
	if !id.valid() {
		return ErrInvalid
	}
	if err := m.options.Authority.Verify(ctx, id.Lineage); err != nil {
		return errors.Join(ErrAuthority, err)
	}
	return ctx.Err()
}

type leaseData struct {
	manager    *Manager
	identity   Identity
	id, owner  string
	generation uint64
	plan       string
	expiresAt  time.Time
	deadline   time.Time
	started    atomic.Bool
}

// Lease is a process-local, private owner capability. Reconstructing an old SQL
// row cannot mint one; recovery must explicitly verify the external lineage.
type Lease struct{ data *leaseData }

func (l Lease) ID() string {
	if l.data == nil {
		return ""
	}
	return l.data.id
}

func leaseFor(m *Manager, row models.PayloadUploadHold) Lease {
	return Lease{data: &leaseData{manager: m, identity: identityOf(row), id: row.ID, owner: row.Owner, generation: row.Generation, plan: row.PlanSHA256, expiresAt: row.ExpiresAt, deadline: row.Deadline}}
}

func (m *Manager) validLease(lease Lease) bool {
	return lease.data != nil && lease.data.manager == m && lease.data.id != "" && lease.data.owner != "" && lease.data.generation > 0
}

// BeginHold commits the reservation before any object PUT. A duplicate identity
// returns a stable receipt through Get; it never silently acquires/replays an
// earlier upload, even when its original lease has expired.
func (m *Manager) BeginHold(ctx context.Context, id Identity, plan Plan) (Lease, error) {
	if err := m.outsideTransaction(); err != nil {
		return Lease{}, err
	}
	id = canonicalIdentity(id)
	if err := m.verifyAuthority(ctx, id); err != nil {
		return Lease{}, err
	}
	now := time.Now().UTC()
	if !id.Deadline.After(now) || id.Deadline.Sub(now) > m.options.MaxDuration {
		return Lease{}, ErrInvalid
	}
	a, b, limits, err := plan.details(m)
	if err != nil {
		return Lease{}, err
	}
	limitJSON, _ := json.Marshal(limits)
	row := models.PayloadUploadHold{ID: HoldID(id), ScopeKind: id.ScopeKind, ScopeID: id.ScopeID, ActorID: id.ActorID, OperationID: id.OperationID, SnapshotID: id.SnapshotID, Lineage: id.Lineage, StorageFingerprint: m.options.StorageFingerprint, PlanSHA256: planHash(id, m.options.StorageFingerprint, a, b, limits), Limits: string(limitJSON), ArchiveSHA256: a.sha256, ArchiveBytes: a.size, ManifestSHA256: b.sha256, ManifestBytes: b.size, ReservedBytes: a.size + b.size, IntentDeadline: id.Deadline, Deadline: id.Deadline, State: StateUploading, Owner: uuid.NewString(), Generation: 1, ExpiresAt: minTime(now.Add(m.options.LeaseTTL), id.Deadline)}
	var duplicate error
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Authorizers that lock Sandbox/Job metadata run before quota/hold.
		if err := m.options.Authorize(ctx, tx, id); err != nil {
			return err
		}
		budget := models.PayloadUploadBudget{ID: m.options.StorageFingerprint}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&budget).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&budget, "id = ?", budget.ID).Error; err != nil {
			return err
		}
		if budget.HoldCount == 0 || budget.ReservedBytes == 0 {
			var existing []models.PayloadUploadHold
			if err := tx.Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("storage_fingerprint = ?", row.StorageFingerprint).Limit(1).Find(&existing).Error; err != nil {
				return err
			}
			if len(existing) != 0 {
				return ErrConflict // Lost counters cannot silently reset capacity.
			}
		}
		var prior models.PayloadUploadHold
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&prior, "id = ?", row.ID).Error
		if err == nil {
			if _, err := m.validateRow(prior, id); err != nil || prior.PlanSHA256 != row.PlanSHA256 {
				return ErrConflict
			}
			duplicate = ErrReconcile
			if prior.State == StatePinned {
				duplicate = ErrPinned
			} else if prior.ExpiresAt.After(now) {
				duplicate = ErrBusy
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// A separate locked counter avoids a stale aggregate under MySQL RR.
		// Every admission locks namespace then scope; no other path reverses it.
		scopeHash := sha256.Sum256([]byte("scope\x00" + row.StorageFingerprint + "\x00" + row.ScopeKind + "\x00" + row.ScopeID))
		scopeBudget := models.PayloadUploadBudget{ID: hex.EncodeToString(scopeHash[:])}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&scopeBudget).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&scopeBudget, "id = ?", scopeBudget.ID).Error; err != nil {
			return err
		}
		if scopeBudget.HoldCount == 0 || scopeBudget.ReservedBytes == 0 {
			var existing []models.PayloadUploadHold
			if err := tx.Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("storage_fingerprint = ? AND scope_kind = ? AND scope_id = ?", row.StorageFingerprint, row.ScopeKind, row.ScopeID).Limit(1).Find(&existing).Error; err != nil {
				return err
			}
			if len(existing) != 0 {
				return ErrConflict
			}
		}
		if budget.ReservedBytes < 0 || scopeBudget.ReservedBytes < 0 || row.ReservedBytes > m.options.MaxReservedBytes-budget.ReservedBytes || row.ReservedBytes > m.options.MaxScopeReservedBytes-scopeBudget.ReservedBytes || budget.HoldCount == math.MaxInt64 || scopeBudget.HoldCount == math.MaxInt64 {
			return ErrLimit
		}
		now := time.Now().UTC()
		if !row.Deadline.After(now) {
			return ErrOwnerLost
		}
		row.ExpiresAt = minTime(now.Add(m.options.LeaseTTL), row.Deadline)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&budget).Updates(map[string]any{"reserved_bytes": budget.ReservedBytes + row.ReservedBytes, "hold_count": budget.HoldCount + 1}).Error; err != nil {
			return err
		}
		return tx.Model(&scopeBudget).Updates(map[string]any{"reserved_bytes": scopeBudget.ReservedBytes + row.ReservedBytes, "hold_count": scopeBudget.HoldCount + 1}).Error
	})
	if err != nil {
		return Lease{}, err
	}
	if duplicate != nil {
		return Lease{}, duplicate
	}
	return leaseFor(m, row), nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Get returns only authorized SQL metadata; it performs no object reads. A
// still-uploading row with an expired owner is unknown, not safe to restart.
func (m *Manager) Get(ctx context.Context, id Identity) (models.PayloadUploadHold, error) {
	var row models.PayloadUploadHold
	if err := m.outsideTransaction(); err != nil {
		return row, err
	}
	id = canonicalIdentity(id)
	if err := m.verifyAuthority(ctx, id); err != nil {
		return row, err
	}
	if err := m.options.Authorize(ctx, m.db.WithContext(ctx), id); err != nil {
		return row, err
	}
	if err := m.db.WithContext(ctx).Take(&row, "id = ?", HoldID(id)).Error; err != nil {
		return row, err
	}
	_, err := m.validateRow(row, id)
	return row, err
}
