package executionlineage

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Entity identifies an execution source or one of its parents. Callers must
// check both the concrete work item and its owning Run/Solo/Queue/Sandbox.
type Entity struct {
	Kind EntityKind
	ID   string
}

// Controller is fixed to the tuple observed at process startup. An external
// restore never makes a running process adopt the new generation. Construct a
// new process after the explicit offline restore/activation protocol instead.
// A failed startup produces a permanently closed controller so history can
// remain readable without granting any execution authority.
type Controller struct {
	db        *gorm.DB
	authority *Authority
	startup   Marker
	closed    error
	local     bool
}

type permitContextKey struct{}

type contextualPermit struct {
	controller *Controller
	permit     Permit
}

// NewLocalController grants execution from the SQL binding alone. An existing
// binding keeps its dataset and lineage, because workspace heads, snapshot
// jobs and review receipts already persisted them; a fresh database gets a
// new random binding. Upgraded history must first be bound by execution-init
// so its legacy work is quarantined. SQL quarantines and the maintenance lock
// still apply.
func NewLocalController(ctx context.Context, db *gorm.DB) (*Controller, error) {
	if db == nil {
		return nil, ErrUnavailable
	}
	if err := outsideSQLTransaction(db); err != nil {
		return nil, err
	}
	var bound int64
	if err := db.WithContext(ctx).Model(&models.ExecutionDataset{}).Where("id = ?", "primary").Count(&bound).Error; err != nil {
		return nil, err
	}
	if bound == 0 && db.Migrator().HasTable("offline_upgrade_receipts") {
		var upgraded int64
		if err := db.WithContext(ctx).Table("offline_upgrade_receipts").Count(&upgraded).Error; err != nil {
			return nil, err
		}
		if upgraded != 0 {
			return nil, errors.Join(ErrUnbound, errors.New("upgraded history has no execution binding; run grasp-storage-admin --mode execution-init in the original upgrade workdir before starting"))
		}
	}
	fresh := models.ExecutionDataset{ID: "primary", NamespaceID: uuid.NewString(), DatasetID: uuid.NewString(), Lineage: uuid.NewString(), OperationID: uuid.NewString(), ControlRevision: 2, BoundAt: time.Now().UTC()}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&fresh).Error; err != nil {
		return nil, err
	}
	var binding models.ExecutionDataset
	if err := db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error; err != nil {
		return nil, err
	}
	if !validUUID(binding.NamespaceID) || !validUUID(binding.DatasetID) || !validUUID(binding.Lineage) || !validUUID(binding.OperationID) {
		return nil, ErrUnbound
	}
	return &Controller{db: db, local: true, startup: Marker{
		Version:     1,
		NamespaceID: binding.NamespaceID,
		DatasetID:   binding.DatasetID,
		Lineage:     binding.Lineage,
		Revision:    binding.ControlRevision,
		State:       Active,
		OperationID: binding.OperationID,
	}}, nil
}

func NewController(ctx context.Context, db *gorm.DB, authority *Authority) (*Controller, error) {
	c := &Controller{db: db, authority: authority}
	if db == nil || authority == nil {
		c.closed = ErrUnavailable
		return c, c.closed
	}
	if err := outsideSQLTransaction(db); err != nil {
		c.closed = err
		return c, err
	}
	permit, err := authority.ReadActive(ctx)
	if err == nil {
		err = authority.CheckBinding(ctx, db, permit)
	}
	if err != nil {
		c.closed = err
		return c, err
	}
	c.startup = permit.data.marker
	return c, nil
}

func (c *Controller) DatasetID() string {
	if c == nil || c.closed != nil {
		return ""
	}
	return c.startup.DatasetID
}

// Prepare performs a fresh external read before the caller's outermost SQL
// transaction. It cannot run with a transaction-bound Controller database.
// There is no background cache or process-local generation authority.
func (c *Controller) Prepare(ctx context.Context) (Permit, error) {
	if c != nil && c.local {
		return Permit{data: &permitData{marker: c.startup}}, nil
	}
	if c == nil || c.closed != nil || c.authority == nil {
		if c != nil && c.closed != nil {
			return Permit{}, errors.Join(ErrUnavailable, c.closed)
		}
		return Permit{}, ErrUnavailable
	}
	permit, err := c.authority.ReadActive(ctx)
	if err != nil {
		return Permit{}, err
	}
	if permit.data.marker != c.startup {
		return Permit{}, ErrConflict
	}
	if err := c.authority.CheckBinding(ctx, c.db, permit); err != nil {
		return Permit{}, err
	}
	return permit, nil
}

