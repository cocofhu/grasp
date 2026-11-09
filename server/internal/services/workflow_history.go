package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
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
	if err := s.db.First(&run, "id = ?", runID).Error; err != nil {
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

// The history is a frozen, Run-authorized context, never a copy of private
// origin-Solo messages. It excludes credentials and Run MCP tokens.
func freezeWorkflowHistory(tx *gorm.DB, session *models.SoloSession, runID string) error {
	var run models.Run
	if err := tx.First(&run, "id = ?", runID).Error; err != nil {
		return err
	}
	if !workflowHistoryTerminal(run.Status) {
		return soloErr(http.StatusConflict, "workflow_still_running", "工作流状态已变化，请刷新后重试")
	}
	segments, _, err := NewRunService(tx).ConversationChain(run, "", 0)
	if err != nil {
		return err
	}
	var artifacts []models.Artifact
	if err := tx.Where("run_id = ?", runID).Order("created_at asc, id asc").Find(&artifacts).Error; err != nil {
		return err
	}
	results := make([]WorkflowArtifactRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		results = append(results, WorkflowArtifactRef{ID: artifact.ID, Name: artifact.Name, Version: artifact.Revision, Content: artifact.Content})
	}
	// Artifacts have no execution credentials; their content is needed for
	// continuation even after the original sandbox has been released.
	// Keep the original request ahead of potentially large artifact bodies.
	// The model receives a bounded excerpt and can retrieve the full history.
	// A map's alphabetical JSON order would put artifacts before the request.
	var instructions []models.WorkflowInstruction
	if err := tx.Where("run_id = ?", runID).Order("created_at,id").Find(&instructions).Error; err != nil {
		return err
	}
	context, err := json.Marshal(struct {
		RunID        string                       `json:"runId"`
		Workflow     string                       `json:"workflow"`
		Title        string                       `json:"title"`
		Status       string                       `json:"status"`
		FirstMessage *models.CompositeText        `json:"firstMessage"`
		Inputs       map[string]any               `json:"inputs"`
		Instructions []models.WorkflowInstruction `json:"instructions,omitempty"`
		Segments     []ChainSegment               `json:"segments"`
		Artifacts    []WorkflowArtifactRef        `json:"artifacts"`
	}{run.ID, run.WorkflowName, run.Title, run.Status, run.FirstMessage, run.Inputs, instructions, segments, results})
	if err != nil {
		return err
	}
	if err := tx.Create(&models.SoloWorkflowLink{ID: uuid.NewString(), SoloID: session.ID, RunID: runID, UserID: session.UserID, Kind: "reference"}).Error; err != nil {
		return err
	}
	message := models.SoloMessage{ID: uuid.NewString(), SessionID: session.ID, Role: "system", WorkflowRunID: runID,
		Content: "已关联历史工作流。当前是独立新环境，尚未恢复原工作区文件或进程。", WorkflowContext: string(context)}
	if err := tx.Create(&message).Error; err != nil {
		return err
	}
	session.ModelContextPending = true
	return tx.Model(session).Update("model_context_pending", true).Error
}
