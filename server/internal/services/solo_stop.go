package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/solobackup"

	"github.com/rs/zerolog/log"
)

// soloStopTimeout bounds archiving and uploading one workspace.
const soloStopTimeout = 60 * time.Minute

// SetBackupStore wires where stopped workspaces are archived.
func (s *SoloService) SetBackupStore(store solobackup.Store) { s.backups = store }

// Stop archives the workspace and then stops the sandbox. It returns once the
// session is "stopping"; the archive runs in the background. A failed archive
// leaves the sandbox running.
func (s *SoloService) Stop(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	switch row.Status {
	case models.SoloStatusStopped, models.SoloStatusStopping:
		return s.Get(ctx, id, user)
	case models.SoloStatusRunning:
	default:
		return nil, soloErr(http.StatusConflict, "session_not_running", "会话未在运行，无法停止")
	}
	if s.backups == nil || s.sbx == nil || row.SandboxID == 0 {
		return nil, soloErr(http.StatusServiceUnavailable, "backup_unavailable", "工作区备份不可用")
	}
	if s.sbx.SoloSandboxIsLocal(row.SandboxID) {
		return nil, soloErr(http.StatusConflict, "stop_unsupported", "本地工作区会话不支持停止")
	}
	if c := s.lookup(id); c != nil {
		if sn := c.sess.Snapshot(); sn.Busy || sn.Waiting > 0 {
			return nil, soloErr(http.StatusConflict, "session_busy", "请等待当前回复结束后再停止")
		}
	}
	res := s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", id, models.SoloStatusRunning).
		Updates(map[string]any{"status": models.SoloStatusStopping, "error": ""})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, soloErr(http.StatusConflict, "session_not_running", "会话未在运行，无法停止")
	}
	s.publishStatus(id)
	go s.finishStop(*row)
	return s.Get(ctx, id, user)
}

func (s *SoloService) finishStop(row models.SoloSession) {
	ctx, cancel := context.WithTimeout(context.Background(), soloStopTimeout)
	defer cancel()
	key := newBackupKey(row.ID)
	obj, err := s.archiveWorkspace(ctx, row.SandboxID, key)
	if err == nil {
		if err = s.sbx.StopSolo(ctx, row.SandboxID); err != nil {
			s.deleteBackup(ctx, key)
		}
	}
	if err != nil {
		log.Warn().Err(err).Str("session", row.ID).Msg("solo stop failed; sandbox kept running")
		msg := "停止失败，会话保持运行：" + truncErr(err)
		if errors.Is(err, solobackup.ErrTooLarge) {
			msg = "工作区超过备份上限，无法停止；请清理大文件后重试"
		}
		s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", row.ID, models.SoloStatusStopping).
			Updates(map[string]any{"status": models.SoloStatusRunning, "error": msg})
		s.appendSystem(row.ID, msg)
		s.publishStatus(row.ID)
		return
	}
	if err := s.recordBackup(ctx, row.ID, key, obj, models.SoloBackupReasonStop, false); err != nil {
		log.Warn().Err(err).Str("session", row.ID).Msg("solo stop archive not recorded; the stopped sandbox is still resumable")
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{"status": models.SoloStatusStopped, "error": ""})
	s.appendSystem(row.ID, "会话已停止，工作区已备份")
	s.publishStatus(row.ID)
}

func (s *SoloService) archiveWorkspace(ctx context.Context, sandboxID uint, key string) (solobackup.Object, error) {
	rc, err := s.sbx.ArchiveSoloWorkspace(ctx, sandboxID)
	if err != nil {
		return solobackup.Object{}, err
	}
	defer func() { _ = rc.Close() }()
	return s.backups.Put(ctx, key, rc)
}

func (s *SoloService) deleteBackup(ctx context.Context, key string) {
	if s.backups == nil || key == "" {
		return
	}
	if err := s.backups.Delete(ctx, key); err != nil {
		log.Warn().Err(err).Str("key", key).Msg("solo backup delete failed")
	}
}

// discardBackup removes every archive of a session that is being closed or
// deleted.
func (s *SoloService) discardBackup(ctx context.Context, row *models.SoloSession) {
	backups, _ := s.sessionBackups(row.ID)
	if len(backups) == 0 && row.BackupKey == "" {
		return
	}
	for _, b := range backups {
		s.removeBackup(ctx, b)
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{
		"backup_store": "", "backup_key": "", "backup_sha256": "", "backup_size": 0, "backup_at": nil,
	})
}

