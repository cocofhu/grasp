// Command migrate-files externalizes and verifies legacy payloads. It is
// resumable and returns nonzero until every selected record has been verified.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
)

func main() {
	configPath := flag.String("config", "config.yaml", "server configuration file")
	flag.Parse()
	if err := run(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string) error {
	if err := config.Load(path); err != nil {
		return err
	}
	cfg := config.GetConfig()
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	report, err := filestore.Migrate(ctx, db, models.AllModels()...)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
		return encodeErr
	}
	if err != nil {
		return err
	}
	if len(report.Failures) > 0 || report.Deferred > 0 {
		return fmt.Errorf("migration incomplete: %d failures, %d concurrent updates; rerun to resume", len(report.Failures), report.Deferred)
	}
	return nil
}
