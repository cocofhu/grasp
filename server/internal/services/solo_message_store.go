package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// soloMessageRecord is the SQL representation. No field invokes a file
// serializer: callers finish SQL/cursor work before loading referenced bodies.
// The public model retains its legacy tags for existing readers and backups.
type soloMessageRecord struct {
	ID, SessionID, Role, ClientMessageID, TurnID, WorkflowRunID string
	Content, WorkflowContext                                    string
	Images, Annotations, Events                                 *string
	Files, EventLog, Usage, UsageByModel, Execution             *string
	MessageReferences                                           *string `gorm:"column:message_references"`
	PreviewShared                                               bool
	Status, Error                                               string
	DurationMs                                                  int64
	CreatedAt                                                   time.Time
}

func (soloMessageRecord) TableName() string { return "solo_messages" }

type soloMessageSQL = soloMessageRecord

type soloMessageFields uint8

const (
	soloMessageContent soloMessageFields = 1 << iota
	soloMessageImages
	soloMessageAnnotations
	soloMessageEvents
	soloMessageWorkflowContext
	soloMessageChat = soloMessageContent | soloMessageImages | soloMessageAnnotations
	soloMessageAll  = soloMessageChat | soloMessageEvents | soloMessageWorkflowContext
)

type preparedSoloMessage struct {
	record soloMessageSQL
	bodies []filestore.Prepared
}

func messageOutsideTransaction(db *gorm.DB) error {
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return errors.New("message payload I/O requires an outer transaction-free storage handle")
	}
	return nil
}

func messageJSON(value any) (*string, error) {
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return nil, err
	}
	raw := string(data)
	return &raw, nil
}

func prepareSoloMessage(ctx context.Context, db *gorm.DB, message models.SoloMessage) (preparedSoloMessage, error) {
	var prepared preparedSoloMessage
	if err := messageOutsideTransaction(db); err != nil {
		return prepared, err
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return prepared, err
	}
	prepared.record = soloMessageRecord{ID: message.ID, SessionID: message.SessionID, Role: message.Role,
		ClientMessageID: message.ClientMessageID, TurnID: message.TurnID, WorkflowRunID: message.WorkflowRunID,
		PreviewShared: message.PreviewShared, Status: message.Status, Error: message.Error, DurationMs: message.DurationMs, CreatedAt: message.CreatedAt}
	if message.ID == "" || message.SessionID == "" {
		return preparedSoloMessage{}, errors.New("message identity and conversation are required")
	}
	if prepared.record.CreatedAt.IsZero() {
		prepared.record.CreatedAt = time.Now().UTC()
	}
	metadata := []struct {
		value any
		raw   **string
	}{{message.Files, &prepared.record.Files}, {message.EventLog, &prepared.record.EventLog},
		{message.References, &prepared.record.MessageReferences}, {message.Usage, &prepared.record.Usage},
		{message.UsageByModel, &prepared.record.UsageByModel}, {message.Execution, &prepared.record.Execution}}
	for _, field := range metadata {
		*field.raw, err = messageJSON(field.value)
		if err != nil {
			return preparedSoloMessage{}, err
		}
	}
	put := func(data []byte, encoding string) (string, error) {
		body, err := files.Prepare(ctx, bytes.NewReader(data), encoding)
		if err == nil {
			prepared.bodies = append(prepared.bodies, body)
		}
		return body.Ref(), err
	}
	for _, field := range []struct {
		value string
		raw   *string
	}{{message.Content, &prepared.record.Content}, {message.WorkflowContext, &prepared.record.WorkflowContext}} {
		if field.value != "" {
			*field.raw, err = put([]byte(field.value), filestore.Text)
			if err != nil {
				return preparedSoloMessage{}, err
			}
		}
	}
	for _, field := range []struct {
		value any
		raw   **string
	}{{message.Images, &prepared.record.Images}, {message.Annotations, &prepared.record.Annotations}, {message.Events, &prepared.record.Events}} {
		data, err := json.Marshal(field.value)
		if err != nil {
			return preparedSoloMessage{}, err
		}
		if string(data) != "null" {
			ref, err := put(data, filestore.JSON)
			if err != nil {
				return preparedSoloMessage{}, err
			}
			*field.raw = &ref
		}
	}
	return prepared, nil
}

