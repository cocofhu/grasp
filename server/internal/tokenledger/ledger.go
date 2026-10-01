// Package tokenledger writes the append-only token_usage_events ledger that
// backs every token analytics surface (global /stats, project board, topbar).
package tokenledger

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// Entry is one usage report to be recorded. Usage / ByModel are the delta for
// this report (not a running total). Missing snapshot fields (workflow, run
// title, project) are resolved from RunID / ThreadID at write time.
type Entry struct {
	At           time.Time
	Source       string
	Phase        string
	Status       string
	ProjectID    string
	ProjectName  string
	WorkflowID   string
	WorkflowName string
	RunID        string
	RunTitle     string
	NodeID       string
	NodeType     string
	ThreadID     string
	SandboxID    uint
	Usage        *models.TokenUsage
	ByModel      models.TokenUsageByModel
}

// Rows explodes an entry into one ledger row per model bucket. When the
// per-model breakdown does not cover the flattened total, the remainder is
// attributed to the unknown bucket so totals are never lost.
func Rows(e Entry) []models.TokenUsageEvent {
	buckets := models.CloneTokenUsageByModel(e.ByModel)
	if buckets == nil {
		buckets = models.TokenUsageByModel{}
	}
	if e.Usage != nil {
		var covered models.TokenUsage
		for _, b := range buckets {
			covered.InputTokens += b.InputTokens
			covered.OutputTokens += b.OutputTokens
			covered.CacheReadTokens += b.CacheReadTokens
			covered.CacheWriteTokens += b.CacheWriteTokens
		}
		rest := models.ModelTokenUsage{
			InputTokens:      positive(e.Usage.InputTokens - covered.InputTokens),
			OutputTokens:     positive(e.Usage.OutputTokens - covered.OutputTokens),
			CacheReadTokens:  positive(e.Usage.CacheReadTokens - covered.CacheReadTokens),
			CacheWriteTokens: positive(e.Usage.CacheWriteTokens - covered.CacheWriteTokens),
			Source:           models.TokenUsageSourceUnknown,
		}
		if rest.Total() > 0 {
			buckets = models.AddTokenUsageByModel(buckets, models.TokenUsageByModel{models.TokenUsageModelUnknown: rest})
		}
	}
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	status := e.Status
	if status == "" {
		status = models.TokenLedgerStatusOK
	}
	out := make([]models.TokenUsageEvent, 0, len(buckets))
	for mk, b := range buckets {
		if b.Total() <= 0 {
			continue
		}
		key := strings.TrimSpace(mk)
		if key == "" {
			key = models.TokenUsageModelUnknown
		}
		out = append(out, models.TokenUsageEvent{
			CreatedAt:        at,
			Source:           e.Source,
			Phase:            e.Phase,
			Status:           status,
			ProjectID:        e.ProjectID,
			WorkflowID:       e.WorkflowID,
			WorkflowName:     e.WorkflowName,
			RunID:            e.RunID,
			RunTitle:         e.RunTitle,
			NodeID:           e.NodeID,
			NodeType:         e.NodeType,
			ThreadID:         e.ThreadID,
			SandboxID:        e.SandboxID,
			ModelKey:         key,
			ModelSource:      b.Source,
			ModelFilled:      b.Filled,
			InputTokens:      b.InputTokens,
			OutputTokens:     b.OutputTokens,
			CacheReadTokens:  b.CacheReadTokens,
			CacheWriteTokens: b.CacheWriteTokens,
		})
	}
	if len(out) == 0 && e.Usage != nil {
		// An explicit all-zero report still marks usage as reported (UI shows 0, not —).
		out = append(out, models.TokenUsageEvent{
			CreatedAt: at, Source: e.Source, Phase: e.Phase, Status: status,
			RunID: e.RunID, NodeID: e.NodeID, NodeType: e.NodeType, ThreadID: e.ThreadID,
			SandboxID: e.SandboxID, ModelKey: models.TokenUsageModelUnknown,
			ModelSource: models.TokenUsageSourceUnknown,
		})
	}
	return out
}

func positive(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// Record resolves snapshot metadata and appends the entry's rows. Errors are
// logged, never returned: accounting must not fail the LLM call that produced it.
func Record(db *gorm.DB, e Entry) {
	if db == nil || (e.Usage == nil && len(e.ByModel) == 0) {
		return
	}
	rows := Rows(e)
	if len(rows) == 0 {
		return
	}
	resolve(db, &e)
	for i := range rows {
		applyMeta(&rows[i], e)
	}
	if err := db.Create(&rows).Error; err != nil {
		log.Warn().Err(err).Str("run_id", e.RunID).Str("thread_id", e.ThreadID).
			Str("source", e.Source).Msg("token ledger write failed")
	}
}

func applyMeta(r *models.TokenUsageEvent, e Entry) {
	r.ProjectID = e.ProjectID
	r.ProjectName = e.ProjectName
	r.WorkflowID = e.WorkflowID
	r.WorkflowName = e.WorkflowName
	r.RunTitle = e.RunTitle
}

func resolve(db *gorm.DB, e *Entry) {
	if e.RunID != "" && (e.WorkflowID == "" || e.RunTitle == "" || e.WorkflowName == "") {
		var run models.Run
		if err := db.Select("id", "workflow_id", "workflow_name", "title").
			Where("id = ?", e.RunID).Limit(1).Find(&run).Error; err == nil && run.ID != "" {
			if e.WorkflowID == "" {
				e.WorkflowID = run.WorkflowID
			}
			if e.WorkflowName == "" {
				e.WorkflowName = run.WorkflowName
			}
			if e.RunTitle == "" {
				e.RunTitle = run.Title
			}
		}
	}
	if e.ProjectID == "" && e.WorkflowID != "" {
		var wf models.WorkflowDef
		if err := db.Select("id", "project_id").Where("id = ?", e.WorkflowID).
			Limit(1).Find(&wf).Error; err == nil {
			e.ProjectID = wf.ProjectID
		}
	}
	if e.ProjectID == "" && e.ThreadID != "" {
		var th models.ChatThread
		if err := db.Select("id", "project_id").Where("id = ?", e.ThreadID).
			Limit(1).Find(&th).Error; err == nil {
			e.ProjectID = th.ProjectID
		}
	}
	if e.ProjectID != "" && e.ProjectName == "" {
		var p models.Project
		if err := db.Select("id", "name").Where("id = ?", e.ProjectID).
			Limit(1).Find(&p).Error; err == nil && p.ID != "" {
			e.ProjectName = p.Name
		}
	}
}
