package services

import (
	"context"
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// workflowChat resolves only a living, caller-owned chat. A Run reference never
// grants access to another user's originating Solo, even to an administrator.
func workflowChat(db *gorm.DB, runID, user string) (string, error) {
	var run models.Run
	if err := db.Select("id", "origin_solo_id").First(&run, "id = ?", runID).Error; err != nil {
		return "", err
	}
	var session models.SoloSession
	err := db.Select("id").Where("id = ? AND user_id = ?", run.OriginSoloID, user).First(&session).Error
	if err == nil {
		return session.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	err = db.Table("solo_sessions AS sessions").Select("sessions.id").
		Joins("JOIN solo_workflow_links AS links ON links.solo_id = sessions.id").
		Where("links.run_id = ? AND links.user_id = ? AND sessions.user_id = ? AND links.kind = ?", runID, user, user, "reference").
		Order("links.created_at DESC, links.id DESC").Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return session.ID, err
}

// WorkflowChat is observational: no chat, sandbox or model is created/woken.
func (s *SoloService) WorkflowChat(runID, user string) (string, error) {
	if user == "" {
		return "", soloErr(http.StatusUnauthorized, "unauthorized", "请先登录")
	}
	return workflowChat(s.db, runID, user)
}

func (s *SoloService) ContinueWorkflow(ctx context.Context, runID, user string, in SoloCreateInput) (*SoloSessionView, error) {
	if in.ClientRequestID != "" {
		if err := validateSoloCreationID(in.ClientRequestID); err != nil {
			return nil, err
		}
		fingerprint, err := soloCreationFingerprint(in, runID)
		if err != nil {
			return nil, err
		}
		if previous, found, err := s.findSoloCreation(ctx, user, in.ClientRequestID, fingerprint); err != nil || found {
			return previous, err
		}
	}
	previous, err := s.WorkflowChat(runID, user)
	if err != nil {
		return nil, err
	}
	if previous != "" {
		if in.ClientRequestID != "" {
			return s.recordExistingSoloCreation(ctx, user, in, runID, previous)
		}
		return s.Get(ctx, previous, user)
	}
	var run models.Run
	if err := s.db.Select("id", "status").First(&run, "id = ?", runID).Error; err != nil {
		return nil, err
	}
	if !workflowHistoryTerminal(run.Status) {
		return nil, soloErr(http.StatusConflict, "workflow_still_running", "工作流仍在执行，请先在任务卡中处理")
	}
	return s.create(ctx, user, in, runID)
}

func workflowHistoryTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}
