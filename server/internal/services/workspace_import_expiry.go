package services

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

const workspaceImportPreviewTTL = 24 * time.Hour
const workspaceImportExpiryBatch = 64

func workspaceImportExpired(row models.WorkspaceImport, now time.Time) bool {
	return row.State == "prepared" && row.ConfirmNonce == "" && row.ExpiresAt != nil && !row.ExpiresAt.IsZero() && !row.ExpiresAt.After(now)
}

// Retirement only removes the unaccepted preview's SQL root. Content-addressed
// objects may be shared or still being published elsewhere; physical GC remains
// disabled. Unknown/accepted imports and legacy rows without an expiry are kept.
// Scope and the bounded ID list are repeated in the CAS to race safely with
// Confirm. No object-store operation or background janitor is involved.
func (s *WorkspaceImportService) expirePrepared(ctx context.Context, soloID, actor, runID, id string) error {
	now := time.Now().UTC()
	query := s.db.WithContext(ctx).Model(&models.WorkspaceImport{}).
		Where("solo_id = ? AND actor_id = ? AND run_id = ? AND state = ? AND confirm_nonce = ''", soloID, actor, runID, "prepared").
		Where("expires_at IS NOT NULL AND expires_at > ? AND expires_at <= ?", time.Time{}, now)
	if id != "" {
		query = query.Where("id = ?", id)
	}
	var ids []string
	if err := query.Session(&gorm.Session{}).Order("expires_at ASC, id ASC").Limit(workspaceImportExpiryBatch).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return query.Where("id IN ?", ids).Updates(map[string]any{"state": "expired", "archive_ref": "", "error": "workspace_import_expired"}).Error
}
