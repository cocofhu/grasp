package services

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// CronTokenHooks registers/unregisters shared MCP tokens for a cron execution.
type CronTokenHooks struct {
	Register   func(projectID, threadID, agent string) (token string, specs []sandbox.MCPServerSpec)
	Unregister func(token string)
}

// CronDelivery is a structured deliverToChannel result for the unified QQ
// egress (changed / unchanged / failed).
type CronDelivery struct {
	ProjectID string
	// AgentName routes delivery to the Channel bound to this Agent (not
	// "latest channel in project").
	AgentName string
	Category  string // job name / pr / daily — used for minimal templates
	Kind      string // changed | unchanged | failed (empty → classify from Text)
	Text      string
}

// ChannelDeliverer pushes a cron result to a project's configured channel.
// Implemented by channels.Manager and injected via SetChannelDeliverer.
// DeliverCron is the coordinated egress (busy → silent push queue, idle → immediate).
type ChannelDeliverer interface {
	DeliverCron(d CronDelivery) error
}

// CronScheduler polls due AgentCronJob rows and executes them via fresh PM sandboxes.
type CronScheduler struct {
	db    *gorm.DB
	pm    *PmService
	sbx   *SandboxService
	turns *PmTurnRunner
	hooks CronTokenHooks
	owner string

	deliverer ChannelDeliverer

	parallel atomic.Int64
	staleMin atomic.Int64
	active   atomic.Int64
}

// SetChannelDeliverer wires the optional cron→channel result push. Safe to call
// before Start.
func (s *CronScheduler) SetChannelDeliverer(d ChannelDeliverer) { s.deliverer = d }

// NewCronScheduler builds a scheduler with defaults (parallel=3, stale=120m).
func NewCronScheduler(db *gorm.DB, pm *PmService, sbx *SandboxService, turns *PmTurnRunner, hooks CronTokenHooks) *CronScheduler {
	s := &CronScheduler{
		db: db, pm: pm, sbx: sbx, turns: turns, hooks: hooks,
		owner: "cf-" + uuid.NewString()[:8],
	}
	s.parallel.Store(3)
	s.staleMin.Store(120)
	return s
}

// Start runs the poll loop until ctx is cancelled.
func (s *CronScheduler) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		s.tick(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.tick(ctx)
			}
		}
	}()
}

