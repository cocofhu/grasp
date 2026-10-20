package services

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

const ledgerReadBatch = 2000

// ledgerRowFilter narrows the SQL scan of token_usage_events.
type ledgerRowFilter struct {
	since   *time.Time
	sources []string
}

func (f ledgerRowFilter) apply(q *gorm.DB) *gorm.DB {
	if f.since != nil {
		q = q.Where("created_at >= ?", f.since.UTC())
	}
	if len(f.sources) > 0 {
		q = q.Where("source IN ?", f.sources)
	}
	return q
}

// loadLedgerEvents streams ledger rows matching f (in batches, id order).
func loadLedgerEvents(ctx context.Context, db *gorm.DB, f ledgerRowFilter, fn func(models.TokenUsageEvent)) error {
	var batch []models.TokenUsageEvent
	q := f.apply(db.WithContext(ctx).Model(&models.TokenUsageEvent{}))
	err := q.FindInBatches(&batch, ledgerReadBatch, func(tx *gorm.DB, _ int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, ev := range batch {
			fn(ev)
		}
		return nil
	}).Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrTokenStatsTimeout
	}
	return err
}

func ledgerUsage(ev models.TokenUsageEvent) models.TokenUsage {
	return models.TokenUsage{
		InputTokens: ev.InputTokens, OutputTokens: ev.OutputTokens,
		CacheReadTokens: ev.CacheReadTokens, CacheWriteTokens: ev.CacheWriteTokens,
	}
}

func ledgerByModel(ev models.TokenUsageEvent) models.TokenUsageByModel {
	key := ev.ModelKey
	if key == "" {
		key = models.TokenUsageModelUnknown
	}
	return models.TokenUsageByModel{key: {
		InputTokens: ev.InputTokens, OutputTokens: ev.OutputTokens,
		CacheReadTokens: ev.CacheReadTokens, CacheWriteTokens: ev.CacheWriteTokens,
		Source: ev.ModelSource, Filled: ev.ModelFilled,
	}}
}

// loadLedgerTokenUsageRows maps ledger rows onto the global aggregation shape.
func (s *WorkspaceService) loadLedgerTokenUsageRows(ctx context.Context, f ledgerRowFilter) ([]globalTokenUsageRow, error) {
	out := make([]globalTokenUsageRow, 0, 256)
	err := loadLedgerEvents(ctx, s.db, f, func(ev models.TokenUsageEvent) {
		out = append(out, globalTokenUsageRow{
			id:           ev.ID,
			ts:           ev.CreatedAt,
			usage:        ledgerUsage(ev),
			byModel:      ledgerByModel(ev),
			runID:        ev.RunID,
			runTitle:     ev.RunTitle,
			workflowID:   ev.WorkflowID,
			workflowName: ev.WorkflowName,
			nodeID:       ev.NodeID,
			nodeType:     ev.NodeType,
			threadID:     ev.ThreadID,
			source:       orDefault(ev.Source, models.TokenLedgerSourceWorkflow),
			status:       orDefault(ev.Status, models.TokenLedgerStatusOK),
			phase:        ev.Phase,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
