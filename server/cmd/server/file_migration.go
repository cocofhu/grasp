package main

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

func migrateFilePayloads(ctx context.Context, db *gorm.DB) {
	for {
		report, err := filestore.Migrate(ctx, db, models.AllModels()...)
		if ctx.Err() != nil {
			return
		}
		if err == nil && len(report.Failures) == 0 && report.Deferred == 0 {
			log.Info().Int("migrated", report.Migrated).Int("verified", report.Verified).Int64("bytes", report.Bytes).Msg("file payload migration verified")
			return
		}
		log.Error().Err(err).Int("migrated", report.Migrated).Int("deferred", report.Deferred).Strs("failures", report.Failures).Msg("file payload migration incomplete; legacy reads remain available")
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
