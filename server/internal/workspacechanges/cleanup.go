package workspacechanges

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A publisher has a bounded capture context. An uncommitted directory can
// be reclaimed after that context even if the server crashed before aborting.
func (s *Service) stageArchive(ctx context.Context, id string) (context.Context, error) {
	store, err := filestore.FromDB(s.db)
	if err != nil {
		return nil, err
	}
	entry := models.WorkspaceChangeGarbage{ID: id, ReadyAt: time.Now().UTC().Add(5 * time.Minute)}
	if err := s.db.WithContext(ctx).Create(&entry).Error; err != nil {
		return nil, err
	}
	scoped, err := store.PrepareWorkspaceArchive(ctx, id)
	if err != nil {
		s.abandonArchive(ctx, id)
	}
	return scoped, err
}

func (s *Service) abandonArchive(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.db.WithContext(ctx).Model(&models.WorkspaceChangeGarbage{}).
		Where("id = ?", id).UpdateColumn("ready_at", time.Now().UTC()).Error; err != nil {
		log.Warn().Err(err).Str("archive", id).Msg("schedule workspace archive cleanup")
	}
}

func retireArchives(tx *gorm.DB, ids []string) error {
	entries := make([]models.WorkspaceChangeGarbage, 0, len(ids))
	for _, id := range ids {
		if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id {
			entries = append(entries, models.WorkspaceChangeGarbage{ID: id, ReadyAt: time.Now().UTC()})
		}
	}
	if len(entries) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&entries, 100).Error
}

// RunCleanup drains the SQL retirement queue on startup and periodically.
func (s *Service) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := s.collectArchives(ctx); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("collect workspace archives")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) collectArchives(ctx context.Context) error {
	store, err := filestore.FromDB(s.db)
	if err != nil {
		return err
	}
	cutoff, after := time.Now().UTC(), ""
	const batchSize = 100
	for ctx.Err() == nil {
		var entries []models.WorkspaceChangeGarbage
		if err := s.db.WithContext(ctx).Where("ready_at <= ? AND id > ?", cutoff, after).
			Order("id").Limit(batchSize).Find(&entries).Error; err != nil {
			return err
		}
		for _, entry := range entries {
			if err := store.RemoveWorkspaceArchive(ctx, entry.ID); err != nil {
				log.Warn().Err(err).Str("archive", entry.ID).Msg("remove workspace archive")
				continue
			}
			if err := s.db.WithContext(ctx).Delete(&entry).Error; err != nil {
				return err
			}
		}
		if len(entries) < batchSize {
			break
		}
		after = entries[len(entries)-1].ID
	}
	return ctx.Err()
}
