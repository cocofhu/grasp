package services

import (
	"context"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
	"gorm.io/gorm"
)

// TokenAccountingStatus makes unavailable or provisional measurements explicit.
// Counts cover receipts created since this accounting version was installed;
// legacy totals alone cannot establish historical reporting completeness.
type TokenAccountingStatus struct {
	Incomplete       bool  `json:"incomplete"`
	IncompleteTurns  int64 `json:"incompleteTurns"`
	PendingReports   int   `json:"pendingReports"`
	LegacyUnverified bool  `json:"legacyUnverified"`
}

func loadTokenAccountingStatus(ctx context.Context, db *gorm.DB, filter GlobalTokenStatsQuery, window windowSlice) (TokenAccountingStatus, error) {
	apply := func(q *gorm.DB) *gorm.DB {
		if window.hasStart {
			q = q.Where("created_at >= ?", window.start.UTC())
		}
		if !window.end.IsZero() {
			q = q.Where("created_at <= ?", window.end.UTC())
		}
		for _, field := range []struct{ key, value string }{
			{"source", filter.Source}, {"run_id", filter.RunID}, {"workflow_id", filter.WorkflowID},
			{"node_type", filter.NodeType}, {"phase", filter.Phase}, {"status", filter.Status},
		} {
			value := strings.TrimSpace(field.value)
			if value != "" && value != "all" {
				q = q.Where(field.key+" = ?", value)
			}
		}
		return q
	}
	q := apply(db.WithContext(ctx).Model(&models.TokenUsageReport{})).Where("(reported = ? OR partial = ?)", false, true)
	out := TokenAccountingStatus{PendingReports: tokenledger.Pending(db)}
	if err := q.Count(&out.IncompleteTurns).Error; err != nil {
		return out, err
	}
	var legacy int64
	if err := apply(db.WithContext(ctx).Model(&models.TokenUsageEvent{})).Where("(report_id IS NULL OR report_id = ?)", "").Count(&legacy).Error; err != nil {
		return out, err
	}
	out.LegacyUnverified = legacy > 0
	out.Incomplete = out.IncompleteTurns > 0 || out.PendingReports > 0 || out.LegacyUnverified
	return out, nil
}
