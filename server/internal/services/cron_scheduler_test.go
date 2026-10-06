package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestCronSchedulerSettingsClamp(t *testing.T) {
	db := setupPmDB(t)
	s := NewCronScheduler(db, NewPmService(db, nil), nil, nil, CronTokenHooks{})
	s.setMaxParallel(0)
	if s.parallel.Load() != 1 {
		t.Fatalf("parallel min want 1 got %d", s.parallel.Load())
	}
	s.setMaxParallel(100)
	if s.parallel.Load() != 16 {
		t.Fatalf("parallel max want 16 got %d", s.parallel.Load())
	}
	s.setClaimStaleMinutes(1)
	if s.staleMin.Load() != 30 {
		t.Fatalf("stale min want 30 got %d", s.staleMin.Load())
	}
	s.setClaimStaleMinutes(9999)
	if s.staleMin.Load() != 1440 {
		t.Fatalf("stale max want 1440 got %d", s.staleMin.Load())
	}
}

func TestCronSchedulerClaimRelease(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("CronClaim", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateCronThread(p.ID, "agent-a", "定时任务")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	job := models.AgentCronJob{
		ID: "cron-claim-1", AgentName: "agent-a", ProjectID: p.ID, ThreadID: th.ID,
		Name: "n", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, NextRunAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	a := NewCronScheduler(db, pm, nil, nil, CronTokenHooks{})
	b := NewCronScheduler(db, pm, nil, nil, CronTokenHooks{})
	if !a.tryClaim(&job, now) {
		t.Fatal("first claim")
	}
	if b.tryClaim(&job, now) {
		t.Fatal("second claim must fail while held")
	}
	a.releaseClaim(&job)
	if !b.tryClaim(&job, time.Now()) {
		t.Fatal("claim after release")
	}
}

func TestCronSchedulerStaleClaimReclaim(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("CronStale", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateCronThread(p.ID, "agent-a", "定时")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	staleAt := now.Add(-2 * time.Hour)
	job := models.AgentCronJob{
		ID: "cron-stale-1", AgentName: "agent-a", ProjectID: p.ID, ThreadID: th.ID,
		Name: "n", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, NextRunAt: &now, CreatedAt: now, UpdatedAt: now,
		ClaimedAt: &staleAt, ClaimOwner: "dead-owner",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	s := NewCronScheduler(db, pm, nil, nil, CronTokenHooks{})
	s.setClaimStaleMinutes(30)
	if !s.tryClaim(&job, now) {
		t.Fatal("stale claim should be reclaimable")
	}
	var got models.AgentCronJob
	if err := db.First(&got, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ClaimOwner != s.owner || got.ClaimedAt == nil {
		t.Fatalf("reclaimed owner=%q claimed=%v want %q", got.ClaimOwner, got.ClaimedAt, s.owner)
	}
}

func TestNextScheduleTimeCronAndEvery(t *testing.T) {
	from := time.Date(2026, 7, 21, 8, 30, 0, 0, time.UTC)
	next, err := NextScheduleTime("cron", "0 9 * * *", "UTC", from)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("cron next=%v want %v", next, want)
	}
	if next.Location() != time.UTC {
		t.Fatalf("cron Location=%v want UTC", next.Location())
	}
	every, err := NextScheduleTime("every", "30m", "", from)
	if err != nil || !every.Equal(from.Add(30*time.Minute)) {
		t.Fatalf("every=%v err=%v", every, err)
	}
	if every.Location() != time.UTC {
		t.Fatalf("every Location=%v want UTC", every.Location())
	}
	if _, err := NextScheduleTime("cron", "0 9 * * *", "Not/AZone", from); err == nil {
		t.Fatal("bad timezone should fail")
	}
}

func TestNextScheduleTimeShanghaiCronReturnsUTC(t *testing.T) {
	// Shanghai wall-clock 10:00 == 02:00 UTC; must return UTC Location for SQLite due compare.
	from := time.Date(2026, 7, 24, 23, 15, 0, 0, time.UTC)
	next, err := NextScheduleTime("cron", "0 10 * * *", "Asia/Shanghai", from)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 25, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next instant=%v want %v", next, want)
	}
	if next.Location() != time.UTC {
		t.Fatalf("Location=%v want UTC (time.Equal alone would miss Location leak)", next.Location())
	}
}

func TestCronSchedulerShanghaiNextRunAtDueAt0200Z(t *testing.T) {
	// Regression: non-UTC Location persisted as "+08:00" wall string made SQLite
	// text compare next_run_at <= now wait until ~10:00Z. Fixed path must be due at 02:00Z.
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("CronShanghaiDue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateCronThread(p.ID, "agent-a", "定时")
	if err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 7, 24, 23, 15, 0, 0, time.UTC)
	next, err := NextScheduleTime("cron", "0 10 * * *", "Asia/Shanghai", from)
	if err != nil {
		t.Fatal(err)
	}
	if next.Location() != time.UTC {
		t.Fatalf("precondition: next must be UTC, got %v", next.Location())
	}

	created := from
	job := models.AgentCronJob{
		ID: "cron-shanghai-due-1", AgentName: "agent-a", ProjectID: p.ID, ThreadID: th.ID,
		Name: "shanghai-10", Prompt: "p", ScheduleKind: "cron", ScheduleExpr: "0 10 * * *",
		Timezone: "Asia/Shanghai", Enabled: true, NextRunAt: &next, CreatedAt: created, UpdatedAt: created,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	at0200Z := time.Date(2026, 7, 25, 2, 0, 0, 0, time.UTC)
	var due []models.AgentCronJob
	if err := db.Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, at0200Z).
		Find(&due).Error; err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != job.ID {
		t.Fatalf("want due at 02:00Z, got %d jobs (pre-fix bug: only due around 10:00Z)", len(due))
	}

	// Old bug: Shanghai Location stored as "2026-07-25 10:00:00+08:00" is not <= "02:00Z" textually.
	// Ensure we did not persist a +08:00 wall string that would only become due at ~10:00Z.
	at0959Z := time.Date(2026, 7, 25, 9, 59, 0, 0, time.UTC)
	var stillDue []models.AgentCronJob
	if err := db.Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, at0959Z).
		Find(&stillDue).Error; err != nil {
		t.Fatal(err)
	}
	if len(stillDue) != 1 {
		t.Fatalf("fixed UTC persist must remain due before 10:00Z; got %d (old +08:00 row would not match)", len(stillDue))
	}
}

func TestCronSchedulerFinishJobUsesCronExpr(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("CronNext", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateCronThread(p.ID, "agent-a", "定时")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 8, 30, 0, 0, time.UTC)
	job := models.AgentCronJob{
		ID: "cron-next-1", AgentName: "agent-a", ProjectID: p.ID, ThreadID: th.ID,
		Name: "n", Prompt: "p", ScheduleKind: "cron", ScheduleExpr: "0 9 * * *",
		Timezone: "UTC", Enabled: true, NextRunAt: &now, CreatedAt: now, UpdatedAt: now,
		ClaimedAt: &now, ClaimOwner: "test",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	s := NewCronScheduler(db, pm, nil, nil, CronTokenHooks{})
	run := &models.AgentCronRun{Status: "ok"}
	s.finishJob(&job, run)

	var got models.AgentCronJob
	if err := db.First(&got, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.NextRunAt == nil {
		t.Fatal("next_run_at unset")
	}
	// finishJob uses time.Now(); cron "0 9 * * *" must land on 09:00:00, not a bare +1h offset.
	nextUTC := got.NextRunAt.UTC()
	if nextUTC.Minute() != 0 || nextUTC.Second() != 0 || nextUTC.Hour() != 9 {
		t.Fatalf("expected next 09:00 UTC boundary, got %v", nextUTC)
	}
}

func TestCronSchedulerExecuteUnavailable(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("CronExec", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateCronThread(p.ID, "agent-a", "定时")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	job := models.AgentCronJob{
		ID: "cron-exec-1", AgentName: "agent-a", ProjectID: p.ID, ThreadID: th.ID,
		Name: "n", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, NextRunAt: &now, CreatedAt: now, UpdatedAt: now,
		ClaimedAt: &now, ClaimOwner: "test",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	s := NewCronScheduler(db, pm, nil, nil, CronTokenHooks{})
	s.owner = "test"
	s.execute(context.Background(), &job)

	var run models.AgentCronRun
	if err := db.Where("job_id = ?", job.ID).First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "error" || !strings.Contains(run.Error, "scheduler runtime unavailable") {
		t.Fatalf("run=%+v", run)
	}
	var got models.AgentCronJob
	if err := db.First(&got, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ClaimedAt != nil || got.ClaimOwner != "" {
		t.Fatalf("claim not cleared: claimed=%v owner=%q", got.ClaimedAt, got.ClaimOwner)
	}
	if got.LastStatus != "error" {
		t.Fatalf("last_status=%q", got.LastStatus)
	}
}
