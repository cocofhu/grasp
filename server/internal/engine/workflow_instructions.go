package engine

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InstructionError struct {
	Code    string
	Message string
}

func (e *InstructionError) Error() string { return e.Message }

func instructionError(code, message string) error {
	return &InstructionError{Code: code, Message: message}
}

// InstructionInput pins the visit actually displayed when the user submitted.
type InstructionInput struct {
	Iteration       int    `json:"iteration"`
	Generation      string `json:"generation"`
	ClientMessageID string `json:"clientMessageId"`
	Text            string `json:"text"`
}

type instructionQueue struct {
	db         *gorm.DB
	mailboxID  uint
	generation string
}

// The first statement acquires a write lock, including on SQLite where SELECT
// FOR UPDATE is unavailable. Incrementing a private revision also avoids MySQL
// no-op UPDATE affected-row semantics. All admission/claim/seal paths use it.
func lockInstructionMailbox(tx *gorm.DB, id uint) (*models.WorkflowInstructionMailbox, error) {
	result := tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", id).
		UpdateColumn("revision", gorm.Expr("revision + 1"))
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, instructionError("instruction_not_current", "当前执行已切换，请刷新工作流任务")
	}
	var row models.WorkflowInstructionMailbox
	if err := tx.First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (e *Engine) openInstructionQueue(ctx context.Context, stateRunID uint, runID, nodeID string, iteration int) (*instructionQueue, error) {
	if stateRunID == 0 {
		return nil, errors.New("自动节点执行记录尚未持久化")
	}
	generation := uuid.NewString()
	err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		readRun := func() error {
			var run models.Run
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&run, "id = ?", runID).Error; err != nil {
				return err
			}
			if run.Status != "running" {
				return instructionError("instruction_not_current", "工作流已停止，不能重新开放自动执行要求通道")
			}
			return nil
		}
		// MySQL locks Run before mailbox, matching terminal transitions. SQLite
		// needs its writer reservation first instead of SELECT FOR UPDATE.
		if tx.Dialector.Name() != "sqlite" {
			if err := readRun(); err != nil {
				return err
			}
		}
		// Start with a write even when the row exists, so SQLite obtains its
		// writer reservation before reading a snapshot another writer can age.
		initial := models.WorkflowInstructionMailbox{ID: stateRunID, RunID: runID, NodeID: nodeID, Iteration: iteration, Attempt: 1, Generation: generation, State: "open"}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		if tx.Dialector.Name() == "sqlite" {
			if err := readRun(); err != nil {
				return err
			}
		}
		var sequence uint64
		if err := tx.Model(&models.WorkflowInstruction{}).Where("run_id = ? AND node_id = ?", runID, nodeID).Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
			return err
		}
		box, err := lockInstructionMailbox(tx, stateRunID)
		if err != nil {
			return err
		}
		if box.RunID != runID || box.NodeID != nodeID || box.Iteration != iteration {
			return errors.New("自动节点执行记录与补充要求目标不一致")
		}
		if box.Generation != generation {
			box.Attempt++
			if err := retireInstructions(tx, stateRunID, box.Generation, "执行进程已重新连接，请核对后重新提交"); err != nil {
				return err
			}
		}
		return tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", stateRunID).
			Updates(map[string]any{"generation": generation, "state": "open", "attempt": max(1, box.Attempt), "next_sequence": max(sequence, box.NextSequence)}).Error
	})
	if err != nil {
		return nil, err
	}
	return &instructionQueue{db: e.db, mailboxID: stateRunID, generation: generation}, nil
}

func retireInstructions(tx *gorm.DB, id uint, generation, reason string) error {
	query := tx.Model(&models.WorkflowInstruction{}).Where("mailbox_id = ? AND generation = ?", id, generation)
	if err := query.Where("state = ?", "queued").Updates(map[string]any{"state": "expired", "error": reason}).Error; err != nil {
		return err
	}
	return tx.Model(&models.WorkflowInstruction{}).Where("mailbox_id = ? AND generation = ? AND state = ?", id, generation, "dispatching").
		Updates(map[string]any{"state": "unknown", "error": "处理是否完成尚未确认，请核对后决定是否重试"}).Error
}

