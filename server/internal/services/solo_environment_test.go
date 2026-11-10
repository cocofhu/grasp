package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloStoppedEnvironmentSurvivesObservationAndFailedResume(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 2)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resident ready", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	row, _ := solo.Require(view.ID, "alice")
	resident, _ := sbx.Get(row.SandboxID)
	marker := filepath.Join(t.TempDir(), "unsaved.txt")
	if err := os.WriteFile(marker, []byte("dirty file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Sandbox{}).Where("id = ?", resident.ID).Update("home_dir", filepath.Dir(marker)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.StopEnvironment(context.Background(), view.ID, "mallory"); err == nil {
		t.Fatal("other user stopped private environment")
	}
	stopped, err := solo.StopEnvironment(context.Background(), view.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != models.SoloStatusStopped || stopped.Sandbox.DesiredState != "stopped" || stopped.Sandbox.Recovery.Process != "restarted" {
		t.Fatalf("%+v", stopped)
	}
	if sbx.activeSoloSandboxCount() != 0 {
		t.Fatal("stopped environment consumes compute quota")
	}
	sbx.ReconcileOnStartup(context.Background(), nil)
	sbx.RecoverSoloSandboxes(context.Background())
	if _, err := solo.Get(context.Background(), view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := sbx.EnsureSoloRunning(context.Background(), resident.ID); err == nil {
		t.Fatal("automatic recovery overrode manual stop")
	}
	if _, err := solo.Send(view.ID, "alice", "must not wake", nil); err == nil {
		t.Fatal("send silently woke manually stopped environment")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stopping deleted user data", err)
	}
	if !gateway.Has(resident.Name) {
		t.Fatal("stop deleted gateway identity")
	}
	gateway.FailStart = true
	if _, err := solo.ResumeEnvironment(context.Background(), view.ID, "alice"); err == nil {
		t.Fatal("expected start failure")
	}
	kept, _ := sbx.Get(resident.ID)
	if kept.Name != resident.Name || kept.Token != resident.Token || !gateway.Has(resident.Name) {
		t.Fatal("failed resume replaced the environment")
	}
	gateway.FailStart = false
	resumed, err := solo.ResumeEnvironment(context.Background(), view.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != models.SoloStatusRunning || resumed.SandboxID != resident.ID {
		t.Fatalf("%+v", resumed)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
}

func TestSoloOperationLeaseSerializesIndependentServices(t *testing.T) {
	db := newTestDB(t)
	row := models.Sandbox{Name: "lease", Purpose: SandboxPurposeSolo}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	first, second := &SandboxService{db: db}, &SandboxService{db: db}
	unlock, err := first.lockSoloRecovery(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err = second.lockSoloRecovery(ctx, row.ID)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrent replica acquired active lease: %v", err)
	}
	unlock()
	unlock2, err := second.lockSoloRecovery(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.OperationOwner != "" || row.OperationExpiresAt != nil || row.Generation != 2 {
		t.Fatalf("%+v", row)
	}
}
