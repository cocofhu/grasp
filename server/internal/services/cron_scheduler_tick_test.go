package services

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestCronSchedulerTickClaimsDueJob(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("CronTick", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateCronThread(p.ID, "agent-a", "定时")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	job := models.AgentCronJob{
		ID: "cron-tick-1", AgentName: "agent-a", ProjectID: p.ID, ThreadID: th.ID,
		Name: "n", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, NextRunAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	hooks := CronTokenHooks{
		Register: func(projectID, threadID, agentName string) (string, []sandbox.MCPServerSpec) {
			return "tok", nil
		},
		Unregister: func(token string) {},
	}
	s := NewCronScheduler(db, pm, nil, NewPmTurnRunner(pm, nil), hooks)
	s.setMaxParallel(2)
	s.tick(context.Background())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var run models.AgentCronRun
		if err := db.Where("job_id = ?", job.ID).First(&run).Error; err == nil && run.Status == "error" {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("cron tick did not finish job with error run (runtime unavailable path)")
}

func TestCronSchedulerTryAcquireParallelSlots(t *testing.T) {
	s := NewCronScheduler(nil, nil, nil, nil, CronTokenHooks{})
	s.setMaxParallel(1)
	if !s.tryAcquire() {
		t.Fatal("first slot")
	}
	if s.tryAcquire() {
		t.Fatal("second slot should fail at max=1")
	}
	s.releaseSlot()
	if !s.tryAcquire() {
		t.Fatal("slot after release")
	}
}

func TestCronSchedulerStartRunsTickOnce(t *testing.T) {
	db := setupPmDB(t)
	s := NewCronScheduler(db, nil, nil, nil, CronTokenHooks{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
}
