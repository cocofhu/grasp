package tokenledger

import (
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// BackfillMarkerKey is the Setting row recording that legacy usage was imported.
const BackfillMarkerKey = "token_ledger_backfilled_v1"

const backfillBatch = 500

// BackfillOnce imports legacy StateRun / ChatMessage usage into the ledger the
// first time the ledger table exists. Later usage is written live by the
// engine / PM / Studio sinks, so the import must not run again.
func BackfillOnce(db *gorm.DB) {
	var n int64
	if err := db.Model(&models.Setting{}).Where(&models.Setting{Key: BackfillMarkerKey}).Count(&n).Error; err != nil {
		log.Warn().Err(err).Msg("token ledger backfill: marker lookup failed")
		return
	}
	if n > 0 {
		return
	}
	if err := Backfill(db); err != nil {
		log.Warn().Err(err).Msg("token ledger backfill failed")
		return
	}
	_ = db.Save(&models.Setting{Key: BackfillMarkerKey, Value: "true", UpdatedAt: time.Now()}).Error
}

// Backfill (re)imports legacy usage. Idempotent: previously backfilled rows are
// deleted first; live-written rows are left untouched.
func Backfill(db *gorm.DB) error {
	if err := db.Where("backfilled = ?", true).Delete(&models.TokenUsageEvent{}).Error; err != nil {
		return err
	}
	meta := newMetaCache(db)
	imported := 0

	var srs []models.StateRun
	err := db.Model(&models.StateRun{}).
		Select("id", "run_id", "node_id", "node_type", "status", "usage", "usage_by_model", "started_at").
		Where("usage IS NOT NULL OR usage_by_model IS NOT NULL").
		FindInBatches(&srs, backfillBatch, func(tx *gorm.DB, _ int) error {
			var rows []models.TokenUsageEvent
			for _, sr := range srs {
				run := meta.run(sr.RunID)
				ts := run.startedAt
				if sr.StartedAt != nil && !sr.StartedAt.IsZero() {
					ts = *sr.StartedAt
				}
				if ts.IsZero() {
					ts = run.createdAt
				}
				e := Entry{
					At: ts, Source: models.TokenLedgerSourceWorkflow,
					Phase: models.TokenLedgerPhaseProduction, Status: StatusFromNode(sr.Status),
					ProjectID: run.projectID, ProjectName: meta.projectName(run.projectID),
					WorkflowID: run.workflowID, WorkflowName: run.workflowName,
					RunID: sr.RunID, RunTitle: run.title, NodeID: sr.NodeID, NodeType: sr.NodeType,
					Usage: sr.Usage, ByModel: sr.UsageByModel,
				}
				rows = append(rows, backfillRows(e)...)
			}
			imported += len(rows)
			return insert(db, rows)
		}).Error
	if err != nil {
		return err
	}

	var msgs []models.ChatMessage
	err = db.Model(&models.ChatMessage{}).
		Select("id", "thread_id", "usage", "usage_by_model", "created_at").
		Where("role = ? AND (usage IS NOT NULL OR usage_by_model IS NOT NULL)", "assistant").
		FindInBatches(&msgs, backfillBatch, func(tx *gorm.DB, _ int) error {
			var rows []models.TokenUsageEvent
			for _, m := range msgs {
				pid := meta.threadProject(m.ThreadID)
				e := Entry{
					At: m.CreatedAt, Source: models.TokenLedgerSourcePM,
					Phase: models.TokenLedgerPhaseChat, Status: models.TokenLedgerStatusOK,
					ProjectID: pid, ProjectName: meta.projectName(pid), ThreadID: m.ThreadID,
					Usage: m.Usage, ByModel: m.UsageByModel,
				}
				rows = append(rows, backfillRows(e)...)
			}
			imported += len(rows)
			return insert(db, rows)
		}).Error
	if err != nil {
		return err
	}
	log.Info().Int("rows", imported).Msg("token ledger backfill done")
	return nil
}

func backfillRows(e Entry) []models.TokenUsageEvent {
	if e.At.IsZero() {
		return nil
	}
	rows := Rows(e)
	for i := range rows {
		applyMeta(&rows[i], e)
		rows[i].Backfilled = true
	}
	return rows
}

func insert(db *gorm.DB, rows []models.TokenUsageEvent) error {
	if len(rows) == 0 {
		return nil
	}
	return db.CreateInBatches(&rows, backfillBatch).Error
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

type runMeta struct {
	projectID, workflowID, workflowName, title string
	startedAt, createdAt                       time.Time
}

type metaCache struct {
	db       *gorm.DB
	runs     map[string]runMeta
	wfProj   map[string]string
	threads  map[string]string
	projects map[string]string
}

func newMetaCache(db *gorm.DB) *metaCache {
	c := &metaCache{
		db: db, runs: map[string]runMeta{}, wfProj: map[string]string{},
		threads: map[string]string{}, projects: map[string]string{},
	}
	var ps []models.Project
	_ = db.Select("id", "name").Find(&ps).Error
	for _, p := range ps {
		c.projects[p.ID] = p.Name
	}
	var wfs []models.WorkflowDef
	_ = db.Select("id", "project_id").Find(&wfs).Error
	for _, w := range wfs {
		c.wfProj[w.ID] = w.ProjectID
	}
	return c
}

func (c *metaCache) run(id string) runMeta {
	if m, ok := c.runs[id]; ok {
		return m
	}
	var r models.Run
	_ = c.db.Select("id", "workflow_id", "workflow_name", "title", "started_at", "created_at").
		Where("id = ?", id).Limit(1).Find(&r).Error
	m := runMeta{
		projectID: c.wfProj[r.WorkflowID], workflowID: r.WorkflowID,
		workflowName: r.WorkflowName, title: r.Title, startedAt: r.StartedAt, createdAt: r.CreatedAt,
	}
	c.runs[id] = m
	return m
}

func (c *metaCache) threadProject(id string) string {
	if p, ok := c.threads[id]; ok {
		return p
	}
	var th models.ChatThread
	_ = c.db.Select("id", "project_id").Where("id = ?", id).Limit(1).Find(&th).Error
	c.threads[id] = th.ProjectID
	return th.ProjectID
}

func (c *metaCache) projectName(id string) string { return c.projects[id] }
