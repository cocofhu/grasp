package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/solobackup"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	defaultSoloBackupKeep = 5
	soloBackupTick        = 5 * time.Minute
)

// errBackupSuperseded drops an archive whose session moved on while it was
// taken: a scheduled archive must not outrank the stop archive taken meanwhile.
var errBackupSuperseded = errors.New("solo session changed while its workspace was archived")

// SetBackupPolicy sets how many archives each session keeps and how often a
// running session with new activity is archived. interval <= 0 disables the
// schedule; archives are then taken only on stop.
func (s *SoloService) SetBackupPolicy(keep int, interval time.Duration) {
	s.backupKeep, s.backupInterval = keep, interval
}

func (s *SoloService) keepBackups() int {
	if s.backupKeep <= 0 {
		return defaultSoloBackupKeep
	}
	return s.backupKeep
}

func newBackupKey(sessionID string) string {
	return fmt.Sprintf("solo/%s/%s.tar.gz", sessionID, uuid.NewString())
}

// recordBackup makes a stored archive the session's newest restore point and
// prunes the oldest beyond the retention count. whileRunning discards the
// archive if the session stopped while it was taken.
func (s *SoloService) recordBackup(ctx context.Context, sessionID, key string, obj solobackup.Object, reason string, whileRunning bool) error {
	b := models.SoloBackup{SessionID: sessionID, Store: s.backups.Kind(), ObjectKey: key, SHA256: obj.SHA256, Size: obj.Size, Reason: reason}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&b).Error; err != nil {
			return err
		}
		q := tx.Model(&models.SoloSession{}).Where("id = ?", sessionID)
		if whileRunning {
			q = q.Where("status = ?", models.SoloStatusRunning)
		}
		res := q.Updates(map[string]any{
			"backup_store": b.Store, "backup_key": b.ObjectKey, "backup_sha256": b.SHA256,
			"backup_size": b.Size, "backup_at": &b.CreatedAt,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errBackupSuperseded
		}
		return nil
	})
	if err != nil {
		s.deleteBackup(ctx, key)
		return err
	}
	s.pruneBackups(ctx, sessionID)
	return nil
}

func (s *SoloService) sessionBackups(sessionID string) ([]models.SoloBackup, error) {
	var rows []models.SoloBackup
	err := s.db.Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (s *SoloService) pruneBackups(ctx context.Context, sessionID string) {
	rows, err := s.sessionBackups(sessionID)
	if err != nil || len(rows) <= s.keepBackups() {
		return
	}
	for _, b := range rows[s.keepBackups():] {
		s.removeBackup(ctx, b)
	}
}

// removeBackup deletes the record and its object. An object in a store that
// is no longer configured cannot be reached and is left in place.
func (s *SoloService) removeBackup(ctx context.Context, b models.SoloBackup) {
	if err := s.db.Delete(&models.SoloBackup{}, b.ID).Error; err != nil {
		log.Warn().Err(err).Str("key", b.ObjectKey).Msg("solo backup record delete failed")
		return
	}
	if s.backups != nil && b.Store == s.backups.Kind() {
		s.deleteBackup(ctx, b.ObjectKey)
		return
	}
	log.Warn().Str("key", b.ObjectKey).Str("store", b.Store).Msg("solo backup left in a store that is no longer configured")
}

// restorePoint returns the chosen archive of a session, or its newest when id is 0.
func (s *SoloService) restorePoint(sessionID string, id uint) (models.SoloBackup, error) {
	var b models.SoloBackup
	q := s.db.Where("session_id = ?", sessionID)
	if id != 0 {
		q = q.Where("id = ?", id)
	}
	err := q.Order("created_at DESC, id DESC").First(&b).Error
	return b, err
}

// ListBackups returns a session's workspace archives, newest first.
func (s *SoloService) ListBackups(id, user string) ([]models.SoloBackup, error) {
	if _, err := s.Require(id, user); err != nil {
		return nil, err
	}
	return s.sessionBackups(id)
}

// adoptLegacyBackups lists archives taken before backups had their own table,
// so retention and restore choice cover them.
func (s *SoloService) adoptLegacyBackups() error {
	var rows []models.SoloSession
	if err := s.db.Where("backup_key <> ''").Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		var n int64
		if err := s.db.Model(&models.SoloBackup{}).Where("session_id = ? AND object_key = ?", r.ID, r.BackupKey).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		b := models.SoloBackup{SessionID: r.ID, Store: r.BackupStore, ObjectKey: r.BackupKey, SHA256: r.BackupSHA256, Size: r.BackupSize, Reason: models.SoloBackupReasonStop}
		if r.BackupAt != nil {
			b.CreatedAt = *r.BackupAt
		}
		if err := s.db.Create(&b).Error; err != nil {
			return err
		}
	}
	return nil
}

// RunBackupSchedule archives running sessions that had activity since their
// last archive once the backup interval has passed. A session in a turn waits
// for it to end, and a paused sandbox is not woken for a backup.
func (s *SoloService) RunBackupSchedule(ctx context.Context) {
	if s.backupInterval <= 0 || s.backups == nil || s.sbx == nil {
		return
	}
	ticker := time.NewTicker(soloBackupTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runScheduledBackups(ctx)
		}
	}
}

func (s *SoloService) runScheduledBackups(ctx context.Context) {
	var rows []models.SoloSession
	if err := s.db.Where("status = ? AND sandbox_id <> 0", models.SoloStatusRunning).Find(&rows).Error; err != nil {
		return
	}
	cutoff := time.Now().Add(-s.backupInterval)
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		s.scheduledBackup(ctx, row, cutoff)
	}
}

func (s *SoloService) scheduledBackup(ctx context.Context, row models.SoloSession, cutoff time.Time) {
	since := row.CreatedAt
	if row.BackupAt != nil {
		since = *row.BackupAt
	}
	if !since.Before(cutoff) || row.LastActiveAt == nil || !row.LastActiveAt.After(since) {
		return
	}
	if c := s.lookup(row.ID); c != nil {
		if sn := c.sess.Snapshot(); sn.Busy || sn.Waiting > 0 {
			return
		}
	}
	if s.sbx.SoloSandboxIsLocal(row.SandboxID) || !s.sbx.SoloSandboxRunning(ctx, row.SandboxID) {
		return
	}
	s.backupMu.Lock()
	if last, ok := s.backupTried[row.ID]; ok && last.After(cutoff) {
		s.backupMu.Unlock()
		return
	}
	if s.backupTried == nil {
		s.backupTried = map[string]time.Time{}
	}
	s.backupTried[row.ID] = time.Now()
	s.backupMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, soloStopTimeout)
	defer cancel()
	key := newBackupKey(row.ID)
	obj, err := s.archiveWorkspace(ctx, row.SandboxID, key)
	if err == nil {
		err = s.recordBackup(ctx, row.ID, key, obj, models.SoloBackupReasonScheduled, true)
	}
	switch {
	case err == nil:
		s.publishStatus(row.ID)
	case !errors.Is(err, errBackupSuperseded):
		log.Warn().Err(err).Str("session", row.ID).Msg("scheduled solo backup failed; retrying after the backup interval")
	}
}
