package payloadstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PrimaryClosureProtocol = "grasp-primary-closure/v1"

var ErrPrimaryClosure = errors.New("primary payload storage needs explicit offline closure certification")

// PrimaryLocation returns only the credential-independent storage identity.
func PrimaryLocation(cfg config.StorageConfig) (Location, error) { return proposedLocation(cfg) }

// PrimaryClosure certifies the configured physical primary, independently of
// retained local bytes. It records an immutable historical verification, not a
// current whole-database digest, an execution authority or a deletion permit.
type PrimaryClosure struct {
	ID                  string     `gorm:"primaryKey;size:36" json:"id"`
	Protocol            string     `gorm:"size:64" json:"protocol"`
	PhysicalFingerprint string     `gorm:"size:64;index" json:"physicalFingerprint"`
	ReferenceSHA256     string     `gorm:"size:64" json:"referenceSHA256"`
	Cells               int64      `json:"cells"`
	Payloads            int64      `json:"payloads"`
	Attachments         int64      `json:"attachments"`
	CompletedAt         time.Time  `json:"completedAt"`
	FinalizedAt         *time.Time `json:"finalizedAt,omitempty"`
}

func (PrimaryClosure) TableName() string { return "payload_storage_closures" }

func closureDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func validPrimaryClosure(row PrimaryClosure) bool {
	return uuid.Validate(row.ID) == nil && row.Protocol == PrimaryClosureProtocol &&
		closureDigest(row.PhysicalFingerprint) && closureDigest(row.ReferenceSHA256) &&
		row.Cells >= 0 && row.Payloads >= 0 && row.Attachments >= 0 && !row.CompletedAt.IsZero() &&
		(row.FinalizedAt == nil || !row.FinalizedAt.Before(row.CompletedAt))
}

// CheckPrimaryClosure is read-only and may run inside a read-only startup
// snapshot. It does not migrate schema, construct stores, probe or read objects.
// Later valid writes may change the reference set; a historical certificate is
// deliberately not compared to today's complete SQL reference digest.
func CheckPrimaryClosure(ctx context.Context, db *gorm.DB, cfg config.StorageConfig) error {
	if db == nil || db.Statement == nil {
		return ErrPrimaryClosure
	}
	target, err := proposedLocation(cfg)
	if err != nil || (target.Driver != "local" && target.Driver != "s3") {
		return ErrPrimaryClosure
	}
	var current Location
	if !db.WithContext(ctx).Migrator().HasTable(&Location{}) {
		return CanBootstrapPrimaryClosure(ctx, db)
	}
	if err := db.WithContext(ctx).First(&current, "name = ?", "primary").Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CanBootstrapPrimaryClosure(ctx, db)
		}
		return err
	}
	if current.Fingerprint != target.Fingerprint || current.Driver != target.Driver {
		return ErrPrimaryClosure
	}
	if current.Driver == "local" {
		return nil
	}
	if db.WithContext(ctx).Migrator().HasTable(&PrimaryClosure{}) {
		var rows []PrimaryClosure
		if err := db.WithContext(ctx).Where("physical_fingerprint = ?", target.Fingerprint).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if validPrimaryClosure(row) {
				return nil
			}
		}
	}
	// These rows are written only by verified replica activation. The current
	// primary must be its destination; a source-side or incomplete copy is not
	// evidence that the primary can serve all historical references.
	if db.WithContext(ctx).Migrator().HasTable(&Replica{}) {
		var rows []Replica
		if err := db.WithContext(ctx).Where("destination_fingerprint = ?", target.Fingerprint).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if uuid.Validate(row.ID) == nil && closureDigest(row.SourceFingerprint) &&
				closureDigest(row.DatabaseSHA256) && closureDigest(row.ManifestSHA256) &&
				row.Objects >= 0 && row.Bytes >= 0 && !row.CreatedAt.IsZero() {
				return nil
			}
		}
	}
	return ErrPrimaryClosure
}

