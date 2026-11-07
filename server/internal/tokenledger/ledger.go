// Package tokenledger writes the append-only token_usage_events ledger that
// backs every token analytics surface (global /stats, workspace board, topbar).
package tokenledger

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Entry is one usage report to be recorded. Usage / ByModel are the delta for
// this report (not a running total). Missing snapshot fields (workflow, run
// title) are resolved from RunID / ThreadID at write time.
type Entry struct {
	// ID identifies the actual ACP operation, retained across replay/retries.
	ID           string
	Partial      bool // the bridge has not confirmed a final measurement yet
	At           time.Time
	Source       string
	Phase        string
	Status       string
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
	if e.Usage != nil && !e.Usage.Valid() {
		return nil
	}
	for _, bucket := range e.ByModel {
		if !bucket.AsTokenUsage().Valid() {
			return nil
		}
	}
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
	if len(out) == 0 && (e.Usage != nil || len(e.ByModel) > 0) {
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

// Record records one actual operation. The production recorder journals reports
// before writing SQL and replays uncommitted reports after an outage/restart.
func Record(db *gorm.DB, e Entry) {
	if db == nil || e.Usage != nil && e.Usage.LedgerRecorded || (e.ID == "" && e.Usage == nil && len(e.ByModel) == 0) {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(e.Source+"\x00"+e.ID)))
	rows := Rows(e)
	for i := range rows {
		applyMeta(&rows[i], e)
		rows[i].ReportID = id
	}
	report := pendingReport{Receipt: models.TokenUsageReport{
		ID: id, CreatedAt: e.At, Source: e.Source, Phase: e.Phase, Status: e.Status,
		RunID: e.RunID, NodeID: e.NodeID, ThreadID: e.ThreadID, Reported: len(rows) > 0, Partial: e.Partial,
		WorkflowID: e.WorkflowID, NodeType: e.NodeType,
	}, Rows: rows}
	for _, row := range rows {
		report.Receipt.Total += row.Total()
	}
	if recorder, ok := db.Config.Plugins[recorderName].(*Recorder); ok {
		recorder.record(report)
		return
	}
	if err := commitReport(db, report); err != nil {
		log.Error().Err(err).Str("report_id", id).Msg("token ledger write failed (recorder not installed)")
	}
}

type pendingReport struct {
	Receipt models.TokenUsageReport  `json:"receipt"`
	Rows    []models.TokenUsageEvent `json:"rows"`
}

// commitReport is atomic and idempotent, including concurrent replay. An
// earlier unavailable measurement may be upgraded when its final usage arrives.
func commitReport(db *gorm.DB, report pendingReport) error {
	// Optional name enrichment happens after write-ahead journaling. A slow or
	// unavailable SQL store must never delay making the measurement durable.
	metadata := Entry{RunID: report.Receipt.RunID, WorkflowID: report.Receipt.WorkflowID}
	if len(report.Rows) > 0 {
		metadata.WorkflowName = report.Rows[0].WorkflowName
		metadata.RunTitle = report.Rows[0].RunTitle
	}
	resolve(db, &metadata)
	report.Receipt.WorkflowID = metadata.WorkflowID
	if len(report.Rows) > 0 {
		report.Rows = append([]models.TokenUsageEvent(nil), report.Rows...)
		for i := range report.Rows {
			applyMeta(&report.Rows[i], metadata)
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&report.Receipt)
		if result.Error != nil {
			return result.Error
		}
		var old models.TokenUsageReport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ?", report.Receipt.ID).Error; err != nil {
			return err
		}
		if old.Committed {
			if !replaceReport(old, report.Receipt) {
				return nil
			}
			// Only provisional measurements may be replaced by their final
			// snapshot; completed reports remain immutable across replay.
			if err := tx.Model(&old).Updates(map[string]any{
				"reported": report.Receipt.Reported, "partial": report.Receipt.Partial,
				"total": report.Receipt.Total, "status": report.Receipt.Status,
				"created_at": report.Receipt.CreatedAt, "workflow_id": report.Receipt.WorkflowID,
				"node_type": report.Receipt.NodeType,
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("report_id = ?", old.ID).Delete(&models.TokenUsageEvent{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&old).Update("committed", true).Error; err != nil {
			return err
		}
		if len(report.Rows) == 0 {
			return nil
		}
		// Reset database-generated ids when replaying a report after an
		// uncertain commit; receipt uniqueness decides whether to insert.
		rows := append([]models.TokenUsageEvent(nil), report.Rows...)
		for i := range rows {
			rows[i].ID = 0
			rows[i].ReportID = report.Receipt.ID
		}
		return tx.Create(&rows).Error
	})
}

func replaceReport(old, next models.TokenUsageReport) bool {
	if old.Reported && !next.Reported {
		return false
	}
	return !old.Reported && next.Reported || old.Partial && (!next.Partial || next.Total > old.Total)
}

func applyMeta(r *models.TokenUsageEvent, e Entry) {
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
}

// StatusFromNode maps a StateRun / node outcome status onto a ledger status.
func StatusFromNode(s string) string {
	switch s {
	case "failed":
		return models.TokenLedgerStatusFailed
	case "cancelled":
		return models.TokenLedgerStatusCancelled
	default:
		return models.TokenLedgerStatusOK
	}
}
