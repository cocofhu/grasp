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
func (s *SoloService) SetBackupStore(store solobackup.Store) {
	s.backups = store
	if s.sbx != nil {
		s.sbx.SetBackupStore(store)
	}
}

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
	if s.sbx == nil || row.SandboxID == 0 {
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
	if err := s.sbx.Reclaim(ctx, row.SandboxID); err != nil {
		status := models.SoloStatusStopping
		if sb, getErr := s.sbx.Get(row.SandboxID); getErr == nil {
			if sb.Status == "stopped" && sb.RecoveryPhase == "" {
				status = models.SoloStatusStopped
			}
			if sb.Status == "running" {
				status = models.SoloStatusRunning
			}
		}
		msg := "停止未完成，可重试：" + truncErr(err)
		s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", row.ID, models.SoloStatusStopping).
			Updates(map[string]any{"status": status, "error": msg})
		s.appendSystem(row.ID, msg)
		s.publishStatus(row.ID)
		return
	}
	s.db.Model(&models.SoloSession{}).Where("id = ? AND status IN ?", row.ID, []string{models.SoloStatusRunning, models.SoloStatusStopping, models.SoloStatusStopped}).Updates(map[string]any{"status": models.SoloStatusStopped, "error": ""})
	s.appendSystem(row.ID, "环境已停止，恢复点已保存")
	s.publishStatus(row.ID)
}

// discardBackup cleans migrated file archives only. Current recovery points
// are owned by SandboxService and follow its durable cancellation queue.
func (s *SoloService) discardBackup(ctx context.Context, row *models.SoloSession) error {
	return discardLegacyBackups(ctx, s.db, s.backups, row)
}

// Resume always restores the newest successful recovery point.
func (s *SoloService) Resume(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status != models.SoloStatusStopped {
		return nil, soloErr(http.StatusConflict, "session_not_stopped", "会话未停止，无需恢复")
	}
	if s.sbx == nil || s.agents == nil {
		return nil, errors.New("solo unavailable")
	}
	if _, ok := s.agents.Get(row.AgentName); !ok {
		return nil, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 已不存在，无法恢复", row.AgentName)
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
	go s.finishResume(*row)
	return s.Get(ctx, id, user)
}

// Wake resumes the same resident of a running session after the provider
// paused it. The recovery lock in EnsureSoloRunning serializes it with a turn
// that wakes the sandbox on its own.
func (s *SoloService) Wake(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status != models.SoloStatusRunning || row.SandboxID == 0 {
		return nil, soloErr(http.StatusConflict, "session_not_running", "会话未在运行，无法恢复环境")
	}
	if s.sbx == nil {
		return nil, errors.New("solo unavailable")
	}
	if err := s.sbx.EnsureSoloRunning(ctx, row.SandboxID); err != nil {
		return nil, soloErr(http.StatusBadGateway, "wake_failed", "%s", truncErr(err))
	}
	return s.Get(ctx, id, user)
}

func (s *SoloService) finishResume(row models.SoloSession) {
	ctx := context.Background()
	if row.SandboxID != 0 {
		warm, err := s.sbx.ResumeSolo(ctx, row.SandboxID)
		if err == nil {
			updates := map[string]any{"status": models.SoloStatusRunning, "error": ""}
			if !warm && s.hasDialogue(row.ID) {
				updates["resume_context_pending"] = true
			}
			s.db.Model(&models.SoloSession{}).Where("id = ? AND status IN ?", row.ID, []string{models.SoloStatusStarting, models.SoloStatusRunning}).Updates(updates)
			s.appendSystem(row.ID, "环境已从最近一次恢复点恢复")
			s.publishStatus(row.ID)
			return
		}
		if !errors.Is(err, ErrSoloSandboxGone) {
			s.failResume(row.ID, err)
			return
		}
	}
	// One-time compatibility for archives saved by the old release after its
	// sandbox row disappeared. New releases keep the stable row/spec.
	backup, err := s.legacyRestorePoint(row.ID)
	if err != nil || s.backups == nil {
		s.failResume(row.ID, errors.New("沙箱已不存在，且没有可用的恢复点"))
		return
	}
	if backup.Store != s.backups.Kind() {
		s.failResume(row.ID, fmt.Errorf("工作区备份存储 %s 未配置", backup.Store))
		return
	}
	s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{"restarts": row.Restarts + 1, "needs_preamble": s.hasDialogue(row.ID), "sandbox_id": 0})
	row.Restarts++
	if err := s.openSandboxWith(ctx, &row, s.restoreFromBackup(backup)); err != nil {
		s.failResume(row.ID, err)
		return
	}
	s.appendSystem(row.ID, "正在恢复最近一次工作区备份；依赖目录需重新安装")
	s.publishStatus(row.ID)
}

func (s *SoloService) failResume(id string, err error) {
	log.Warn().Err(err).Str("session", id).Msg("solo resume failed; session kept stopped")
	msg := "恢复未完成，可重试：" + truncErr(err)
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
