// Package formatguard checks cleanup-version eligibility without attaching a
// payload store, migrating SQL, registering a runtime, or repairing old data.
package formatguard

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMigrationRequired = errors.New("database format requires the migration release; follow server/UPGRADING.md before starting this cleanup release")

// Check inspects only SQL formats. Production startup uses CheckConfiguration
// to additionally require the configured primary's physical closure proof.
func Check(ctx context.Context, cfg config.DatabaseConfig) error {
	return check(ctx, cfg, nil)
}

// CheckConfiguration runs before OpenRuntime: even its filestore attachment,
// runtime registration and SQLite backup are too early for an unverified old
// database. SQL format and physical-primary evidence share one read-only view.
func CheckConfiguration(ctx context.Context, cfg *config.Config) error {
	if cfg == nil {
		return ErrMigrationRequired
	}
	return check(ctx, cfg.Database, &cfg.Storage)
}

func check(ctx context.Context, cfg config.DatabaseConfig, storage *config.StorageConfig) error {
	ctx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	db, err := openReadOnly(ctx, cfg)
	if err != nil {
		return fmt.Errorf("%w: could not inspect SQL", ErrMigrationRequired)
	}
	if db == nil {
		return nil // Missing persistent SQLite file or an ephemeral new database.
	}
	conn, err := db.DB()
	if err != nil {
		return fmt.Errorf("%w: could not inspect SQL", ErrMigrationRequired)
	}
	defer func() { _ = conn.Close() }()
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := CheckSQL(ctx, tx); err != nil {
			return err
		}
		if storage == nil {
			return nil
		}
		tables, err := tx.Migrator().GetTables()
		if err != nil {
			return err
		}
		if originalEmpty(tables) {
			// OpenRuntime must capture the empty origin before creating any
			// application table. First-primary binding then repeats the empty
			// business-data checks in its own SQL transaction.
			return nil
		}
		return payloadstore.CheckPrimaryClosure(ctx, tx, *storage)
	}, &sql.TxOptions{ReadOnly: true}); err != nil {
		return fmt.Errorf("%w: %v", ErrMigrationRequired, err)
	}
	return nil
}

// CheckSQL is SQL-only and accepts no untrusted completion marker. It checks
// supported durable receipts plus the current formats, not a frozen whole-DB
// reference digest that legitimate new work would change.
func CheckSQL(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return ErrMigrationRequired
	}
	db = db.Session(&gorm.Session{NewDB: true, Context: ctx}).Model(nil)
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return err
	}
	if originalEmpty(tables) {
		return nil
	}
	fresh := validFresh(ctx, db)
	upgraded := validUpgrade(ctx, db)
	if !fresh && !upgraded {
		return errors.New("no supported fresh-origin seal or complete normalization receipt")
	}
	if fresh && freshBootstrap(ctx, db, tables) {
		return nil // Explicit execution-init created only an empty core schema.
	}
	if err := currentSchema(ctx, db); err != nil {
		return err
	}
	if err := canonicalCells(ctx, db); err != nil {
		return err
	}
	history, err := historymigration.CheckSoloEventsFormat(ctx, db)
	if err != nil {
		return err
	}
	if history.Messages > 0 && !upgraded {
		return errors.New("imported history has no complete normalization receipt")
	}
	return retainedSoloPending(ctx, db, upgraded)
}

func originalEmpty(tables []string) bool {
	for _, table := range tables {
		if table != "payload_storage_control" && table != "payload_storage_runtimes" && table != "sqlite_sequence" {
			return false
		}
	}
	return true
}

func validFresh(ctx context.Context, db *gorm.DB) bool {
	if !db.Migrator().HasTable(&database.FreshOriginSeal{}) {
		return false
	}
	var seal database.FreshOriginSeal
	if db.WithContext(ctx).Take(&seal, "id = ?", "primary").Error != nil {
		return false
	}
	return seal.Protocol == database.FreshOriginProtocol && uuid.Validate(seal.OriginID) == nil &&
		(seal.Initializer == "runtime" || seal.Initializer == "execution-init") &&
		!seal.CapturedAt.IsZero() && !seal.SealedAt.IsZero() && !seal.SealedAt.Before(seal.CapturedAt)
}

func digest(value string) bool {
	bytes, err := hex.DecodeString(value)
	return err == nil && len(bytes) == 32 && hex.EncodeToString(bytes) == value
}

func validUpgrade(ctx context.Context, db *gorm.DB) bool {
	if !db.Migrator().HasTable(&database.OfflineUpgradeReceipt{}) {
		return false
	}
	var rows []database.OfflineUpgradeReceipt
	if db.WithContext(ctx).Order("completed_at DESC").Find(&rows).Error != nil {
		return false
	}
	for _, row := range rows {
		if uuid.Validate(row.ID) == nil && row.Protocol == "grasp-offline-normalization/v1" &&
			row.HistoryProtocol == historymigration.Protocol && row.LegacyEvents == 0 &&
			digest(row.SourceDatabaseSHA256) && digest(row.ConfigurationSHA256) && digest(row.ReferenceSHA256) && digest(row.HistorySHA256) &&
			row.Cells >= 0 && row.Payloads >= 0 && row.Attachments >= 0 && row.LegacyPending >= 0 && row.LegacyStarted >= 0 &&
			row.HistoryMessages >= 0 && row.HistoryEvents >= 0 && row.HistoryTruncated >= 0 && row.HistoryEmptyMessages >= 0 &&
			row.HistoryTruncated <= row.HistoryMessages && row.HistoryEmptyMessages <= row.HistoryMessages &&
			!row.CompletedAt.IsZero() {
			return true
		}
	}
	return false
}

