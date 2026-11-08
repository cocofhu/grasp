package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cocofhu/grasp/internal/chatsession"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkflowArtifactRef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	Content string `json:"content,omitempty"`
}
type WorkflowEventPayload struct {
	RunID        string                `json:"runId"`
	WorkflowName string                `json:"workflowName"`
	Title        string                `json:"title"`
	Status       string                `json:"status"`
	Artifacts    []WorkflowArtifactRef `json:"artifacts"`
}

// EnqueueWorkflowEvent must be called inside the transaction that changes Run.
func EnqueueWorkflowEvent(tx *gorm.DB, runID, status string) error {
	var run models.Run
	if err := tx.Select("id", "origin_solo_id", "created_by", "workflow_name", "title").First(&run, "id = ?", runID).Error; err != nil {
		return err
	}
	if run.OriginSoloID == "" {
		return nil
	}
	if err := tx.Model(&models.Run{}).Where("id = ?", runID).Update("event_version", gorm.Expr("COALESCE(event_version, 0) + 1")).Error; err != nil {
		return err
	}
	if err := tx.Select("event_version").First(&run, "id = ?", runID).Error; err != nil {
		return err
	}
	payload := WorkflowEventPayload{RunID: runID, WorkflowName: run.WorkflowName, Title: run.Title, Status: status, Artifacts: []WorkflowArtifactRef{}}
	var artifacts []models.Artifact
	if err := tx.Where("run_id = ?", runID).Order("id asc").Find(&artifacts).Error; err != nil {
		return err
	}
	for _, artifact := range artifacts {
		payload.Artifacts = append(payload.Artifacts, WorkflowArtifactRef{ID: artifact.ID, Name: artifact.Name, Version: artifact.Revision, Content: artifact.Content})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.Create(&models.WorkflowConversationEvent{ID: uuid.NewString(), RunID: runID, Version: run.EventVersion, SoloID: run.OriginSoloID, UserID: run.CreatedBy, Kind: status, Payload: string(data), Disposition: "pending"}).Error
}

func workflowEventText(payload WorkflowEventPayload) string {
	name := strings.TrimSpace(payload.Title)
	if name == "" {
		name = payload.WorkflowName
	}
	labels := map[string]string{"queued": "已启动", "waiting_human": "需要处理", "completed": "已完成", "failed": "执行失败", "cancelled": "已取消", "paused": "已暂停"}
	state := labels[payload.Status]
	if state == "" {
		state = payload.Status
	}
	var text strings.Builder
	fmt.Fprintf(&text, "工作流 %s · %s · %s", payload.RunID, name, state)
	for _, artifact := range payload.Artifacts {
		fmt.Fprintf(&text, "\n产物：%s（版本 %d）", artifact.Name, artifact.Version)
	}
	return text.String()
}

func (s *SoloService) DeliverWorkflowEvents(ctx context.Context) error {
	var events []models.WorkflowConversationEvent
	if err := s.db.WithContext(ctx).Where("disposition = ?", "pending").Order("created_at asc, run_id asc, version asc").Limit(100).Find(&events).Error; err != nil {
		return err
	}
	for _, event := range events {
		var payload WorkflowEventPayload
		if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
			return err
		}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var session models.SoloSession
			err := tx.Select("id").Where("id = ? AND user_id = ?", event.SoloID, event.UserID).First(&session).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return tx.Model(&models.WorkflowConversationEvent{}).Where("id = ? AND disposition = ?", event.ID, "pending").Update("disposition", "orphaned").Error
			}
			if err != nil {
				return err
			}
			message := models.SoloMessage{ID: "workflow-event-" + event.ID, SessionID: event.SoloID, Role: "system", Content: workflowEventText(payload), WorkflowRunID: event.RunID, WorkflowContext: event.Payload}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&message)
			if created.Error != nil {
				return created.Error
			}
			now := time.Now().UTC()
			if created.RowsAffected > 0 {
				if err := tx.Model(&models.SoloSession{}).Where("id = ?", event.SoloID).Updates(map[string]any{"updated_at": now, "model_context_pending": true, "workflow_context_version": gorm.Expr("workflow_context_version + 1")}).Error; err != nil {
					return err
				}
			}
			return tx.Model(&models.WorkflowConversationEvent{}).Where("id = ? AND disposition = ?", event.ID, "pending").Updates(map[string]any{"disposition": "delivered", "delivered_at": now}).Error
		})
		if err != nil {
			return err
		}
		s.publishStatus(event.SoloID)
		if chat := s.lookup(event.SoloID); chat != nil {
			chat.stream.Session("history_changed", map[string]any{"eventId": event.ID}, chatsession.KeepAppend)
		}
	}
	return nil
}

func (s *SoloService) RunWorkflowEventDelivery(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.DeliverWorkflowEvents(ctx); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("workflow result delivery will retry")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *SoloService) WorkflowRuns(id, user string) ([]models.Run, error) {
	if _, err := s.Require(id, user); err != nil {
		return nil, err
	}
	var runs []models.Run
	err := s.db.Where("id IN (?)", s.db.Model(&models.SoloWorkflowLink{}).Select("run_id").Where("solo_id = ? AND user_id = ?", id, user)).Order("created_at asc, id asc").Find(&runs).Error
	return runs, err
}
