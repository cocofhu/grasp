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
	Title        string `json:"title"`
	WorkflowName string `json:"workflowName"`
	Status       string `json:"status"`
}

type SoloTaskUsage struct {
	Total         int64    `json:"total"`
	EstimatedCost *float64 `json:"estimatedCost,omitempty"`
	Currency      string   `json:"currency"`
	UnpricedTotal int64    `json:"unpricedTotal"`
	Reported      bool     `json:"reported"`
	Incomplete    bool     `json:"incomplete"`
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
		return tx.Model(&models.Run{}).Select("id", "title", "workflow_name", "status").
			Where("workflow_id <> '' AND id IN (?)", runs).
			Order("started_at DESC, id DESC").
			Find(&out.WorkflowRuns).Error
	}, &sql.TxOptions{ReadOnly: true})
	return out, err
}

func soloTaskUsage(db *gorm.DB, id string) (SoloTaskUsage, error) {
	var buckets []struct {
		models.TokenUsage
		ModelKey string
		Legacy   int64
	}
	scope := db.Where("source = ? AND thread_id = ?", models.TokenLedgerSourceSolo, id)
	if err := scope.Session(&gorm.Session{}).Model(&models.TokenUsageEvent{}).
		Select(`model_key, SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens,
		        SUM(cache_read_tokens) AS cache_read_tokens, SUM(cache_write_tokens) AS cache_write_tokens,
		        MAX(CASE WHEN report_id IS NULL OR report_id = '' THEN 1 ELSE 0 END) AS legacy`).
		Group("model_key").Scan(&buckets).Error; err != nil {
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

	pricing := LoadTokenPricing(db)
	alias := unknownModelAlias(db)
	usage := SoloTaskUsage{Currency: pricing.Currency, Reported: reports.Reported > 0, Incomplete: reports.Incomplete > 0}
	var cost float64
	for _, bucket := range buckets {
		total := bucket.Total()
		if total <= 0 {
			continue
		}
		usage.Total += total
		usage.Reported = true
		usage.Incomplete = usage.Incomplete || bucket.Legacy > 0
		key := rebucketGlobalModelKey(orDefault(bucket.ModelKey, models.TokenUsageModelUnknown), alias)
		value, ok := pricing.Cost(key, bucket.TokenUsage)
		if !ok {
			usage.UnpricedTotal += total
			continue
		}
		cost += value
	}
	if usage.Total > usage.UnpricedTotal || (usage.Reported && !usage.Incomplete && usage.Total == 0) {
		usage.EstimatedCost = &cost
	}
	return usage, nil
}
