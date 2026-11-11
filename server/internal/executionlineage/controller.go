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

// Controller is fixed to the SQL tuple observed at process startup. Offline
// restore never makes a running process adopt the new generation. Construct a
// new process after the explicit offline restore/activation protocol instead.
// A failed startup produces a permanently closed controller so history can
// remain readable without granting any execution authority.
type Controller struct {
	db      *gorm.DB
	startup Marker
	closed  error
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
	c := &Controller{db: db}
	if db == nil {
		c.closed = ErrUnavailable
		return c, c.closed
	}
	if err := outsideSQLTransaction(db); err != nil {
		c.closed = err
		return c, err
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maintenance storageguard.Control
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Take(&maintenance, "name = ?", "primary").Error; err != nil {
			return err
		}
		if maintenance.Owner != "" {
			return ErrInactive
		}
		if err := strictNoPendingRecovery(tx); err != nil {
			return err
		}
		var bound int64
		if err := tx.Model(&models.ExecutionDataset{}).Where("id = ?", "primary").Count(&bound).Error; err != nil {
			return err
		}
		if bound == 0 && tx.Migrator().HasTable("offline_upgrade_receipts") {
			var upgraded int64
			if err := tx.Table("offline_upgrade_receipts").Count(&upgraded).Error; err != nil {
				return err
			}
			if upgraded != 0 {
				return errors.Join(ErrUnbound, errors.New("upgraded history has no execution binding; run grasp-storage-admin --mode execution-init in the original upgrade workdir before starting"))
			}
		}
		if bound == 0 {
			fresh := models.ExecutionDataset{ID: "primary", NamespaceID: uuid.NewString(), DatasetID: uuid.NewString(), Lineage: uuid.NewString(), OperationID: uuid.NewString(), ControlRevision: 2, BoundAt: time.Now().UTC()}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&fresh).Error; err != nil {
				return err
			}
		}
		var binding models.ExecutionDataset
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Take(&binding, "id = ?", "primary").Error; err != nil {
			return err
		}
		var err error
		c.startup, err = markerFromBinding(binding)
		return err
	})
	if err != nil {
		c.closed = err
		return c, err
	}
	return c, nil
}

func (c *Controller) DatasetID() string {
	if c == nil || c.closed != nil {
		return ""
	}
	return c.startup.DatasetID
}

// Prepare performs a fresh SQL preflight before the caller's outermost
// transaction. The startup identity is immutable for this process.
func (c *Controller) Prepare(ctx context.Context) (Permit, error) {
	if c == nil || c.closed != nil || c.db == nil {
		if c != nil && c.closed != nil {
			return Permit{}, errors.Join(ErrUnavailable, c.closed)
		}
		return Permit{}, ErrUnavailable
	}
	if err := outsideSQLTransaction(c.db); err != nil {
		return Permit{}, err
	}
	permit := Permit{data: &permitData{controller: c, marker: c.startup}}
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return c.GuardTx(tx, permit) })
	if err != nil {
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
// historical metadata read. A proof remains tied to
// this process and every use still compares the current SQL tuple.
func (c *Controller) HasPreparedContext(ctx context.Context) bool {
	proof, ok := ctx.Value(permitContextKey{}).(contextualPermit)
	return ok && proof.controller == c
}

// CheckPrepared uses a proof already obtained outside SQL for a metadata-only
// preflight. It neither refreshes nor caches a permit.
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
	if c == nil || c.closed != nil || permit.data == nil || permit.data.controller != c || permit.data.marker != c.startup {
		return ErrUnavailable
	}
	if tx == nil {
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
	if err := strictNoPendingRecovery(tx); err != nil {
		return err
	}
	var binding models.ExecutionDataset
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Take(&binding, "id = ?", "primary").Error; err != nil {
		return errors.Join(ErrUnbound, err)
	}
	if !bindingMatches(binding, c.startup, c.startup.Revision) {
		return ErrUnbound
	}
	for _, entity := range entities {
		if !knownKind(entity.Kind) || entity.ID == "" {
			return ErrConflict
		}
		var rows []struct{ ID string }
		if err := tx.Table("execution_quarantines").Select("id").Where("entity_kind = ? AND entity_id = ? AND review_state = ?", string(entity.Kind), entity.ID, "pending").Limit(1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 0 {
			return ErrQuarantined
		}
	}
	return nil
}

// Transaction is the standard boundary for new execution metadata writes.
// The body receives the original caller context plus no serialized capability
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
