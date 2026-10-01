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
	since     *time.Time
	projectID string
	sources   []string
}

func (f ledgerRowFilter) apply(q *gorm.DB) *gorm.DB {
	if f.since != nil {
		q = q.Where("created_at >= ?", f.since.UTC())
	}
	if f.projectID != "" {
		q = q.Where("project_id = ?", f.projectID)
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
// Live project names win over write-time snapshots (renames); deleted projects
// keep their snapshot name.
func (s *ProjectService) loadLedgerTokenUsageRows(ctx context.Context, f ledgerRowFilter) ([]globalTokenUsageRow, error) {
	names := map[string]string{}
	for _, p := range s.List() {
		names[p.ID] = p.Name
	}
	out := make([]globalTokenUsageRow, 0, 256)
	err := loadLedgerEvents(ctx, s.db, f, func(ev models.TokenUsageEvent) {
		name := ev.ProjectName
		if live, ok := names[ev.ProjectID]; ok {
			name = live
		}
		out = append(out, globalTokenUsageRow{
			id:           ev.ID,
			ts:           ev.CreatedAt,
			usage:        ledgerUsage(ev),
			byModel:      ledgerByModel(ev),
			projectID:    ev.ProjectID,
			projectName:  name,
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

// ledgerSumsByProjectSource returns Σtotal per project per source.
func ledgerSumsByProjectSource(db *gorm.DB, projectIDs []string, sources []string) (map[string]map[string]int64, error) {
	type row struct {
		ProjectID string
		Source    string
		Total     int64
	}
	var rows []row
	q := db.Model(&models.TokenUsageEvent{}).
		Select("project_id, source, SUM(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens) AS total").
		Where("project_id IN ?", projectIDs)
	if len(sources) > 0 {
		q = q.Where("source IN ?", sources)
	}
	if err := q.Group("project_id, source").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]map[string]int64{}
	for _, r := range rows {
		m := out[r.ProjectID]
		if m == nil {
			m = map[string]int64{}
			out[r.ProjectID] = m
		}
		m[r.Source] += r.Total
	}
	return out, nil
}