// PrepareContext carries one explicitly prepared private proof to metadata
// callbacks. Each caller must invoke it outside its outermost transaction; it
// is not a cache and does not refresh or perform I/O from GuardContextTx.
func (c *Controller) PrepareContext(ctx context.Context) (context.Context, error) {
	permit, err := c.Prepare(ctx)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, permitContextKey{}, contextualPermit{c, permit}), nil
}

func (c *Controller) GuardContextTx(ctx context.Context, tx *gorm.DB, entities ...Entity) error {
	proof, ok := ctx.Value(permitContextKey{}).(contextualPermit)
	if !ok || proof.controller != c {
		return ErrUnavailable
	}
	return c.GuardTx(tx, proof.permit, entities...)
}

// HasPreparedContext distinguishes an explicit execution preflight from a
// historical metadata read. An expired proof remains present and must fail;
// callers must never fall back to an unguarded read because it expired.
func (c *Controller) HasPreparedContext(ctx context.Context) bool {
	proof, ok := ctx.Value(permitContextKey{}).(contextualPermit)
	return ok && proof.controller == c
}

// CheckPrepared uses a proof already obtained outside SQL for a metadata-only
// preflight. It neither refreshes nor caches an external permit.
func (c *Controller) CheckPrepared(ctx context.Context, db *gorm.DB, entities ...Entity) error {
	if db == nil {
		return ErrUnavailable
	}
	if err := outsideSQLTransaction(db); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return c.GuardContextTx(ctx, tx, entities...) })
}

// GuardTx is pure SQL and must precede execution row locks. The SQL
// maintenance SHARE lock prevents an offline transition from crossing an
// already admitted SQL publication. Native calls occur after releasing SQL.
func (c *Controller) GuardTx(tx *gorm.DB, permit Permit, entities ...Entity) error {
	if c == nil || c.closed != nil || c.authority == nil && !c.local || permit.data == nil || permit.data.marker != c.startup {
		return ErrUnavailable
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrTransaction
	}
	var maintenance storageguard.Control
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Take(&maintenance, "name = ?", "primary").Error; err != nil {
		return err
	}
	if maintenance.Owner != "" {
		return ErrInactive
	}
	if c.local {
		return localGuardTx(tx, entities)
	}
	if err := c.authority.GuardTx(tx, permit, "", ""); err != nil {
		return err
	}
	for _, entity := range entities {
		if err := c.authority.GuardTx(tx, permit, entity.Kind, entity.ID); err != nil {
			return err
		}
	}
	return nil
}

// Transaction is the standard boundary for new execution metadata writes.
// The body receives the original caller context plus no external capability
// strings; the private proof is checked before application rows are locked.
func (c *Controller) Transaction(ctx context.Context, db *gorm.DB, entities []Entity, body func(*gorm.DB) error) error {
	if db == nil {
		return ErrUnavailable
	}
	if err := outsideSQLTransaction(db); err != nil {
		return err
	}
	ctx, err := c.PrepareContext(ctx)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := c.GuardContextTx(ctx, tx, entities...); err != nil {
			return err
		}
		if err := body(tx); err != nil {
			return err
		}
		return c.GuardContextTx(ctx, tx, entities...)
	})
}

// Check is a short metadata-only preflight immediately before a native call.
// Callers still fence each later publication with a fresh Transaction/Prepare.
func (c *Controller) Check(ctx context.Context, entities ...Entity) error {
	if c == nil {
		return ErrUnavailable
	}
	return c.Transaction(ctx, c.db, entities, func(*gorm.DB) error { return nil })
}

func (c *Controller) Current(ctx context.Context) (string, error) {
	permit, err := c.Prepare(ctx)
	return permit.Lineage(), err
}

func (c *Controller) Verify(ctx context.Context, lineage string) error {
	permit, err := c.Prepare(ctx)
	if err != nil {
		return err
	}
	if permit.Lineage() != lineage {
		return ErrConflict
	}
	return nil
}
