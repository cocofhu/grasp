package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestCronMaintenanceWaitsForClaimToBecomeActive(t *testing.T) {
	db := setupPmDB(t)
	now := time.Now().Add(-time.Minute)
	job := models.AgentCronJob{ID: "maintenance-race", Enabled: true, NextRunAt: &now, ScheduleKind: "every", ScheduleExpr: "1h"}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	claimEntered, claimRelease := make(chan struct{}), make(chan struct{})
	finishEntered, finishRelease := make(chan struct{}), make(chan struct{})
	if err := db.Callback().Update().Before("gorm:update").Register("test:block-cron-claim", func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "agent_cron_jobs" && values["claim_owner"] != nil && values["claim_owner"] != "" {
			close(claimEntered)
			<-claimRelease
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:block-cron-completion", func(tx *gorm.DB) {
		if tx.Statement.Table == "agent_cron_runs" {
			close(finishEntered)
			<-finishRelease
		}
	}); err != nil {
		t.Fatal(err)
	}
	s := NewCronScheduler(db, nil, nil, nil, CronTokenHooks{})
	tickDone := make(chan struct{})
	go func() { defer close(tickDone); s.tick(context.Background()) }()
	<-claimEntered
	pauseDone := make(chan struct{})
	go func() { defer close(pauseDone); s.Pause() }()
	select {
	case <-pauseDone:
		close(claimRelease)
		close(finishRelease)
		<-tickDone
		t.Fatal("maintenance paused while an uncounted claim could still launch")
	case <-time.After(50 * time.Millisecond):
	}
	close(claimRelease)
	<-pauseDone
	<-finishEntered
	if active := s.ActiveExecutions(); active != 1 {
		t.Errorf("maintenance lost the admitted execution: %d", active)
	}
	close(finishRelease)
	<-tickDone
	deadline := time.Now().Add(time.Second)
	for s.ActiveExecutions() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.ActiveExecutions() != 0 {
		t.Fatal("completed execution remained active")
	}
	// Once Pause returns, even a due job cannot be newly claimed.
	if err := db.Model(&job).Updates(map[string]any{"enabled": true, "next_run_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	s.tick(context.Background())
	var runs int64
	if err := db.Model(&models.AgentCronRun{}).Count(&runs).Error; err != nil || runs != 1 {
		t.Fatalf("paused scheduler admitted more work: runs=%d err=%v", runs, err)
	}
}

func TestCodexRefreshTargetsSelectedCredentialAndCannotReviveIt(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	svc := NewCredentialService(db)
	login := `{"auth_mode":"chatgpt","tokens":{"access_token":"original","refresh_token":"refresh"}}`
	refreshed := `{"auth_mode":"chatgpt","tokens":{"access_token":"refreshed","refresh_token":"refresh"}}`
	first, err := svc.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "First", Value: login})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "Second", Value: login})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.WriteBackCodexLoginCredential(second.ID, refreshed); err != nil {
		t.Fatal(err)
	}
	_, got, ok := svc.ResolveSelectedValue(first.ID)
	if !ok || got != login {
		t.Fatal("refresh overwrote another credential")
	}
	_, got, ok = svc.ResolveSelectedValue(second.ID)
	if !ok || got != refreshed {
		t.Fatal("refresh did not update selected credential")
	}
	if err = svc.Revoke(second.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.WriteBackCodexLoginCredential(second.ID, login); err == nil {
		t.Fatal("revoked credential revived")
	}
	if err = svc.Clear(first.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.WriteBackCodexLoginCredential(first.ID, refreshed); err == nil {
		t.Fatal("cleared credential revived")
	}
}

func TestMaintenanceArchiveIsPrivateAndRejectsSymlinks(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(filepath.Join(src, "auth.json"), []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivateTree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("archive exposed private files")
	}
	if err = os.Symlink(filepath.Join(src, "auth.json"), filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err = copyPrivateTree(src, dst); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatal("unsafe archive accepted symlink")
	}
}

func TestWorkflowGatePolicyUsesOrdinaryOverrideAndStopsAdmission(t *testing.T) {
	db := newTestDB(t)
	pm := NewPmService(db, NewAgentService(t.TempDir()))
	svc := NewGateAutoInvokeService(db, pm, nil, nil, CronTokenHooks{})
	wf := models.WorkflowDef{ID: "override", Name: "override", Defaults: &models.WorkflowDefaults{GateAuto: &models.WorkflowGateAutoPolicy{Enabled: true, AgentName: "reviewer", Variable: "approve", Prompt: "inspect", EnabledMcps: []string{"pm-workflow-write"}}}}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	p, ok := svc.effectiveWorkspace(GateAutoTask{WorkflowID: wf.ID})
	if !ok || p.PmLeaderAgent != "reviewer" || p.PmGateAutoVar != "approve" || !p.PmLeaderEnabled {
		t.Fatalf("override=%+v %v", p, ok)
	}
	svc.BeginMaintenance()
	svc.Enqueue(GateAutoTask{WorkflowID: wf.ID, Vars: map[string]any{"approve": true}})
	if svc.PendingMaintenance() != 0 {
		t.Fatal("maintenance accepted new gate task")
	}
}
