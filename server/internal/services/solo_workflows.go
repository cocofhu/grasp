package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
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
	RunID        string                       `json:"runId"`
	WorkflowName string                       `json:"workflowName"`
	Title        string                       `json:"title"`
	Status       string                       `json:"status"`
	Instructions []models.WorkflowInstruction `json:"instructions,omitempty"`
	Artifacts    []WorkflowArtifactRef        `json:"artifacts"`
}

// Terminal status, mailbox closure, delivery receipts and the frozen result
// share the caller's transaction. A crash cannot leave an ended Run accepting
// instructions, or a frozen "dispatching" receipt pretending work will resume.
func sealRunInstructions(tx *gorm.DB, runID string) error {
	var boxes []models.WorkflowInstructionMailbox
	if err := tx.Select("id").Where("run_id = ?", runID).Order("id").Find(&boxes).Error; err != nil {
		return err
	}
	for _, box := range boxes {
		if err := tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", box.ID).Update("state", "sealed").Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&models.WorkflowInstruction{}).Where("run_id = ? AND state = ?", runID, "queued").
		Updates(map[string]any{"state": "expired", "error": "工作流已结束，此要求未派发，不会转移到其他节点"}).Error; err != nil {
		return err
	}
	return tx.Model(&models.WorkflowInstruction{}).Where("run_id = ? AND state = ?", runID, "dispatching").
		Updates(map[string]any{"state": "unknown", "error": "工作流已结束，处理结果未确认，请核对后决定是否重试"}).Error
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
	if err := messageOutsideTransaction(s.db); err != nil {
		return err
	}
	var events []workflowEventRecord
	query := s.db.WithContext(ctx).Where("disposition = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", "pending", time.Now().UTC())
	if s.execution != nil && !s.execution.LocalOnly() {
		if err := s.execution.Check(ctx); err != nil {
			return err
		}
		query = query.Where("NOT EXISTS (SELECT 1 FROM execution_quarantines q WHERE q.review_state = 'pending' AND ((q.entity_kind = 'workflow_event' AND q.entity_id = workflow_conversation_events.id) OR (q.entity_kind = 'run' AND q.entity_id = workflow_conversation_events.run_id) OR (q.entity_kind = 'solo' AND q.entity_id = workflow_conversation_events.solo_id)))")
	}
	if err := query.
		Order("created_at asc, run_id asc, version asc").Limit(100).Find(&events).Error; err != nil {
		return err
	}
	var metadataFailures []error
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(metadataFailures, err)...)
		}
		stage := "recipient_lookup"
		exists, err := s.workflowEventRecipient(ctx, event)
		if err == nil && !exists {
			continue // Orphans never materialize objects, even missing ones.
		}
		var delivered bool
		if err == nil {
			stage, delivered, err = s.deliverWorkflowEvent(ctx, event)
		}
		if err != nil {
			if ctx.Err() != nil {
				return errors.Join(append(metadataFailures, ctx.Err())...)
			}
			// A single bad immutable result is retried independently. Its raw
			// object/error contents never become another user's request error.
			if persistErr := s.deferWorkflowEvent(ctx, event, stage); persistErr != nil {
				metadataFailures = append(metadataFailures, persistErr)
			}
			continue
		}
		if delivered {
			s.publishStatus(event.SoloID)
			if chat := s.lookup(event.SoloID); chat != nil {
				chat.publish("history_changed", map[string]any{"eventId": event.ID})
			}
		}
	}
	return errors.Join(metadataFailures...)
}

func (s *SoloService) deliverWorkflowEvent(ctx context.Context, event workflowEventRecord) (string, bool, error) {
	if err := s.executionCheck(ctx, event.SoloID, workflowExecutionEntities(event)...); err != nil {
		return "execution_authority", false, err
	}
	event, body, err := preparePendingWorkflowEvent(ctx, s.db, event, s.execution)
	if err != nil {
		return "prepare_payload", false, err
	}
	var payload WorkflowEventPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "decode_payload", false, err
	}
	message, err := prepareSoloMessage(ctx, s.db, models.SoloMessage{ID: "workflow-event-" + event.ID, SessionID: event.SoloID, Role: "system",
		Content: workflowEventText(payload), WorkflowRunID: event.RunID, WorkflowContext: string(body)})
	if err != nil {
		return "prepare_message", false, err
	}
	delivered := false
	err = s.executionTransaction(ctx, event.SoloID, func(tx *gorm.DB) error {
		var session models.SoloSession
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ? AND user_id = ?", event.SoloID, event.UserID).First(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Model(&workflowEventRecord{}).Where("id = ? AND disposition = ?", event.ID, "pending").
				Updates(map[string]any{"disposition": "orphaned", "next_attempt_at": nil, "last_error": ""}).Error
		}
		if err != nil {
			return err
		}
		var current workflowEventRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", event.ID).Error; err != nil {
			return err
		}
		if current.Disposition != "pending" {
			return nil
		}
		if current.RunID != event.RunID || current.Version != event.Version || current.SoloID != event.SoloID || current.UserID != event.UserID || current.Kind != event.Kind || current.Payload != event.Payload || current.SourceVersion != event.SourceVersion || current.SourceCount != event.SourceCount || current.SourceFingerprint != event.SourceFingerprint {
			return errors.New("workflow event changed before message publication")
		}
		created, err := insertPreparedSoloMessageTx(tx, message)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if created {
			if err := tx.Model(&models.SoloSession{}).Where("id = ?", event.SoloID).Updates(map[string]any{"updated_at": now, "conversation_context_pending": true, "workflow_context_version": gorm.Expr("workflow_context_version + 1")}).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&workflowEventRecord{}).Where("id = ? AND disposition = ?", event.ID, "pending").
			Updates(map[string]any{"disposition": "delivered", "delivered_at": now, "next_attempt_at": nil, "last_error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("workflow delivery lost its pending receipt")
		}
		delivered = true
		return nil
	}, workflowExecutionEntities(event)...)
	return "publish_message", delivered && err == nil, err
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