// CanBootstrapPrimaryClosure admits only a genuinely sealed installation that
// still has no physical primary and no business data. An old fresh-origin seal
// alone never certifies existing messages, pending work, quarantines or objects.
func CanBootstrapPrimaryClosure(ctx context.Context, db *gorm.DB) error {
	var seal database.FreshOriginSeal
	if !db.WithContext(ctx).Migrator().HasTable(&seal) ||
		db.WithContext(ctx).First(&seal, "id = ?", "primary").Error != nil ||
		seal.Protocol != database.FreshOriginProtocol || uuid.Validate(seal.OriginID) != nil ||
		(seal.Initializer != "runtime" && seal.Initializer != "execution-init") ||
		seal.CapturedAt.IsZero() || seal.SealedAt.IsZero() || seal.SealedAt.Before(seal.CapturedAt) {
		return ErrPrimaryClosure
	}
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return err
	}
	for _, table := range tables {
		switch table {
		case "payload_storage_control", "payload_storage_runtimes", "sqlite_sequence", "database_fresh_origins", "execution_datasets":
			if table == "execution_datasets" {
				var count int64
				if err := db.WithContext(ctx).Table(table).Where("quarantine_count <> 0 OR source_dataset_id <> '' OR source_lineage <> '' OR backup_sha256 <> ''").Count(&count).Error; err != nil || count != 0 {
					return ErrPrimaryClosure
				}
			}
			continue
		}
		query := db.WithContext(ctx).Table(table)
		if table == "workspaces" {
			var workspace models.Workspace
			err := db.WithContext(ctx).First(&workspace, "id = ?", models.WorkspaceID).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && (workspace.Name != models.DefaultWorkspaceName || workspace.Description != "" ||
				len(workspace.Variables) != 0 || workspace.PmLeaderEnabled || workspace.PmLeaderAgent != "" ||
				len(workspace.PmEnabledMcps) != 0 || workspace.PmGateAutoVar != "" || workspace.PmGateAutoPrompt != "" ||
				workspace.UnknownModelDisplayName != "" || !reflect.DeepEqual(workspace.NotifyPolicy, models.DefaultWorkspaceNotifyPolicy())) {
				return ErrPrimaryClosure
			}
			query = query.Where("id <> ?", models.WorkspaceID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("%w: %s contains retained data", ErrPrimaryClosure, table)
		}
	}
	return nil
}

// BindEmptyPrimaryClosureTx creates the first primary and its exact empty-set
// receipt together. The caller must have created these two metadata tables
// before its transaction. It never migrates, opens objects or accepts a caller
// supplied boolean as proof; all empty-origin checks are repeated in this TX.
func BindEmptyPrimaryClosureTx(ctx context.Context, tx *gorm.DB, target Location) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return errors.New("empty primary binding requires a SQL transaction")
	}
	if err := CanBootstrapPrimaryClosure(ctx, tx); err != nil {
		return err
	}
	if target.Driver != "s3" || !closureDigest(target.Fingerprint) {
		return ErrPrimaryClosure
	}
	normal, err := proposedLocation(config.StorageConfig{Driver: target.Driver, BlobsRoot: target.LocalRoot,
		S3: config.S3StorageConfig{Endpoint: target.Endpoint, Bucket: target.Bucket, Prefix: target.Prefix, Region: target.Region}})
	if err != nil || normal.Fingerprint != target.Fingerprint || target.Name != "primary" {
		return ErrPrimaryClosure
	}
	var seal database.FreshOriginSeal
	if err := tx.WithContext(ctx).First(&seal, "id = ?", "primary").Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(&target).Error; err != nil {
		return err
	}
	// The fixed workspace seed itself has ordinary JSON metadata, which the
	// verifier includes in its cell digest. Preserve that exact convention.
	stamp, err := closureReferenceStamp(ctx, tx)
	if err != nil {
		return err
	}
	row := PrimaryClosure{ID: seal.OriginID, Protocol: PrimaryClosureProtocol,
		PhysicalFingerprint: target.Fingerprint, ReferenceSHA256: stamp.SHA256, Cells: stamp.Cells, CompletedAt: time.Now().UTC().Truncate(time.Millisecond)}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}