func (q *instructionQueue) close(ctx context.Context, reason string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return q.db.WithContext(cleanup).Transaction(func(tx *gorm.DB) error {
		box, err := lockInstructionMailbox(tx, q.mailboxID)
		if err != nil {
			return err
		}
		if box.Generation != q.generation {
			return nil // an obsolete executor cannot close its successor's queue
		}
		if err := retireInstructions(tx, box.ID, q.generation, reason); err != nil {
			return err
		}
		return tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", box.ID).Update("state", "sealed").Error
	})
}

func (q *instructionQueue) Next(ctx context.Context, sealIfEmpty bool) (*runtime.AutoInstruction, error) {
	var message *runtime.AutoInstruction
	err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		box, err := lockInstructionMailbox(tx, q.mailboxID)
		if err != nil {
			return err
		}
		if box.Generation != q.generation {
			return instructionError("instruction_not_current", "执行代次已更新，停止向旧会话派发补充要求")
		}
		if box.State != "open" {
			return nil
		}
		var run models.Run
		if err := tx.Select("status").First(&run, "id = ?", box.RunID).Error; err != nil {
			return err
		}
		var visit models.StateRun
		if err := tx.Select("id", "status", "mode").Where("run_id = ? AND node_id = ?", box.RunID, box.NodeID).Order("iteration desc,id desc").First(&visit).Error; err != nil {
			return err
		}
		if run.Status != "running" || visit.ID != box.ID || visit.Status != "running" || visit.IsManual() {
			return instructionError("instruction_not_current", "本轮已停止或切换，未处理的补充要求不会继续派发")
		}
		var item models.WorkflowInstruction
		err = tx.Where("mailbox_id = ? AND generation = ? AND state = ?", box.ID, box.Generation, "queued").Order("sequence,id").First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if sealIfEmpty {
				return tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", box.ID).Update("state", "sealed").Error
			}
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&models.WorkflowInstruction{}).Where("id = ? AND state = ?", item.ID, "queued").Update("state", "dispatching").Error; err != nil {
			return err
		}
		message = &runtime.AutoInstruction{ID: item.ID, Text: item.Text}
		return nil
	})
	return message, err
}

func (q *instructionQueue) Complete(ctx context.Context, id, state, reason string) error {
	if state != "applied" && state != "unknown" {
		return errors.New("invalid instruction acknowledgement")
	}
	return q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		box, err := lockInstructionMailbox(tx, q.mailboxID)
		if err != nil {
			return err
		}
		if box.Generation != q.generation {
			return instructionError("instruction_not_current", "执行代次已更新，无法确认旧会话处理结果")
		}
		result := tx.Model(&models.WorkflowInstruction{}).Where("id = ? AND mailbox_id = ? AND generation = ? AND state = ?", id, box.ID, box.Generation, "dispatching").
			Updates(map[string]any{"state": state, "error": reason})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("instruction acknowledgement does not match an active dispatch")
		}
		return nil
	})
}

func (q *instructionQueue) RestartAttempt(ctx context.Context) error {
	generation := uuid.NewString()
	err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		box, err := lockInstructionMailbox(tx, q.mailboxID)
		if err != nil {
			return err
		}
		if box.Generation != q.generation {
			return instructionError("instruction_not_current", "执行代次已更新，无法重启旧执行尝试")
		}
		if err := retireInstructions(tx, box.ID, box.Generation, "本次执行尝试已结束，未处理的要求未转移，请核对后重新提交"); err != nil {
			return err
		}
		return tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", box.ID).
			Updates(map[string]any{"generation": generation, "state": "open", "attempt": max(1, box.Attempt) + 1}).Error
	})
	if err == nil {
		q.generation = generation
	}
	return err
}

