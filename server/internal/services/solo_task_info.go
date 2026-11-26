package services

import (
	"context"
	"database/sql"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

type SoloTaskInfo struct {
	Usage        SoloTaskUsage `json:"usage"`
	WorkflowRuns []SoloTaskRun `json:"workflowRuns"`
}

type SoloTaskRun struct {
	ID           string `json:"id"`
	WorkflowName string `json:"workflowName"`
}

type SoloTaskUsage struct {
	models.TokenUsage
	Total      int64 `json:"total"`
	Reported   bool  `json:"reported"`
	Incomplete bool  `json:"incomplete"`
}

// TaskInfo summarizes the entire owned Solo session, including previous sandboxes.
func (s *SoloService) TaskInfo(ctx context.Context, id, user string) (SoloTaskInfo, error) {
	session, err := s.Require(id, user)
	if err != nil {
		return SoloTaskInfo{}, err
	}
	out := SoloTaskInfo{WorkflowRuns: []SoloTaskRun{}}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		usage, err := soloTaskUsage(tx, session.ID)
		if err != nil {
			return err
		}
		out.Usage = usage

		// A workflow turn can pin an explicit @ reference to this Solo session.
		runs := tx.Model(&models.RunReference{}).Select("run_id").
			Where("kind = ? AND target = ? AND user_id = ?", models.ReferenceKindSolo, session.ID, session.UserID)
		const workflowName = "COALESCE(NULLIF(runs.workflow_name, ''), workflow_defs.name, '')"
		return tx.Model(&models.Run{}).Select("runs.id, "+workflowName+" AS workflow_name").
			Joins("LEFT JOIN workflow_defs ON workflow_defs.id = runs.workflow_id").
			Where("runs.workflow_id <> '' AND runs.id IN (?)", runs).
			Where(workflowName + " <> ''").
			Order("runs.started_at DESC, runs.id DESC").
			Find(&out.WorkflowRuns).Error
	}, &sql.TxOptions{ReadOnly: true})
	return out, err
}

func soloTaskUsage(db *gorm.DB, id string) (SoloTaskUsage, error) {
	var totals struct {
		models.TokenUsage
		Legacy int64
	}
	scope := db.Where("source = ? AND thread_id = ?", models.TokenLedgerSourceSolo, id)
	if err := scope.Session(&gorm.Session{}).Model(&models.TokenUsageEvent{}).
		Select(`COALESCE(SUM(input_tokens), 0) AS input_tokens,
		        COALESCE(SUM(output_tokens), 0) AS output_tokens,
		        COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
		        COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
		        COALESCE(MAX(CASE WHEN report_id IS NULL OR report_id = '' THEN 1 ELSE 0 END), 0) AS legacy`).
		Scan(&totals).Error; err != nil {
		return SoloTaskUsage{}, err
	}
	var reports struct {
		Reported   int64
		Incomplete int64
	}
	if err := scope.Session(&gorm.Session{}).Model(&models.TokenUsageReport{}).
		Select(`COALESCE(SUM(CASE WHEN reported = ? THEN 1 ELSE 0 END), 0) AS reported,
		        COALESCE(SUM(CASE WHEN reported = ? OR partial = ? THEN 1 ELSE 0 END), 0) AS incomplete`,
			true, false, true).Scan(&reports).Error; err != nil {
		return SoloTaskUsage{}, err
	}

	total := totals.Total()
	return SoloTaskUsage{
		TokenUsage: totals.TokenUsage,
		Total:      total,
		Reported:   reports.Reported > 0 || total > 0,
		Incomplete: reports.Incomplete > 0 || totals.Legacy > 0,
	}, nil
}
