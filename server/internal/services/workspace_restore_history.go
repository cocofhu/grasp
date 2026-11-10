package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// Resolve observes the original nonce; a missing receipt does not submit it.
func (s *WorkspaceRestoreService) Resolve(ctx context.Context, actor, soloID, nonce string) (row models.WorkspaceRestore, err error) {
	if !snapshotCatalogCoordinate(actor, workspaceKeyMax) || !snapshotCatalogCoordinate(soloID, workspaceKeyMax) || !workspaceJobID.MatchString(nonce) {
		return row, gorm.ErrRecordNotFound
	}
	receipt, err := s.Get(ctx, actor, workspaceRestoreIdentity(actor, soloID, nonce))
	if err != nil || receipt.SoloID != soloID {
		if err == nil {
			err = gorm.ErrRecordNotFound
		}
		return row, err
	}
	return receipt, nil
}

type WorkspaceRestoreHistoryPage struct {
	Items      []models.WorkspaceRestore
	NextCursor string
	HasMore    bool
}

func (s *WorkspaceRestoreService) List(ctx context.Context, actor, soloID, cursor string, limit int) (WorkspaceRestoreHistoryPage, error) {
	if !snapshotCatalogCoordinate(actor, workspaceKeyMax) || !snapshotCatalogCoordinate(soloID, workspaceKeyMax) {
		return WorkspaceRestoreHistoryPage{}, gorm.ErrRecordNotFound
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return WorkspaceRestoreHistoryPage{}, ErrWorkspaceJobInvalid
	}
	query := s.db.WithContext(ctx).Where("actor_id = ? AND solo_id = ?", actor, soloID)
	if cursor != "" {
		if !workspaceJobID.MatchString(cursor) {
			return WorkspaceRestoreHistoryPage{}, ErrWorkspaceJobInvalid
		}
		var before models.WorkspaceRestore
		if err := s.db.WithContext(ctx).Select("id", "created_at").Take(&before, "id = ? AND actor_id = ? AND solo_id = ?", cursor, actor, soloID).Error; err != nil {
			return WorkspaceRestoreHistoryPage{}, err
		}
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", before.CreatedAt, before.CreatedAt, before.ID)
	}
	var rows []models.WorkspaceRestore
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return WorkspaceRestoreHistoryPage{}, err
	}
	page := WorkspaceRestoreHistoryPage{Items: make([]models.WorkspaceRestore, 0, min(limit, len(rows))), HasMore: len(rows) > limit}
	if page.HasMore {
		rows = rows[:limit]
	}
	for _, row := range rows {
		current, err := s.Get(ctx, actor, row.ID)
		if err != nil {
			return WorkspaceRestoreHistoryPage{}, err
		}
		page.Items = append(page.Items, current)
	}
	if page.HasMore {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}
