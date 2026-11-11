package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
)

// initializationAfterUpgrade runs inside execution-init's existing workdir
// lock, before it initializes the SQL execution binding. It only verifies the
// original normalization receipt; it cannot create or repair missing history.
func initializationAfterUpgrade(ctx context.Context, opts options, cfg *config.Config, proposed initialization, existing bool) (initialization, error) {
	if _, err := os.Stat(filepath.Join(opts.workdir, "upgrade.json")); errors.Is(err, os.ErrNotExist) {
		return proposed, nil // Existing fresh-install/restore initialization path.
	} else if err != nil {
		return proposed, err
	}
	resume := opts
	resume.resume = true
	state, err := prepareUpgrade(resume, cfg)
	if err != nil {
		return proposed, err
	}
	if existing && proposed.ID != state.ID {
		return proposed, errors.New("execution initialization does not belong to this upgrade receipt")
	}
	raw, err := os.ReadFile(filepath.Join(opts.workdir, "upgrade-complete.json"))
	if err != nil {
		return proposed, errors.New("upgrade is incomplete; resume upgrade in the original workdir before execution-init")
	}
	var receipt database.OfflineUpgradeReceipt
	if len(raw) > 64<<10 || json.Unmarshal(raw, &receipt) != nil || receipt.ID != state.ID || receipt.Protocol != offlineUpgradeProtocol ||
		receipt.ConfigurationSHA256 != state.ConfigurationSHA256 || receipt.SourceDatabaseSHA256 != state.DatabaseSHA256 || receipt.CompletedAt.IsZero() {
		return proposed, errors.New("invalid completed upgrade receipt")
	}
	digest, err := payloadstore.FileSHA256(filepath.Join(opts.workdir, state.Database))
	if err != nil || digest != receipt.SourceDatabaseSHA256 {
		return proposed, errors.New("original upgrade SQL backup is missing or changed")
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		return proposed, err
	}
	defer func() { _ = database.Close(db) }()
	var saved database.OfflineUpgradeReceipt
	if err := db.WithContext(ctx).First(&saved, "id = ?", receipt.ID).Error; err != nil || !sameUpgradeReceipt(saved, receipt) {
		return proposed, errors.New("SQL and workdir upgrade receipts disagree")
	}
	var control storageguard.Control
	if err := db.WithContext(ctx).First(&control, "name = ?", "primary").Error; err != nil {
		return proposed, err
	}
	if control.Owner == "" && existing {
		// An exact retry after successful finalization can only use the
		// already-active SQL identity, without creating a new generation.
		var binding models.ExecutionDataset
		if err := db.WithContext(ctx).First(&binding, "id = ?", "primary").Error; err != nil || binding.OperationID != state.ID {
			return proposed, errors.New("upgrade initialization has no matching completed execution binding")
		}
		authority, err := executionlineage.OpenSQL(ctx, db)
		if err != nil {
			return proposed, err
		}
		permit, err := authority.ReadActive(ctx)
		if err != nil {
			return proposed, err
		}
		if err := authority.CheckBinding(ctx, db, permit); err != nil {
			return proposed, err
		}
		return proposed, nil
	}
	if control.Owner != state.ID {
		return proposed, errors.New("original upgrade maintenance ownership is not retained")
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return proposed, err
	}
	storage := &payloadstore.Storage{Files: files}
	if cfg.Storage.Driver == "s3" || cfg.Storage.Driver == "cos" {
		storage.Objects, err = payloadstore.OpenObjects(ctx, cfg.Storage)
		if err != nil {
			return proposed, err
		}
		if err := files.SetObjectStore(storage.Objects); err != nil {
			return proposed, err
		}
	} else {
		if info, err := os.Stat(cfg.Storage.BlobsRoot); err != nil || !info.IsDir() {
			return proposed, errors.New("original local attachment root is unavailable")
		}
		storage.Blobs, err = blob.NewLocalFS(cfg.Storage.BlobsRoot)
		if err != nil {
			return proposed, err
		}
	}
	history, err := historymigration.VerifySoloEvents(ctx, db, state.ID)
	if err != nil {
		return proposed, err
	}
	if history.Protocol != receipt.HistoryProtocol || history.Messages != receipt.HistoryMessages || history.Events != receipt.HistoryEvents ||
		history.PossiblyTruncated != receipt.HistoryTruncated || history.EmptyMessages != receipt.HistoryEmptyMessages || history.SHA256 != receipt.HistorySHA256 {
		return proposed, errors.New("upgrade history no longer matches the completed import")
	}
	objects, err := upgradeVerificationObjects(storage)
	if err != nil {
		return proposed, err
	}
	refs, err := payloadstore.VerifyReferences(ctx, db, objects)
	if err != nil {
		return proposed, err
	}
	if refs.Cells != receipt.Cells || refs.Payloads != receipt.Payloads || refs.Attachments != receipt.Attachments || refs.SHA256 != receipt.ReferenceSHA256 {
		return proposed, errors.New("upgrade reference closure no longer matches its completion receipt")
	}
	if err := storageguard.Check(ctx, db, state.ID); err != nil {
		return proposed, err
	}
	proposed.ID = state.ID
	return proposed, nil
}

func sameUpgradeReceipt(a, b database.OfflineUpgradeReceipt) bool {
	if !a.CompletedAt.Equal(b.CompletedAt) {
		return false
	}
	a.CompletedAt, b.CompletedAt = time.Time{}, time.Time{}
	return a == b
}
