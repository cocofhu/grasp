package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

const maxInstructionTextBytes = 16 << 10

// instructionRecord is the same SQL schema without file serializers. The
// public model keeps its legacy tag for older readers and the payload catalogue.
type instructionRecord struct {
	ID, Generation, ClientMessageID, RunID, NodeID string
	MailboxID                                      uint
	Iteration, Attempt                             int
	Sequence                                       uint64
	ActorID, Text, State, Error                    string
	CreatedAt, UpdatedAt                           time.Time
}

func (instructionRecord) TableName() string { return "workflow_instructions" }

func (r instructionRecord) message(text string) models.WorkflowInstruction {
	return models.WorkflowInstruction{ID: r.ID, MailboxID: r.MailboxID, Generation: r.Generation,
		ClientMessageID: r.ClientMessageID, RunID: r.RunID, NodeID: r.NodeID, Iteration: r.Iteration,
		Attempt: r.Attempt, Sequence: r.Sequence, ActorID: r.ActorID, Text: text, State: r.State,
		Error: r.Error, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

type preparedInstructionText struct {
	body filestore.Prepared
}

func instructionOutsideTransaction(db *gorm.DB) error {
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return errors.New("instruction payload I/O requires an outer transaction-free storage handle")
	}
	return nil
}

func prepareInstructionText(ctx context.Context, db *gorm.DB, text string) (preparedInstructionText, error) {
	if err := instructionOutsideTransaction(db); err != nil {
		return preparedInstructionText{}, err
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return preparedInstructionText{}, err
	}
	body, err := files.Prepare(ctx, strings.NewReader(text), filestore.Text)
	return preparedInstructionText{body: body}, err
}

// Matching an accepted receipt needs no object I/O. Its canonical text reference
// contains the exact original bytes' SHA; legacy inline receipts compare bytes.
func instructionTextMatches(raw, text string) (bool, error) {
	if !filestore.IsRef(raw) {
		if strings.HasPrefix(raw, filestore.Prefix) {
			return false, errors.New("invalid stored instruction reference")
		}
		return raw == text, nil
	}
	encoding, digest, err := filestore.Parse(raw)
	if err != nil {
		return false, err
	}
	if encoding != filestore.Text {
		return false, errors.New("stored instruction encoding is not text")
	}
	hash := sha256.Sum256([]byte(text))
	return digest == hex.EncodeToString(hash[:]), nil
}

func instructionReceipt(row instructionRecord, actor string, input InstructionInput) (*models.WorkflowInstruction, error) {
	match, err := instructionTextMatches(row.Text, input.Text)
	if err != nil {
		return nil, err
	}
	if row.ActorID != actor || !match || row.Iteration != input.Iteration {
		return nil, instructionError("instruction_idempotency_conflict", "该消息身份已用于其他补充要求")
	}
	message := row.message(input.Text)
	return &message, nil
}

func loadInstructionText(ctx context.Context, db *gorm.DB, raw string) (string, error) {
	if err := instructionOutsideTransaction(db); err != nil {
		return "", err
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return "", err
	}
	body, err := files.LoadBounded(ctx, raw, filestore.Text, maxInstructionTextBytes)
	if err != nil {
		return "", err
	}
	if len(body) == 0 || !utf8.Valid(body) {
		return "", errors.New("stored instruction text is empty or invalid UTF-8")
	}
	return string(body), nil
}

func queuedInstruction(db *gorm.DB, mailboxID uint, generation string) (*instructionRecord, error) {
	var row instructionRecord
	err := db.Where("mailbox_id = ? AND generation = ? AND state = ?", mailboxID, generation, "queued").
		Order("sequence,id").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
