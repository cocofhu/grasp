package services

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// PM publication follows the same explicit boundary as Solo. Preparing all
// bodies before the execution transaction keeps control/SQL locks off S3 I/O.
func (s *PmService) appendPreparedMessage(ctx context.Context, message models.ChatMessage) error {
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return err
	}
	images, err := json.Marshal(message.Images)
	if err != nil {
		return err
	}
	citations, err := json.Marshal(message.Citations)
	if err != nil {
		return err
	}
	values := map[string]any{"id": message.ID, "thread_id": message.ThreadID, "role": message.Role, "status": message.Status, "fail_kind": message.FailKind, "source": message.Source, "created_at": message.CreatedAt}
	var prepared []filestore.Prepared
	for _, field := range []struct {
		name, encoding string
		body           []byte
	}{{"content", filestore.Text, []byte(message.Content)}, {"images", filestore.JSON, images}, {"citations", filestore.JSON, citations}} {
		body, err := files.Prepare(ctx, bytes.NewReader(field.body), field.encoding)
		if err != nil {
			return err
		}
		prepared = append(prepared, body)
		values[field.name] = body.Ref()
	}
	for _, field := range []struct {
		name  string
		value any
	}{{"attached_context", message.AttachedContext}, {"usage", message.Usage}, {"usage_by_model", message.UsageByModel}} {
		raw, err := messageJSON(field.value)
		if err != nil {
			return err
		}
		values[field.name] = raw
	}
	return s.executionTransaction(ctx, message.ThreadID, func(tx *gorm.DB) error {
		for _, body := range prepared {
			if _, err := files.PreparedRef(body); err != nil {
				return err
			}
		}
		if err := tx.Table("chat_messages").Create(&values).Error; err != nil {
			return err
		}
		return tx.Model(&models.ChatThread{}).Where("id = ?", message.ThreadID).Update("updated_at", time.Now()).Error
	})
}