// insertPreparedSoloMessageTx never performs payload I/O. Conflict columns
// belong to the producer's existing policy (final reply vs interruption notice).
func insertPreparedSoloMessageTx(tx *gorm.DB, prepared preparedSoloMessage, updates ...string) (bool, error) {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside || prepared.record.ID == "" || prepared.record.SessionID == "" {
		return false, errors.New("prepared message requires an SQL publication transaction")
	}
	files, err := filestore.FromDB(tx)
	if err != nil {
		return false, err
	}
	for _, body := range prepared.bodies {
		if _, err := files.PreparedRef(body); err != nil {
			return false, err
		}
	}
	refs := map[string]bool{}
	for _, body := range prepared.bodies {
		refs[body.Ref()] = true
	}
	for _, ref := range []string{prepared.record.Content, prepared.record.WorkflowContext} {
		if ref != "" && !refs[ref] {
			return false, errors.New("message body was changed after preparation")
		}
	}
	for _, ref := range []*string{prepared.record.Images, prepared.record.Annotations, prepared.record.Events} {
		if ref != nil && *ref != "" && !refs[*ref] {
			return false, errors.New("structured message body was changed after preparation")
		}
	}
	conflict := clause.OnConflict{DoNothing: true}
	if len(updates) > 0 {
		conflict = clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns(updates)}
	}
	result := tx.Clauses(conflict).Create(&prepared.record)
	return result.RowsAffected > 0, result.Error
}

func readSoloMessages(query *gorm.DB, fields soloMessageFields) ([]models.SoloMessage, error) {
	if err := messageOutsideTransaction(query); err != nil {
		return nil, err
	}
	omit := []string{}
	for _, field := range []struct {
		bit  soloMessageFields
		name string
	}{{soloMessageContent, "content"}, {soloMessageImages, "images"}, {soloMessageAnnotations, "annotations"},
		{soloMessageEvents, "events"}, {soloMessageWorkflowContext, "workflow_context"}} {
		if fields&field.bit == 0 {
			omit = append(omit, field.name)
		}
	}
	var records []soloMessageSQL
	if err := query.Model(&soloMessageSQL{}).Omit(omit...).Find(&records).Error; err != nil {
		return nil, err
	}
	// Find has returned and closed the SQL cursor before any Load begins.
	result := make([]models.SoloMessage, 0, len(records))
	for _, record := range records {
		message, err := hydrateSoloMessage(query.Statement.Context, query, record, fields)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, nil
}

func readSoloMessage(query *gorm.DB, fields soloMessageFields) (models.SoloMessage, error) {
	rows, err := readSoloMessages(query.Limit(1), fields)
	if err != nil {
		return models.SoloMessage{}, err
	}
	if len(rows) == 0 {
		return models.SoloMessage{}, gorm.ErrRecordNotFound
	}
	return rows[0], nil
}

func hydrateSoloMessage(ctx context.Context, db *gorm.DB, record soloMessageSQL, fields soloMessageFields) (models.SoloMessage, error) {
	message := models.SoloMessage{ID: record.ID, SessionID: record.SessionID, Role: record.Role, ClientMessageID: record.ClientMessageID,
		TurnID: record.TurnID, WorkflowRunID: record.WorkflowRunID, PreviewShared: record.PreviewShared,
		Status: record.Status, Error: record.Error, DurationMs: record.DurationMs, CreatedAt: record.CreatedAt}
	for _, field := range []struct {
		raw   *string
		value any
	}{{record.Files, &message.Files}, {record.EventLog, &message.EventLog}, {record.MessageReferences, &message.References},
		{record.Usage, &message.Usage}, {record.UsageByModel, &message.UsageByModel}, {record.Execution, &message.Execution}} {
		if field.raw != nil && *field.raw != "" {
			if err := json.Unmarshal([]byte(*field.raw), field.value); err != nil {
				return models.SoloMessage{}, err
			}
		}
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return models.SoloMessage{}, err
	}
	for _, field := range []struct {
		bit   soloMessageFields
		raw   string
		value *string
	}{{soloMessageContent, record.Content, &message.Content}, {soloMessageWorkflowContext, record.WorkflowContext, &message.WorkflowContext}} {
		if fields&field.bit != 0 && field.raw != "" {
			body, err := files.Load(ctx, field.raw, filestore.Text)
			if err != nil {
				return models.SoloMessage{}, fmt.Errorf("message %s body: %w", record.ID, err)
			}
			*field.value = string(body)
		}
	}
	for _, field := range []struct {
		bit   soloMessageFields
		raw   *string
		value any
	}{{soloMessageImages, record.Images, &message.Images}, {soloMessageAnnotations, record.Annotations, &message.Annotations}, {soloMessageEvents, record.Events, &message.Events}} {
		if fields&field.bit != 0 && field.raw != nil && *field.raw != "" {
			body, err := files.Load(ctx, *field.raw, filestore.JSON)
			if err != nil {
				return models.SoloMessage{}, fmt.Errorf("message %s structured body: %w", record.ID, err)
			}
			if err := json.Unmarshal(body, field.value); err != nil {
				return models.SoloMessage{}, err
			}
		}
	}
	return message, nil
}