// Resume starts a stopped session. The original sandbox is started when it
// still exists (E2B keeps processes; Docker/Kubernetes keep files). Otherwise
// a new sandbox is created and the workspace restored from the newest archive.
// A backupID other than the newest replaces the sandbox with that archive.
func (s *SoloService) Resume(ctx context.Context, id, user string, backupID uint) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status != models.SoloStatusStopped {
		return nil, soloErr(http.StatusConflict, "session_not_stopped", "会话未停止，无需恢复")
	}
	if backupID != 0 {
		if _, err := s.restorePoint(id, backupID); err != nil {
			return nil, soloErr(http.StatusNotFound, "backup_not_found", "工作区备份不存在")
		}
		if newest, err := s.restorePoint(id, 0); err == nil && newest.ID == backupID {
			backupID = 0
		}
	}
	if s.sbx == nil || s.agents == nil {
		return nil, errors.New("solo unavailable")
	}
	if _, ok := s.agents.Get(row.AgentName); !ok {
		return nil, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 已不存在，无法恢复", row.AgentName)
	}
	if s.sbx.activeSoloSandboxCount() >= s.sbx.MaxSoloSandboxes() {
		return nil, &SoloError{Status: http.StatusConflict, Code: "solo_limit", Msg: ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error()}
	}
	now := time.Now()
	res := s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", id, models.SoloStatusStopped).
		Updates(map[string]any{"status": models.SoloStatusStarting, "error": "", "last_active_at": &now})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, soloErr(http.StatusConflict, "session_not_stopped", "会话未停止，无需恢复")
	}
	s.publishStatus(id)
	go s.finishResume(*row, backupID)
	return s.Get(ctx, id, user)
}

func (s *SoloService) finishResume(row models.SoloSession, backupID uint) {
	ctx := context.Background()
	if row.SandboxID != 0 && backupID != 0 {
		s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Update("sandbox_id", 0)
		s.destroySandbox(ctx, row.SandboxID)
		row.SandboxID = 0
	}
	if row.SandboxID != 0 {
		warm, err := s.sbx.ResumeSolo(ctx, row.SandboxID)
		if err == nil {
			updates := map[string]any{"status": models.SoloStatusRunning, "error": ""}
			if !warm && s.hasDialogue(row.ID) {
				updates["resume_context_pending"] = true
			}
			// The recovered hook may already have marked the session running.
			s.db.Model(&models.SoloSession{}).Where("id = ? AND status IN ?", row.ID,
				[]string{models.SoloStatusStarting, models.SoloStatusRunning}).Updates(updates)
			if warm {
				s.appendSystem(row.ID, "会话已恢复，进程和工作区保持停止前的状态")
			} else {
				s.appendSystem(row.ID, "会话已恢复，工作区已保留；进程已重新启动")
			}
			s.publishStatus(row.ID)
			return
		}
		if !errors.Is(err, ErrSoloSandboxGone) {
			s.failResume(row.ID, err)
			return
		}
		// Unbind first: destroying the stale row must not mark this session failed.
		s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Update("sandbox_id", 0)
		s.destroySandbox(ctx, row.SandboxID)
	}
	backup, err := s.restorePoint(row.ID, backupID)
	if err != nil || s.backups == nil {
		s.failResume(row.ID, errors.New("沙箱已不存在，且没有可用的工作区备份"))
		return
	}
	if backup.Store != s.backups.Kind() {
		s.failResume(row.ID, fmt.Errorf("工作区备份保存在 %s 存储，当前配置为 %s", backup.Store, s.backups.Kind()))
		return
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{
		"restarts": row.Restarts + 1, "needs_preamble": s.hasDialogue(row.ID), "sandbox_id": 0,
	})
	row.Restarts++
	if err := s.openSandboxWith(ctx, &row, s.restoreFromBackup(backup)); err != nil {
		s.failResume(row.ID, err)
		return
	}
	if backupID != 0 {
		s.appendSystem(row.ID, "正在新沙箱中从所选备份恢复工作区；依赖目录（如 node_modules）需重新安装")
	} else {
		s.appendSystem(row.ID, "原沙箱已不存在，正在新沙箱中从备份恢复工作区；依赖目录（如 node_modules）需重新安装")
	}
	s.publishStatus(row.ID)
}

func (s *SoloService) failResume(id string, err error) {
	log.Warn().Err(err).Str("session", id).Msg("solo resume failed; session kept stopped")
	msg := "恢复失败，工作区备份仍保留，可重试：" + truncErr(err)
	s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", id, models.SoloStatusStarting).
		Updates(map[string]any{"status": models.SoloStatusStopped, "error": msg})
	s.publishStatus(id)
}

func (s *SoloService) hasDialogue(id string) bool {
	var n int64
	s.db.Model(&models.SoloMessage{}).Where("session_id = ? AND role <> ?", id, "system").Count(&n)
	return n > 0
}

// restoreFromBackup extracts the archive and then checks it is the one the
// stop recorded. Gzip already rejects corrupted bytes during extraction.
func (s *SoloService) restoreFromBackup(b models.SoloBackup) func(context.Context, *sandbox.Sandbox) error {
	key, want := b.ObjectKey, strings.ToLower(b.SHA256)
	return func(ctx context.Context, sb *sandbox.Sandbox) error {
		rc, err := s.backups.Open(ctx, key)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		h := sha256.New()
		if err := sb.RestoreWorkspace(ctx, io.TeeReader(rc, h)); err != nil {
			return err
		}
		if _, err := io.Copy(h, rc); err != nil {
			return err
		}
		if want != "" && hex.EncodeToString(h.Sum(nil)) != want {
			return errors.New("workspace archive checksum mismatch")
		}
		return nil
	}
}