func (s *CronScheduler) tryAcquire() bool {
	for {
		cur := s.active.Load()
		max := s.parallel.Load()
		if cur >= max {
			return false
		}
		if s.active.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

func (s *CronScheduler) releaseSlot() {
	s.active.Add(-1)
}

func (s *CronScheduler) tick(ctx context.Context) {
	stale := time.Duration(s.staleMin.Load()) * time.Minute
	limit := int(s.parallel.Load()) * 2
	if limit < 8 {
		limit = 8
	}
	now := time.Now()
	cutoff := now.Add(-stale)
	var jobs []models.AgentCronJob
	err := s.db.Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Where("claimed_at IS NULL OR claimed_at < ?", cutoff).
		Order("next_run_at asc").Limit(limit).Find(&jobs).Error
	if err != nil {
		log.Warn().Err(err).Msg("cron scheduler scan failed")
		return
	}
	for i := range jobs {
		job := jobs[i]
		if !s.tryClaim(&job, now) {
			continue
		}
		if !s.tryAcquire() {
			s.releaseClaim(&job)
			continue
		}
		go func(j models.AgentCronJob) {
			defer s.releaseSlot()
			s.execute(ctx, &j)
		}(job)
	}
}

func (s *CronScheduler) tryClaim(job *models.AgentCronJob, now time.Time) bool {
	res := s.db.Model(&models.AgentCronJob{}).
		Where("id = ? AND enabled = ? AND (claimed_at IS NULL OR claimed_at < ?)",
			job.ID, true, now.Add(-time.Duration(s.staleMin.Load())*time.Minute)).
		Updates(map[string]any{"claimed_at": now, "claim_owner": s.owner, "updated_at": now})
	return res.Error == nil && res.RowsAffected == 1
}

func (s *CronScheduler) releaseClaim(job *models.AgentCronJob) {
	if err := s.db.Model(&models.AgentCronJob{}).Where("id = ?", job.ID).
		Updates(map[string]any{"claimed_at": nil, "claim_owner": "", "updated_at": time.Now()}).Error; err != nil {
		log.Error().Err(err).Str("job", job.ID).Msg("cron release claim failed")
	}
}

func (s *CronScheduler) execute(ctx context.Context, job *models.AgentCronJob) {
	start := time.Now()
	run := models.AgentCronRun{
		ID: "crun-" + uuid.NewString()[:12], JobID: job.ID, StartedAt: start,
	}
	defer func() {
		fin := time.Now()
		run.FinishedAt = &fin
		if err := s.db.Create(&run).Error; err != nil {
			log.Error().Err(err).Str("job", job.ID).Str("run", run.ID).
				Str("status", run.Status).Msg("cron persist run failed")
		}
		s.finishJob(job, &run)
	}()

	// Cron is agent-owned (not PM-only). Skip only when the agent profile is gone.
	if s.sbx == nil || s.turns == nil || s.hooks.Register == nil {
		run.Status = "error"
		run.Error = "scheduler runtime unavailable"
		return
	}
	ag, ok := s.sbx.skills.Get(job.AgentName)
	if !ok {
		run.Status = "skipped"
		run.Error = "agent unavailable"
		return
	}
	if !AgentProjectMatches(ag, job.ProjectID) {
		run.Status = "skipped"
		run.Error = "agent home project mismatch"
		return
	}

	token, specs := s.hooks.Register(job.ProjectID, job.ThreadID, job.AgentName)
	if token == "" || len(specs) == 0 {
		run.Status = "skipped"
		run.Error = "agent home project mismatch"
		return
	}
	defer func() {
		if s.hooks.Unregister != nil {
			s.hooks.Unregister(token)
		}
	}()

	row, err := s.sbx.OpenAgentSandboxFresh(ctx, job.AgentName, job.ProjectID, job.ThreadID, token, specs)
	if err != nil {
		run.Status = "error"
		run.Error = err.Error()
		return
	}
	run.SandboxID = row.ID

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		var cur models.Sandbox
		if err := s.db.First(&cur, row.ID).Error; err == nil {
			if cur.Status == "running" {
				row = &cur
				break
			}
			if cur.Status == "error" {
				run.Status = "error"
				run.Error = cur.Error
				s.destroyCronSandbox(ctx, row.ID, job.ID)
				return
			}
		}
		time.Sleep(time.Second)
	}
	if row.Status != "running" {
		run.Status = "error"
		run.Error = "sandbox not ready"
		s.destroyCronSandbox(ctx, row.ID, job.ID)
		return
	}

	prompt := "请先用 context-store / memory-store 拉取所需历史与记忆，再执行任务。\n\n" + job.Prompt
	userMsg, err := s.pm.AppendMessageSource(job.ThreadID, "user", prompt, "cron", nil, nil, nil, nil, nil)
	if err != nil {
		run.Status = "error"
		run.Error = err.Error()
		s.destroyCronSandbox(ctx, row.ID, job.ID)
		return
	}
	run.MessageID = userMsg.ID
	if err := s.turns.Start(job.ThreadID, userMsg.ID, row.ID, prompt, nil); err != nil {
		run.Status = "error"
		run.Error = err.Error()
		s.deliverCronFailure(job, "启动失败："+err.Error())
		s.destroyCronSandbox(ctx, row.ID, job.ID)
		return
	}
	turnDeadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(turnDeadline) {
		if !s.turns.Active(job.ThreadID) {
			run.Status = "ok"
			if err := s.maybeDeliver(job, userMsg); err != nil {
				run.Error = err.Error()
			}
			s.destroyCronSandbox(ctx, row.ID, job.ID)
			return
		}
		time.Sleep(2 * time.Second)
	}
	run.Status = "error"
	run.Error = "turn timeout"
	s.turns.Cancel(job.ThreadID)
	s.deliverCronFailure(job, "回合超时")
	s.destroyCronSandbox(ctx, row.ID, job.ID)
}

// deliverCronFailure pushes a structured failed result through the coordinated
// QQ egress when deliverToChannel is enabled (timeout / start failure).
func (s *CronScheduler) deliverCronFailure(job *models.AgentCronJob, reason string) {
	if job == nil || !job.DeliverToChannel || s.deliverer == nil {
		return
	}
	category := strings.TrimSpace(job.Name)
	if category == "" {
		category = "cron"
	}
	text := strings.TrimSpace(reason)
	if text == "" {
		text = "执行失败"
	}
	d := CronDelivery{
		ProjectID: job.ProjectID,
		AgentName: job.AgentName,
		Category:  category,
		Kind:      "failed",
		Text:      text,
	}
	if err := s.deliverer.DeliverCron(d); err != nil {
		log.Warn().Err(err).Str("job", job.ID).Msg("cron failure delivery failed")
	}
}

func (s *CronScheduler) destroyCronSandbox(ctx context.Context, id uint, jobID string) {
	if s.sbx == nil {
		return
	}
	if err := s.sbx.Destroy(ctx, id); err != nil {
		log.Warn().Err(err).Str("job", jobID).Uint("sandbox", id).Msg("cron sandbox destroy failed")
	}
}

// maybeDeliver classifies the turn's assistant reply and routes it through the
// coordinated channel egress (DeliverCron). Cron Work must not auto-merge PRs
// or start workflows; this path only pushes a short status report.
func (s *CronScheduler) maybeDeliver(job *models.AgentCronJob, userMsg models.ChatMessage) error {
	if !job.DeliverToChannel || s.deliverer == nil {
		return nil
	}
	has, err := s.pm.HasAssistantAfter(job.ThreadID, userMsg.ID)
	if err != nil || !has {
		return nil
	}
	text := s.lastAssistantReply(job.ThreadID, userMsg.CreatedAt)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	kind := ClassifyCronDeliveryText(text)
	category := strings.TrimSpace(job.Name)
	if category == "" {
		category = "cron"
	}
	d := CronDelivery{
		ProjectID: job.ProjectID,
		AgentName: job.AgentName,
		Category:  category,
		Kind:      kind,
		Text:      text,
	}
	if err := s.deliverer.DeliverCron(d); err != nil {
		log.Warn().Err(err).Str("job", job.ID).Str("kind", kind).Str("agent", job.AgentName).
			Msg("cron channel delivery failed")
		return err
	}
	return nil
}

// ClassifyCronDeliveryText maps assistant cron text into changed/unchanged/failed.
func ClassifyCronDeliveryText(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return "failed"
	}
	lower := strings.ToLower(t)
	switch {
	case strings.Contains(t, "失败") || strings.Contains(t, "错误：") || strings.Contains(t, "错误:") ||
		strings.Contains(lower, "failed") || strings.Contains(lower, "error:") || strings.Contains(lower, "failure"):
		return "failed"
	case strings.Contains(t, "无变化") || strings.Contains(t, "无更新") || strings.Contains(t, "无新的") ||
		strings.Contains(t, "暂无变化") || strings.Contains(lower, "no change") ||
		strings.Contains(lower, "unchanged") || strings.Contains(lower, "no updates") ||
		strings.Contains(lower, "nothing changed"):
		return "unchanged"
	default:
		return "changed"
	}
}

