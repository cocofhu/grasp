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
	Total         int64                    `json:"total"`
	Reported      bool                     `json:"reported"`
	Incomplete    bool                     `json:"incomplete"`
	ByModel       models.TokenUsageByModel `json:"byModel,omitempty"`
	Cost          *float64                 `json:"cost,omitempty"`
	Currency      string                   `json:"currency"`
	UnpricedTotal int64                    `json:"unpricedTotal,omitempty"`
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
	var buckets []struct {
		models.ModelTokenUsage
		ModelKey string
		Legacy   int64
	}
	scope := db.Where("source = ? AND thread_id = ?", models.TokenLedgerSourceSolo, id)
	// Group by the exact key even when the database uses a case-insensitive collation.
	if err := scope.Session(&gorm.Session{}).Model(&models.TokenUsageEvent{}).
		Select(`COALESCE(MIN(model_key), '') AS model_key,
		        COALESCE(SUM(input_tokens), 0) AS input_tokens,
		        COALESCE(SUM(output_tokens), 0) AS output_tokens,
		        COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
		        COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
		        MAX(CASE WHEN model_filled = ? OR model_source = ? THEN 1 ELSE 0 END) AS filled,
		        MAX(CASE WHEN report_id IS NULL OR report_id = '' THEN 1 ELSE 0 END) AS legacy`,
			true, models.TokenUsageSourceBridge).
		Group("HEX(model_key)").Scan(&buckets).Error; err != nil {
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
	usage := SoloTaskUsage{Currency: pricing.Currency, Incomplete: reports.Incomplete > 0}
	var cost float64
	for _, bucket := range buckets {
		usage.Incomplete = usage.Incomplete || bucket.Legacy > 0
		part := bucket.AsTokenUsage()
		models.AddTokenUsage(&usage.TokenUsage, &part)
		total := part.Total()
		if total == 0 {
			continue
		}
		key := bucket.ModelKey
		switch {
		case models.IsWeakModelKey(key) || key == models.TokenUsageModelUnknown:
			key = models.TokenUsageModelUnknown
			bucket.Source, bucket.Filled = models.TokenUsageSourceUnknown, false
		case bucket.Filled:
			bucket.Source = models.TokenUsageSourceBridge
		}
		usage.ByModel = models.AddTokenUsageByModel(usage.ByModel, models.TokenUsageByModel{key: bucket.ModelTokenUsage})
		amount, priced := pricing.Cost(rebucketGlobalModelKey(key, alias), part)
		if !priced {
			usage.UnpricedTotal += total
			continue
		}
		cost += amount
	}
	usage.Total = usage.TokenUsage.Total()
	usage.Reported = reports.Reported > 0 || usage.Total > 0
	if usage.Total > usage.UnpricedTotal || (usage.Reported && !usage.Incomplete && usage.Total == 0) {
		usage.Cost = &cost
	}
	return usage, nil
}
