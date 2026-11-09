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
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type closureCheckpoint struct {
	Version             int       `json:"version"`
	ID                  string    `json:"id"`
	PhysicalFingerprint string    `json:"physicalFingerprint"`
	DatabaseFingerprint string    `json:"databaseFingerprint"`
	CreatedAt           time.Time `json:"createdAt"`
}

func closureDatabaseIdentity(cfg config.DatabaseConfig) (string, error) {
	var identity string
	if cfg.Driver == "mysql" {
		dsn, err := mysql.ParseDSN(cfg.DSN)
		if err != nil {
			return "", errors.New("invalid SQL identity for primary certification")
		}
		identity = "mysql\x00" + dsn.Net + "\x00" + dsn.Addr + "\x00" + dsn.DBName
	} else {
		path, err := filepath.Abs(cfg.Path)
		if err != nil {
			return "", err
		}
		identity = "sqlite\x00" + path
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:]), nil
}

func prepareClosure(opts options, cfg *config.Config) (closureCheckpoint, error) {
	var state closureCheckpoint
	location, err := payloadstore.PrimaryLocation(cfg.Storage)
	if err != nil || location.Driver != "s3" {
		return state, errors.New("certify-primary applies to a configured remote primary; local storage needs no remote certificate")
	}
	databaseID, err := closureDatabaseIdentity(cfg.Database)
	if err != nil {
		return state, err
	}
	path := filepath.Join(opts.workdir, "primary-closure.json")
	raw, err := os.ReadFile(path)
	if err == nil {
		if !opts.resume || len(raw) > 16<<10 || json.Unmarshal(raw, &state) != nil || state.Version != 1 ||
			uuid.Validate(state.ID) != nil || state.CreatedAt.IsZero() || state.DatabaseFingerprint != databaseID ||
			state.PhysicalFingerprint != location.Fingerprint {
			return state, errors.New("original primary certificate checkpoint differs; resume the same SQL/physical primary and workdir")
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) || opts.resume {
		return state, errors.New("original primary certificate checkpoint is missing or unreadable")
	}
	entries, err := os.ReadDir(opts.workdir)
	if err != nil {
		return state, err
	}
	for _, entry := range entries {
		if entry.Name() != ".lock" {
			return state, errors.New("primary certification requires a new private workdir")
		}
	}
	state = closureCheckpoint{Version: 1, ID: uuid.NewString(), PhysicalFingerprint: location.Fingerprint,
		DatabaseFingerprint: databaseID, CreatedAt: time.Now().UTC()}
	return state, writeJSON(path, state)
}

func readClosureReceipt(opts options, state closureCheckpoint) (payloadstore.PrimaryClosure, error) {
	var row payloadstore.PrimaryClosure
	raw, err := os.ReadFile(filepath.Join(opts.workdir, "primary-closure-complete.json"))
	if err != nil || len(raw) > 16<<10 || json.Unmarshal(raw, &row) != nil || row.ID != state.ID ||
		row.Protocol != payloadstore.PrimaryClosureProtocol || row.PhysicalFingerprint != state.PhysicalFingerprint || row.CompletedAt.IsZero() {
		return row, errors.New("original completed primary certificate is missing or invalid; resume certification first")
	}
	return row, nil
}

func sameClosureReceipt(a, b payloadstore.PrimaryClosure) bool {
	return a.ID == b.ID && a.Protocol == b.Protocol && a.PhysicalFingerprint == b.PhysicalFingerprint &&
		a.ReferenceSHA256 == b.ReferenceSHA256 && a.Cells == b.Cells && a.Payloads == b.Payloads &&
		a.Attachments == b.Attachments && a.CompletedAt.Equal(b.CompletedAt)
}

func certifyPrimary(ctx context.Context, opts options, cfg *config.Config, db *gorm.DB, output io.Writer) error {
	lock, err := lockExecutionWorkdir(opts.workdir, !opts.resume)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	state, err := prepareClosure(opts, cfg)
	if err != nil {
		return err
	}
	var previous payloadstore.PrimaryClosure
	_, receiptErr := os.Stat(filepath.Join(opts.workdir, "primary-closure-complete.json"))
	hasReceipt := receiptErr == nil
	if receiptErr != nil && !errors.Is(receiptErr, os.ErrNotExist) {
		return receiptErr
	}
	if opts.finalize || hasReceipt {
		previous, err = readClosureReceipt(opts, state)
		if err != nil {
			return err
		}
	}
	// The SQL final stamp survives a crash after Leave but before the response
	// or local receipt update. Reading that original result never re-enters or
	// clears another operation's maintenance, nor performs Initialize/Activate.
	hasTable := db.WithContext(ctx).Migrator().HasTable(&payloadstore.PrimaryClosure{})
	if hasReceipt && !hasTable {
		return errors.New("original completed primary certificate SQL table is missing")
	}
	if hasTable {
		var saved payloadstore.PrimaryClosure
		err := db.WithContext(ctx).First(&saved, "id = ?", state.ID).Error
		if hasReceipt && (err != nil || !sameClosureReceipt(previous, saved)) {
			return errors.New("original SQL primary certificate differs from its completed file receipt")
		}
		if err == nil && saved.FinalizedAt != nil {
			if saved.PhysicalFingerprint != state.PhysicalFingerprint || saved.Protocol != payloadstore.PrimaryClosureProtocol ||
				!hasReceipt || !sameClosureReceipt(previous, saved) {
				return errors.New("finalized primary certificate differs from its original intent/receipt")
			}
			if err := payloadstore.CheckPrimaryClosure(ctx, db, cfg.Storage); err != nil {
				return err
			}
			return json.NewEncoder(output).Encode(map[string]any{"completed": true, "finalized": true, "originalReceipt": true, "executionUnchanged": true, "receipt": saved})
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if err := storageguard.Enter(ctx, db, state.ID, stoppedRuntimes(opts)); err != nil {
		return err
	}
	failure := func(err error) error {
		return fmt.Errorf("%w; primary certification maintenance %s remains active; resume the original workdir", err, state.ID)
	}
	var row payloadstore.PrimaryClosure
	if opts.finalize {
		if err := db.WithContext(ctx).First(&row, "id = ?", state.ID).Error; err != nil {
			return failure(err)
		}
		if !sameClosureReceipt(previous, row) || previous.FinalizedAt != nil {
			return failure(errors.New("completed certificate file differs from the original SQL receipt"))
		}
		row, err = payloadstore.FinalizePrimaryClosure(ctx, db, cfg.Storage, state.ID)
	} else {
		row, err = payloadstore.CertifyPrimary(ctx, db, cfg.Storage, state.ID)
	}
	if err != nil {
		return failure(err)
	}
	if hasReceipt && !sameClosureReceipt(previous, row) {
		return failure(errors.New("completed primary certificate changed during resume"))
	}
	if err := writeJSON(filepath.Join(opts.workdir, "primary-closure-complete.json"), row); err != nil {
		return err // SQL's original final stamp makes this recoverable.
	}
	return json.NewEncoder(output).Encode(map[string]any{"completed": true, "maintenanceActive": !opts.finalize,
		"finalized": opts.finalize, "sourceRetained": true, "executionUnchanged": true, "receipt": row})
}