func (e *Engine) EnqueueInstruction(ctx context.Context, runID, nodeID, actor string, input InstructionInput) (*models.WorkflowInstruction, error) {
	input.Text = strings.TrimSpace(input.Text)
	if actor == "" || input.Iteration < 1 || input.Generation == "" || len(input.Generation) > 36 || strings.TrimSpace(input.ClientMessageID) == "" || len(input.ClientMessageID) > 128 || input.Text == "" || !utf8.ValidString(input.Text) || len(input.Text) > 16<<10 {
		return nil, instructionError("invalid_instruction", "请选择当前执行轮次并填写 16 KB 以内的补充要求")
	}
	var target models.WorkflowInstructionMailbox
	if err := e.db.WithContext(ctx).Where("run_id = ? AND node_id = ?", runID, nodeID).Order("id desc").First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, instructionError("instruction_not_current", "当前节点尚未接收补充要求或已经切换")
		}
		return nil, err
	}
	var item models.WorkflowInstruction
	err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		box, err := lockInstructionMailbox(tx, target.ID)
		if err != nil {
			return err
		}
		err = tx.Where("mailbox_id = ? AND generation = ? AND client_message_id = ?", box.ID, input.Generation, input.ClientMessageID).First(&item).Error
		if err == nil {
			if item.ActorID != actor || item.Text != input.Text || item.Iteration != input.Iteration {
				return instructionError("instruction_idempotency_conflict", "该消息身份已用于其他补充要求")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if box.Generation != input.Generation || box.Iteration != input.Iteration {
			return instructionError("instruction_not_current", "节点执行轮次已更新，草稿已保留，请核对目标后重试")
		}
		if box.State != "open" {
			return instructionError("instruction_closed", "本轮正在结束，未接收此补充要求；可在工作流结束后续聊")
		}
		var run models.Run
		if err := tx.First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		var visit models.StateRun
		if err := tx.Where("run_id = ? AND node_id = ?", runID, nodeID).Order("iteration desc,id desc").First(&visit).Error; err != nil {
			return err
		}
		node := run.Graph.FindNode(nodeID)
		if run.Status != "running" || visit.ID != box.ID || visit.Status != "running" || visit.IsManual() || node == nil || node.Type != "agent" || node.Caps == nil || node.Caps.Clarify() {
			return instructionError("instruction_not_current", "当前目标不是正在自动执行的节点")
		}
		var pending int64
		if err := tx.Model(&models.WorkflowInstruction{}).Where("mailbox_id = ? AND generation = ? AND state IN ?", box.ID, box.Generation, []string{"queued", "dispatching"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending >= chatsession.DefaultCapacity {
			return instructionError("instruction_queue_full", "补充要求排队已满，请等待当前轮处理")
		}
		sequence := box.NextSequence + 1
		if err := tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", box.ID).Update("next_sequence", sequence).Error; err != nil {
			return err
		}
		item = models.WorkflowInstruction{ID: uuid.NewString(), MailboxID: box.ID, RunID: runID, NodeID: nodeID, Iteration: box.Iteration, Attempt: max(1, box.Attempt), Generation: box.Generation, ClientMessageID: input.ClientMessageID, ActorID: actor, Text: input.Text, State: "queued", Sequence: sequence}
		return tx.Create(&item).Error
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// InstructionHistory pages the node's history, optionally selecting an exact
// generation. Accepted receipts survive closed visits without waking a guest.
func (e *Engine) InstructionHistory(ctx context.Context, runID, nodeID, generation string, before uint64) (*models.WorkflowInstructionMailbox, []models.WorkflowInstruction, bool, error) {
	var box models.WorkflowInstructionMailbox
	if err := e.db.WithContext(ctx).Where("run_id = ? AND node_id = ?", runID, nodeID).Order("id desc").First(&box).Error; err != nil {
		return nil, nil, false, err
	}
	query := e.db.WithContext(ctx).Where("run_id = ? AND node_id = ?", runID, nodeID)
	if generation != "" {
		query = query.Where("generation = ?", generation)
	}
	if before > 0 {
		query = query.Where("sequence < ?", before)
	}
	var items []models.WorkflowInstruction
	if err := query.Order("sequence desc,id desc").Limit(51).Find(&items).Error; err != nil {
		return nil, nil, false, err
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return &box, items, more, nil
}

var _ runtime.AutoInstructions = (*instructionQueue)(nil)
