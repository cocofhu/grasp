package payloadstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func closureOuter(db *gorm.DB) error {
	if db == nil || db.Statement == nil {
		return errors.New("primary certification requires an outer SQL connection")
	}
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return errors.New("primary certification cannot read objects inside a SQL transaction")
	}
	return nil
}

// CertifyPrimary reads the actual configured remote primary, never the serving
// Store or its retained local fallback. Construction performs no bucket probe
// or object mutation. Successful verification keeps maintenance held.
func CertifyPrimary(ctx context.Context, db *gorm.DB, cfg config.StorageConfig, owner string) (PrimaryClosure, error) {
	if err := closureOuter(db); err != nil {
		return PrimaryClosure{}, err
	}
	target, err := proposedLocation(cfg)
	if err != nil || target.Driver != "s3" || uuid.Validate(owner) != nil {
		return PrimaryClosure{}, ErrPrimaryClosure
	}
	options := cfg.S3
	if strings.EqualFold(strings.TrimSpace(cfg.Driver), "cos") && options.PathStyle == nil {
		pathStyle := false
		options.PathStyle = &pathStyle
	}
	objects, err := objectstore.NewS3(options)
	if err != nil {
		return PrimaryClosure{}, err
	}
	return certifyPrimaryObjects(ctx, db, target, owner, objects)
}

func certifyPrimaryObjects(ctx context.Context, db *gorm.DB, target Location, owner string, objects objectstore.Store) (PrimaryClosure, error) {
	if err := closureOuter(db); err != nil {
		return PrimaryClosure{}, err
	}
	if err := closureFence(ctx, db, target, owner); err != nil {
		return PrimaryClosure{}, err
	}
	// DDL is confined to this explicit offline command, outside all object I/O
	// and outside the short receipt publication transaction.
	if err := db.WithContext(ctx).AutoMigrate(&PrimaryClosure{}); err != nil {
		return PrimaryClosure{}, err
	}
	refs, err := VerifyReferences(ctx, db, objects)
	if err != nil {
		return PrimaryClosure{}, err
	}
	stamp, err := closureReferenceStamp(ctx, db)
	if err != nil || stamp.Cells != refs.Cells || stamp.SHA256 != refs.SHA256 {
		return PrimaryClosure{}, errors.New("SQL reference set changed during primary verification; maintenance retained")
	}
	row := PrimaryClosure{ID: owner, Protocol: PrimaryClosureProtocol, PhysicalFingerprint: target.Fingerprint,
		ReferenceSHA256: refs.SHA256, Cells: refs.Cells, Payloads: refs.Payloads, Attachments: refs.Attachments, CompletedAt: time.Now().UTC().Truncate(time.Millisecond)}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := closureFence(ctx, tx, target, owner); err != nil {
			return err
		}
		var existing PrimaryClosure
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, "id = ?", owner).Error; err == nil {
			if !validPrimaryClosure(existing) || existing.PhysicalFingerprint != row.PhysicalFingerprint ||
				existing.ReferenceSHA256 != row.ReferenceSHA256 || existing.Cells != row.Cells ||
				existing.Payloads != row.Payloads || existing.Attachments != row.Attachments {
				return errors.New("original primary closure receipt differs; maintenance retained")
			}
			row = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&row).Error
	})
	return row, err
}

func closureFence(ctx context.Context, db *gorm.DB, target Location, owner string) error {
	var control storageguard.Control
	if err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&control, "name = ?", "primary").Error; err != nil {
		return err
	}
	if uuid.Validate(owner) != nil || control.Owner != owner {
		return errors.New("primary closure maintenance ownership lost")
	}
	var runtimes int64
	if err := db.WithContext(ctx).Model(&storageguard.Runtime{}).Count(&runtimes).Error; err != nil {
		return err
	}
	if runtimes != 0 {
		return errors.New("primary closure requires all registered runtimes stopped")
	}
	var current Location
	if err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "name = ?", "primary").Error; err != nil {
		return err
	}
	if current.Fingerprint != target.Fingerprint || current.Driver != target.Driver {
		return ErrPrimaryClosure
	}
	return nil
}

// FinalizePrimaryClosure releases only the original maintenance operation.
// It first re-verifies the primary outside its SQL transaction. A durable final
// stamp and Leave commit together, so a lost response never clears a later
// operation's maintenance or initializes another execution lineage.
func FinalizePrimaryClosure(ctx context.Context, db *gorm.DB, cfg config.StorageConfig, owner string) (PrimaryClosure, error) {
	var row PrimaryClosure
	if err := closureOuter(db); err != nil {
		return row, err
	}
	verified, err := CertifyPrimary(ctx, db, cfg, owner)
	if err != nil {
		return row, err
	}
	target, err := proposedLocation(cfg)
	if err != nil {
		return row, err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := closureFence(ctx, tx, target, owner); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", owner).Error; err != nil {
			return err
		}
		if !validPrimaryClosure(row) || row.PhysicalFingerprint != target.Fingerprint {
			return ErrPrimaryClosure
		}
		if row.ReferenceSHA256 != verified.ReferenceSHA256 || row.Cells != verified.Cells ||
			row.Payloads != verified.Payloads || row.Attachments != verified.Attachments || !row.CompletedAt.Equal(verified.CompletedAt) {
			return ErrPrimaryClosure
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		result := tx.Model(&PrimaryClosure{}).Where("id = ? AND finalized_at IS NULL", owner).Update("finalized_at", now)
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return ErrPrimaryClosure
		}
		if err := storageguard.Leave(ctx, tx, owner); err != nil {
			return err
		}
		row.FinalizedAt = &now
		return nil
	})
	return row, err
}
