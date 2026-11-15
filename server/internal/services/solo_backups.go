package services

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/solobackup"
	"gorm.io/gorm"
)

// Legacy archive records remain only as a data migration boundary. New saves
// are persisted on the owning sandbox and all recovery uses the newest point.
func (s *SoloService) sessionBackups(sessionID string) ([]models.SoloBackup, error) {
	var rows []models.SoloBackup
	err := s.db.Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (s *SoloService) legacyRestorePoint(sessionID string) (models.SoloBackup, error) {
	var b models.SoloBackup
	err := s.db.Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").First(&b).Error
	return b, err
}

// discardLegacyBackups retains each old database key until its object has
// actually gone, including releases that stored only SoloSession.BackupKey.
func discardLegacyBackups(ctx context.Context, db *gorm.DB, store solobackup.Store, row *models.SoloSession) error {
	var backups []models.SoloBackup
	if err := db.Where("session_id = ?", row.ID).Find(&backups).Error; err != nil {
		return err
	}
	known := make(map[string]bool, len(backups))
	for _, b := range backups {
		known[b.ObjectKey] = true
	}
	if row.BackupKey != "" && !known[row.BackupKey] {
		backups = append(backups, models.SoloBackup{Store: row.BackupStore, ObjectKey: row.BackupKey})
	}
	var failures []error
	for _, b := range backups {
		if store == nil || store.Kind() != b.Store {
			failures = append(failures, errors.New("archive storage is unavailable; cleanup will retry"))
			continue
		}
		if err := store.Delete(ctx, b.ObjectKey); err != nil {
			failures = append(failures, err)
			continue
		}
		if b.ID != 0 {
			if err := db.Delete(&models.SoloBackup{}, b.ID).Error; err != nil {
				failures = append(failures, err)
			}
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{
		"backup_store": "", "backup_key": "", "backup_sha256": "", "backup_size": 0, "backup_at": nil,
	}).Error
}

// Closed legacy sessions can have archives without a Sandbox row. Their
// persisted closed state is cancellation intent and must survive restarts too.
func (s *SandboxService) retryLegacyBackupCleanup(ctx context.Context) {
	var sessions []models.SoloSession
	if s.db.Where("status = ? AND (backup_key <> '' OR id IN (?))", models.SoloStatusClosed,
		s.db.Model(&models.SoloBackup{}).Select("session_id")).Find(&sessions).Error != nil {
		return
	}
	for _, row := range sessions {
		if ctx.Err() != nil {
			return
		}
		if discardLegacyBackups(ctx, s.db, s.backups, &row) != nil {
			continue
		}
		var remaining int64
		if s.db.Model(&models.Sandbox{}).Where("id = ?", row.SandboxID).Count(&remaining).Error == nil && remaining == 0 {
			s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", row.ID, models.SoloStatusClosed).Update("error", "")
		}
	}
}

// adoptLegacyBackups moves saved archives to the stable sandbox record. It
// does not delete stored data, and an interrupted migration is idempotent.
func (s *SoloService) adoptLegacyBackups() error {
	var sessions []models.SoloSession
	if err := s.db.Where("backup_key <> ''").Find(&sessions).Error; err != nil {
		return err
	}
	for _, session := range sessions {
		backups, err := s.sessionBackups(session.ID)
		if err != nil {
			return err
		}
		if len(backups) == 0 {
			legacy := models.SoloBackup{SessionID: session.ID, Store: session.BackupStore, ObjectKey: session.BackupKey, SHA256: session.BackupSHA256, Size: session.BackupSize, Reason: "stop"}
			if session.BackupAt != nil {
				legacy.CreatedAt = *session.BackupAt
			}
			if err := s.db.Create(&legacy).Error; err != nil {
				return err
			}
			backups = []models.SoloBackup{legacy}
		}
		var sb models.Sandbox
		if session.SandboxID == 0 || s.db.First(&sb, session.SandboxID).Error != nil {
			continue
		}
		known := map[string]bool{}
		for _, p := range sb.RecoveryPoints {
			known[p.Key] = true
		}
		for _, b := range backups {
			if !known[b.ObjectKey] {
				sb.RecoveryPoints = append(sb.RecoveryPoints, models.SandboxRecoveryPoint{Store: b.Store, Key: b.ObjectKey, SHA256: b.SHA256, Size: b.Size, CreatedAt: b.CreatedAt})
				known[b.ObjectKey] = true
			}
		}
		if err := s.db.Model(&models.Sandbox{ID: sb.ID}).Select("recovery_points").Updates(&models.Sandbox{RecoveryPoints: sb.RecoveryPoints}).Error; err != nil {
			return err
		}
		if err := s.db.Where("session_id = ?", session.ID).Delete(&models.SoloBackup{}).Error; err != nil {
			return err
		}
		if err := s.db.Model(&models.SoloSession{}).Where("id = ?", session.ID).Updates(map[string]any{"backup_key": "", "backup_store": "", "backup_sha256": ""}).Error; err != nil {
			return err
		}
	}
	return nil
}