func (s *CronScheduler) lastAssistantReply(threadID string, after time.Time) string {
	msgs, err := s.pm.ListMessages(threadID)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == "assistant" && !m.CreatedAt.Before(after) {
			return m.Content
		}
	}
	return ""
}

func (s *CronScheduler) finishJob(job *models.AgentCronJob, run *models.AgentCronRun) {
	now := time.Now()
	updates := map[string]any{
		"claimed_at": nil, "claim_owner": "", "last_run_at": now,
		"last_status": run.Status, "last_error": run.Error, "updated_at": now,
	}
	switch run.Status {
	case "ok":
		updates["consecutive_errors"] = 0
		if job.ScheduleKind == "at" {
			updates["enabled"] = false
			updates["next_run_at"] = nil
		} else if next, err := NextScheduleTime(job.ScheduleKind, job.ScheduleExpr, job.Timezone, now); err == nil {
			updates["next_run_at"] = next
		}
	case "skipped":
		if job.ScheduleKind == "at" {
			updates["enabled"] = false
		} else if next, err := NextScheduleTime(job.ScheduleKind, job.ScheduleExpr, job.Timezone, now); err == nil {
			updates["next_run_at"] = next
		}
	default:
		n := job.ConsecutiveErrors + 1
		updates["consecutive_errors"] = n
		if n >= 5 {
			updates["enabled"] = false
		} else {
			// Persist UTC so SQLite text compare with UTC now stays correct (see NextScheduleTime).
			updates["next_run_at"] = now.Add(cronBackoff(n)).UTC()
		}
	}
	if err := s.db.Model(&models.AgentCronJob{}).Where("id = ?", job.ID).Updates(updates).Error; err != nil {
		log.Error().Err(err).Str("job", job.ID).Str("status", run.Status).
			Msg("cron finish job update failed")
	}
	log.Info().Str("job", job.ID).Str("status", run.Status).Str("err", run.Error).Msg("cron job finished")
}

func cronBackoff(n int) time.Duration {
	switch {
	case n <= 1:
		return 30 * time.Second
	case n == 2:
		return time.Minute
	case n == 3:
		return 5 * time.Minute
	case n == 4:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}
