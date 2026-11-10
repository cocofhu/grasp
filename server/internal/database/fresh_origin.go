package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const FreshOriginProtocol = "grasp-fresh-origin/v1"

// FreshOriginSeal records that an initializer observed no application tables
// before creating them. It is not an old-data normalization receipt, execution
// authority, SQL backup identity, or proof that later payloads are intact.
type FreshOriginSeal struct {
	ID          string    `gorm:"primaryKey;size:32" json:"id"`
	Protocol    string    `gorm:"size:64" json:"protocol"`
	OriginID    string    `gorm:"size:36" json:"originId"`
	Initializer string    `gorm:"size:32" json:"initializer"`
	CapturedAt  time.Time `json:"capturedAt"`
	SealedAt    time.Time `json:"sealedAt"`
}

func (FreshOriginSeal) TableName() string { return "database_fresh_origins" }

// FreshOriginProof has no inbound representation. A current schema, empty
// execution quarantine, or active execution marker cannot construct this proof.
type FreshOriginProof struct {
	pool       *sql.DB
	originID   string
	capturedAt time.Time
	empty      bool
}

func (p FreshOriginProof) Empty() bool { return p.empty }

func freshOuterConnection(db *gorm.DB) (*sql.DB, error) {
	if db == nil || db.Statement == nil {
		return nil, errors.New("fresh origin requires an outer SQL connection")
	}
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return nil, errors.New("fresh origin requires an outer SQL connection")
	}
	return db.DB()
}

// CaptureFreshOrigin is read-only. The two registry tables are the sole
// application-independent allowance; any application or unknown table prevents
// fresh-origin attestation, even when it has no rows.
func CaptureFreshOrigin(ctx context.Context, db *gorm.DB) (FreshOriginProof, error) {
	pool, err := freshOuterConnection(db)
	if err != nil {
		return FreshOriginProof{}, err
	}
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return FreshOriginProof{}, err
	}
	for _, table := range tables {
		if table != "payload_storage_control" && table != "payload_storage_runtimes" && table != "sqlite_sequence" {
			return FreshOriginProof{}, nil
		}
	}
	return FreshOriginProof{pool: pool, originID: uuid.NewString(), capturedAt: time.Now().UTC(), empty: true}, nil
}

func validateFreshSeal(seal FreshOriginSeal) error {
	if seal.ID != "primary" || seal.Protocol != FreshOriginProtocol || uuid.Validate(seal.OriginID) != nil ||
		(seal.Initializer != "runtime" && seal.Initializer != "execution-init") || seal.CapturedAt.IsZero() ||
		seal.SealedAt.IsZero() || seal.SealedAt.Before(seal.CapturedAt) {
		return errors.New("invalid fresh database origin seal")
	}
	return nil
}

// SealFreshOrigin persists only a private same-pool proof captured before the
// initializer's schema work. A crash after schema creation but before this seal
// needs explicit upgrade repair; a later startup never infers an empty past.
func SealFreshOrigin(ctx context.Context, db *gorm.DB, proof FreshOriginProof, initializer string) error {
	pool, err := freshOuterConnection(db)
	if err != nil {
		return err
	}
	if !proof.empty || proof.pool != pool || uuid.Validate(proof.originID) != nil || proof.capturedAt.IsZero() ||
		(initializer != "runtime" && initializer != "execution-init") {
		return errors.New("fresh database origin was not captured by this SQL connection")
	}
	if db.Migrator().HasTable(&FreshOriginSeal{}) {
		var existing FreshOriginSeal
		err := db.WithContext(ctx).First(&existing, "id = ?", "primary").Error
		if err == nil {
			return validateFreshSeal(existing) // Concurrent empty initializers share the first seal.
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	// Before the first seal, startup has not admitted business operations. Only
	// the fixed workspace seed and maintenance/presence rows may be nonempty.
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return err
	}
	for _, table := range tables {
		if table == "payload_storage_control" || table == "payload_storage_runtimes" || table == "sqlite_sequence" || table == "database_fresh_origins" {
			continue
		}
		query := db.WithContext(ctx).Table(table)
		if table == "workspaces" {
			query = query.Where("id <> ?", models.WorkspaceID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("fresh database origin changed before sealing: %s contains data", table)
		}
	}
	if err := db.WithContext(ctx).AutoMigrate(&FreshOriginSeal{}); err != nil {
		return err
	}
	seal := FreshOriginSeal{ID: "primary", Protocol: FreshOriginProtocol, OriginID: proof.originID,
		Initializer: initializer, CapturedAt: proof.capturedAt, SealedAt: time.Now().UTC()}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seal).Error; err != nil {
		return err
	}
	var saved FreshOriginSeal
	if err := db.WithContext(ctx).First(&saved, "id = ?", "primary").Error; err != nil {
		return err
	}
	return validateFreshSeal(saved)
}
