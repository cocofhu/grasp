package services

import (
	"context"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *PmService) upsertPreparedDraft(threadID, userMsgID, partialText, status string, chunkIndex, eventSeq int, sandboxID uint) (models.ChatTurnDraft, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.executionCheck(ctx, threadID); err != nil {
		return models.ChatTurnDraft{}, err
	}
	existing, err := s.GetDraft(threadID)
	if err != nil {
		return models.ChatTurnDraft{}, err
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return models.ChatTurnDraft{}, err
	}
	body, err := files.Prepare(ctx, strings.NewReader(partialText), filestore.Text)
	if err != nil {
		return models.ChatTurnDraft{}, err
	}
	now := time.Now()
	row := models.ChatTurnDraft{ID: "draft-" + uuid.NewString()[:12], ThreadID: threadID, CreatedAt: now}
	if existing != nil {
		row = *existing
	}
	row.UserMsgID, row.PartialText, row.Status, row.FailKind = userMsgID, partialText, status, ""
	row.ChunkIndex, row.EventSeq, row.SandboxID, row.UpdatedAt = chunkIndex, eventSeq, sandboxID, now
	err = s.executionTransaction(ctx, threadID, func(tx *gorm.DB) error {
		ref, err := files.PreparedRef(body)
		if err != nil {
			return err
		}
		values := map[string]any{"user_msg_id": userMsgID, "partial_text": ref, "chunk_index": chunkIndex, "event_seq": eventSeq, "status": status, "sandbox_id": sandboxID, "updated_at": now, "fail_kind": ""}
		if existing != nil {
			return tx.Table("chat_turn_drafts").Where("id = ?", row.ID).Updates(values).Error
		}
		values["id"], values["thread_id"], values["created_at"] = row.ID, threadID, row.CreatedAt
		return tx.Table("chat_turn_drafts").Create(values).Error
	})
	return row, err
}
