package services

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRunExpiryStopFailureRetainsIdentityAndRetries(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	past := time.Now().Add(-time.Hour)
	row := models.Sandbox{Name: "expired-files", Purpose: "run", Status: "running", DestroyAt: &past}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "running")
	ds.fg.FailStop = true
	s.sweepOnce(context.Background())
	var failed models.Sandbox
	if err := db.First(&failed, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !ds.fg.Has(row.Name) || failed.Status == "stopped" || failed.DesiredState != "stopped" || failed.DestroyAt == nil || failed.Error == "" {
		t.Fatalf("failed stop lost the only file copy or its retry: %+v", failed)
	}
	// Restart/retry uses the same provider identity and never destroys it.
	ds.fg.FailStop = false
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("destroy_at", past).Error; err != nil {
		t.Fatal(err)
	}
	other := NewSandboxService(db, s.mgr, s.skills, s.host, SandboxOptions{})
	other.sweepOnce(context.Background())
	var stopped models.Sandbox
	if err := db.First(&stopped, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stopped.Name != row.Name || stopped.Status != "stopped" || stopped.DestroyAt != nil || !ds.fg.Has(row.Name) {
		t.Fatalf("retry must stop compute and retain files: %+v", stopped)
	}
}

func TestRunReconcileKeepsStoppedAndUnavailableWorkspaces(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	for _, status := range []string{"stopped", "error", "not_found"} {
		row := models.Sandbox{Name: "run-" + status, Purpose: "run", RunID: "r", Status: "running"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		ds.setStatus(row.Name, status)
	}
	s.ReconcileOnStartup(context.Background(), nil)
	var rows []models.Sandbox
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("reconciliation dropped recovery attribution: %d", len(rows))
	}
	for _, row := range rows {
		if !ds.fg.Has(row.Name) || row.Status == "running" {
			t.Fatalf("passive observation must not destroy or wake: %+v", row)
		}
	}
}

func TestRunSandboxRetryAndLoopKeepEveryAttempt(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	for index, name := range []string{"first-attempt", "retry-same-visit", "next-loop"} {
		info := runtimeInfo(name)
		info.RunID, info.NodeID = "run", "implement"
		info.Iteration = 1
		if index == 2 {
			info.Iteration = 2
		}
		s.BeginRunSandbox(info)
		placeholder := info.Name
		info.Name, info.CorrelationName = "gateway-"+name, placeholder
		ds.setStatus(info.Name, "running")
		s.RegisterRunSandbox(info)
		s.RetireRunSandbox(info.Name)
	}
	var rows []models.Sandbox
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("attempt replaced an earlier workspace: %+v", rows)
	}
	for i, row := range rows {
		if !ds.fg.Has(row.Name) || row.Status != "running" || row.DestroyAt == nil {
			t.Fatalf("attempt %d lost its file identity: %+v", i, row)
		}
	}
	if rows[0].Iteration != 1 || rows[1].Iteration != 1 || rows[2].Iteration != 2 {
		t.Fatalf("workspace visit attribution lost: %+v", rows)
	}
}