func freshBootstrap(ctx context.Context, db *gorm.DB, tables []string) bool {
	allowed := map[string]bool{"runs": true, "state_runs": true, "execution_datasets": true, "execution_quarantines": true,
		"database_fresh_origins": true, "payload_storage_control": true, "payload_storage_runtimes": true, "sqlite_sequence": true}
	for _, table := range tables {
		if !allowed[table] {
			return false
		}
		if table == "runs" || table == "state_runs" || table == "execution_quarantines" {
			var count int64
			if db.WithContext(ctx).Table(table).Count(&count).Error != nil || count != 0 {
				return false
			}
		}
	}
	return schemaFor(ctx, db, &models.Run{}, &models.StateRun{}) == nil
}

func currentSchema(ctx context.Context, db *gorm.DB) error {
	return schemaFor(ctx, db, models.AllModels()...)
}

func schemaFor(ctx context.Context, db *gorm.DB, modelSet ...any) error {
	for _, model := range modelSet {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		columns, err := db.WithContext(ctx).Migrator().ColumnTypes(stmt.Schema.Table)
		if err != nil || len(columns) == 0 {
			return fmt.Errorf("required application table %s is missing", stmt.Schema.Table)
		}
		present := make(map[string]bool, len(columns))
		for _, column := range columns {
			present[column.Name()] = true
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName != "" && !field.IgnoreMigration && !present[field.DBName] {
				return fmt.Errorf("required application column %s.%s is missing", stmt.Schema.Table, field.DBName)
			}
		}
	}
	return nil
}

func canonicalCells(ctx context.Context, db *gorm.DB) error {
	for _, model := range models.AllModels() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		fields, err := filestore.PayloadFields(stmt.Schema)
		if err != nil {
			return err
		}
		owned := make([]filestore.PayloadField, 0, len(fields))
		columns := make([]string, 0, len(fields)+1)
		for _, field := range fields {
			if field.Encoding != "" {
				owned = append(owned, field)
				columns = append(columns, stmt.Quote(field.Column))
			}
		}
		if len(owned) == 0 {
			continue // Ordinary serializer:json remains legitimate SQL metadata.
		}
		if len(stmt.Schema.PrimaryFields) != 1 {
			return fmt.Errorf("unsupported payload cursor for %s", stmt.Schema.Table)
		}
		pk := stmt.Schema.PrimaryFields[0].DBName
		columns = append(columns, stmt.Quote(pk))
		artifact := stmt.Schema.Table == "artifacts" || stmt.Schema.Table == "artifact_versions"
		if artifact {
			columns = append(columns, stmt.Quote("kind"))
		}
		var after any
		for {
			query := db.WithContext(ctx).Table(stmt.Schema.Table).Select(columns).
				Order(clause.OrderByColumn{Column: clause.Column{Name: pk}}).Limit(256)
			if after != nil {
				query = query.Where(clause.Gt{Column: clause.Column{Name: pk}, Value: after})
			}
			var rows []map[string]any
			if err := query.Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				for _, field := range owned {
					raw, err := cell(row[field.Column])
					if err != nil {
						return err
					}
					if raw == "" || !field.Explicit && field.Encoding == filestore.JSON && raw == "null" {
						continue
					}
					encoding, _, err := filestore.Parse(raw)
					if err != nil || !ownedEncoding(field, encoding, artifact, row["kind"]) {
						return fmt.Errorf("noncanonical payload in %s.%s", stmt.Schema.Table, field.Column)
					}
				}
			}
			if rows[len(rows)-1][pk] == nil {
				return fmt.Errorf("payload primary cursor is null in %s", stmt.Schema.Table)
			}
			after = rows[len(rows)-1][pk]
		}
	}
	return nil
}

func ownedEncoding(field filestore.PayloadField, encoding string, artifact bool, kindValue any) bool {
	if encoding == field.Encoding {
		return true
	}
	// Artifact content is a deliberate tagged union: the ordinary serializer
	// writes image bytes, and file import writes raw binary media in this same
	// historical column. Neither is an inline/legacy payload.
	if field.Explicit || !artifact || field.Column != "content" || field.Serializer != "filetext" {
		return false
	}
	kind, err := cell(kindValue)
	if err != nil {
		return false
	}
	if encoding == filestore.Image && kind == "image" {
		return true
	}
	return encoding == filestore.Binary && (kind == "file" || kind == "pdf" || kind == "audio" || kind == "video")
}

func cell(value any) (string, error) {
	switch value := value.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		return "", errors.New("payload cell has an unsupported SQL type")
	}
}

func retainedSoloPending(ctx context.Context, db *gorm.DB, upgraded bool) error {
	var count int64
	if err := db.WithContext(ctx).Table("chat_pending_turns").Where("surface = ?", "solo").Count(&count).Error; err != nil || count == 0 {
		return err
	}
	if !upgraded {
		return errors.New("retained Solo requests require a normalization receipt and quarantine")
	}
	var binding models.ExecutionDataset
	if err := db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error; err != nil || uuid.Validate(binding.DatasetID) != nil {
		return errors.New("retained Solo requests require explicit execution initialization")
	}
	var missing int64
	err := db.WithContext(ctx).Table("chat_pending_turns AS p").Where("p.surface = ?", "solo").
		Where("NOT EXISTS (SELECT 1 FROM execution_quarantines q WHERE q.dataset_id = ? AND q.entity_kind = ? AND q.entity_id = p.id AND q.review_state = ?)", binding.DatasetID, "legacy_pending", "pending").Count(&missing).Error
	if err != nil {
		return err
	}
	if missing != 0 {
		return errors.New("retained Solo requests lack current-dataset quarantine")
	}
	return nil
}

// The deadline limits diagnostics, not proof lifetime or execution authority.
const inspectTimeout = 30 * time.Second
