package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const offlineUpgradeProtocol = "grasp-offline-normalization/v1"

type upgradeCheckpoint struct {
	Version             int       `json:"version"`
	ID                  string    `json:"id"`
	ConfigurationSHA256 string    `json:"configurationSHA256"`
	Database            string    `json:"database"`
	DatabaseSHA256      string    `json:"databaseSHA256,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}

// upgradeDatabase deliberately leaves the offline fence active. A successful
// normalization receipt is not permission to resume an old execution, activate
// an execution lineage, remove old files, or release maintenance.
func upgradeDatabase(ctx context.Context, opts options, cfg *config.Config, db *gorm.DB, output io.Writer) error {
	lock, err := lockExecutionWorkdir(opts.workdir, !opts.resume)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	state, err := prepareUpgrade(opts, cfg)
	if err != nil {
		return err
	}
	if err := storageguard.Enter(ctx, db, state.ID, stoppedRuntimes(opts)); err != nil {
		return err
	}
	failed := func(err error) error {
		return fmt.Errorf("%w; upgrade maintenance %s remains active; resume the original workdir", err, state.ID)
	}
	if err := backupUpgrade(ctx, opts, cfg, db, &state); err != nil {
		return failed(err)
	}
	if err := database.UpgradeOfflineSchema(ctx, db, state.ID); err != nil {
		return failed(err)
	}
	storage, err := payloadstore.Open(ctx, db, cfg)
	if err != nil {
		return failed(err)
	}
	defer storage.Close()
	report, err := filestore.Migrate(ctx, db, models.AllModels()...)
	if err != nil {
		return failed(err)
	}
	if report.Deferred != 0 || len(report.Failures) != 0 {
		return failed(fmt.Errorf("payload normalization incomplete: %d deferred, %d invalid or unavailable cells; originals retained", report.Deferred, len(report.Failures)))
	}
	history, err := historymigration.ImportSoloEvents(ctx, db, state.ID)
	if err != nil {
		return failed(err)
	}
	if _, err := storage.ArchiveLegacy(ctx); err != nil {
		return failed(err)
	}
	if err := storageguard.Check(ctx, db, state.ID); err != nil {
		return failed(err)
	}
	objects, err := upgradeVerificationObjects(storage)
	if err != nil {
		return failed(err)
	}
	refs, err := payloadstore.VerifyReferences(ctx, db, objects)
	if err != nil {
		return failed(err)
	}
	receipt, err := completeUpgrade(ctx, db, state, refs, history)
	if err != nil {
		return failed(err)
	}
	if err := writeJSON(filepath.Join(opts.workdir, "upgrade-complete.json"), receipt); err != nil {
		return failed(err)
	}
	return json.NewEncoder(output).Encode(map[string]any{
		"completed": true, "maintenanceActive": true, "sourceRetained": true,
		"executionActivated": false, "soloJournalHistoryConverted": true, "legacyRunEventsRetained": true,
		"receipt": receipt, "normalization": report,
	})
}

func prepareUpgrade(opts options, cfg *config.Config) (upgradeCheckpoint, error) {
	var state upgradeCheckpoint
	identity, err := json.Marshal(struct {
		Database config.DatabaseConfig
		Storage  config.StorageConfig
	}{cfg.Database, cfg.Storage})
	if err != nil {
		return state, err
	}
	digest := sha256.Sum256(identity)
	fingerprint := hex.EncodeToString(digest[:])
	path := filepath.Join(opts.workdir, "upgrade.json")
	raw, err := os.ReadFile(path)
	if err == nil {
		if !opts.resume {
			return state, errors.New("upgrade checkpoint exists; use --resume with its original workdir")
		}
		if len(raw) > 16<<10 || json.Unmarshal(raw, &state) != nil || state.Version != 1 || uuid.Validate(state.ID) != nil ||
			state.ConfigurationSHA256 != fingerprint || (state.Database != "before.sqlite" && state.Database != "before.sql") {
			return state, errors.New("upgrade checkpoint is invalid or configuration differs")
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) || opts.resume {
		return state, errors.New("original upgrade checkpoint is missing or unreadable")
	}
	entries, err := os.ReadDir(opts.workdir)
	if err != nil {
		return state, err
	}
	for _, entry := range entries {
		if entry.Name() != ".lock" {
			return state, errors.New("upgrade requires a new private workdir")
		}
	}
	state = upgradeCheckpoint{Version: 1, ID: uuid.NewString(), ConfigurationSHA256: fingerprint,
		Database: "before.sqlite", CreatedAt: time.Now().UTC()}
	if cfg.Database.Driver == "mysql" {
		state.Database = "before.sql"
	}
	return state, writeJSON(path, state)
}

func backupUpgrade(ctx context.Context, opts options, cfg *config.Config, db *gorm.DB, state *upgradeCheckpoint) error {
	backup := filepath.Join(opts.workdir, state.Database)
	if cfg.Database.Driver != "mysql" {
		active, err := filepath.Abs(cfg.Database.Path)
		if err != nil {
			return err
		}
		if active == backup {
			return errors.New("upgrade backup cannot be the active database")
		}
	}
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
		if state.DatabaseSHA256 != "" {
			return errors.New("original upgrade backup is missing")
		}
		partial := backup + ".partial"
		if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := database.Snapshot(ctx, db, cfg.Database, state.ID, partial); err != nil {
			return err
		}
		if err := os.Rename(partial, backup); err != nil {
			return err
		}
		if err := syncDirectory(opts.workdir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	digest, err := payloadstore.FileSHA256(backup)
	if err != nil {
		return err
	}
	if state.DatabaseSHA256 != "" && state.DatabaseSHA256 != digest {
		return errors.New("original upgrade backup checksum changed")
	}
	state.DatabaseSHA256 = digest
	return writeJSON(filepath.Join(opts.workdir, "upgrade.json"), state)
}

func completeUpgrade(ctx context.Context, db *gorm.DB, state upgradeCheckpoint, refs payloadstore.ReferenceReport, history historymigration.Report) (database.OfflineUpgradeReceipt, error) {
	receipt := database.OfflineUpgradeReceipt{ID: state.ID, Protocol: offlineUpgradeProtocol,
		SourceVersion: "unrecorded", SourceDatabaseSHA256: state.DatabaseSHA256,
		ConfigurationSHA256: state.ConfigurationSHA256, ReferenceSHA256: refs.SHA256,
		HistoryProtocol: history.Protocol, HistoryMessages: history.Messages, HistoryEvents: history.Events,
		HistoryTruncated: history.PossiblyTruncated, HistoryEmptyMessages: history.EmptyMessages, HistorySHA256: history.SHA256,
		Cells: refs.Cells, Payloads: refs.Payloads, Attachments: refs.Attachments, CompletedAt: time.Now().UTC()}
	if err := storageguard.Check(ctx, db, state.ID); err != nil {
		return receipt, err
	}
	if err := db.WithContext(ctx).AutoMigrate(&database.OfflineUpgradeReceipt{}); err != nil {
		return receipt, err
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fenced := tx.Model(&storageguard.Control{}).Where("name = ? AND owner = ?", "primary", state.ID).
			UpdateColumn("revision", gorm.Expr("revision + 1"))
		if fenced.Error != nil {
			return fenced.Error
		}
		if fenced.RowsAffected != 1 {
			return errors.New("upgrade maintenance ownership lost before completion")
		}
		if err := tx.Table("chat_pending_turns").Count(&receipt.LegacyPending).Error; err != nil {
			return err
		}
		if err := tx.Table("chat_pending_turns").Where("started = ?", true).Count(&receipt.LegacyStarted).Error; err != nil {
			return err
		}
		if err := tx.Table("solo_messages").Where("events IS NOT NULL AND events NOT IN ? AND (event_log IS NULL OR event_log IN ?)", []string{"", "null", "[]"}, []string{"", "null"}).Count(&receipt.LegacyEvents).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt).Error; err != nil {
			return err
		}
		var saved database.OfflineUpgradeReceipt
		if err := tx.First(&saved, "id = ?", state.ID).Error; err != nil {
			return err
		}
		if saved.Protocol != receipt.Protocol || saved.SourceDatabaseSHA256 != receipt.SourceDatabaseSHA256 ||
			saved.ConfigurationSHA256 != receipt.ConfigurationSHA256 || saved.ReferenceSHA256 != receipt.ReferenceSHA256 ||
			saved.Cells != receipt.Cells || saved.Payloads != receipt.Payloads || saved.Attachments != receipt.Attachments ||
			saved.LegacyPending != receipt.LegacyPending || saved.LegacyStarted != receipt.LegacyStarted || saved.LegacyEvents != receipt.LegacyEvents {
			return errors.New("completed upgrade receipt disagrees with current normalized data")
		}
		if saved.HistoryProtocol != receipt.HistoryProtocol || saved.HistoryMessages != receipt.HistoryMessages || saved.HistoryEvents != receipt.HistoryEvents ||
			saved.HistoryTruncated != receipt.HistoryTruncated || saved.HistoryEmptyMessages != receipt.HistoryEmptyMessages || saved.HistorySHA256 != receipt.HistorySHA256 {
			return errors.New("completed upgrade history receipt disagrees with verified imports")
		}
		receipt = saved
		return nil
	})
	return receipt, err
}
