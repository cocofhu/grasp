package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRunSandboxSourceDoesNotAdoptAnotherAttempt(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	first := runtimeInfo("pending-first")
	first.RunID, first.NodeID, first.Iteration = "same-run", "same-node", 3
	first.StateRunID, first.RunAttempt = 41, 1
	s.BeginRunSandbox(first)
	second := first
	second.Name, second.StateRunID, second.RunAttempt = "pending-second", 42, 2
	s.BeginRunSandbox(second)
	// No correlation name: the exact persisted attempt still separates the
	// concurrent placeholders. A later attempt cannot adopt the first one's row.
	first.Name = "physical-first"
	s.RegisterRunSandbox(first)
	var retained, other models.Sandbox
	if err := db.Where("name = ?", first.Name).First(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", second.Name).First(&other).Error; err != nil {
		t.Fatal(err)
	}
	if retained.StateRunID != 41 || retained.RunAttempt != 1 || retained.ProviderAttempt != 0 || other.StateRunID != 42 || other.RunAttempt != 2 || other.Status != "creating" {
		t.Fatalf("wrong attempt adopted: first=%+v second=%+v", retained, other)
	}
}

func TestRunSandboxReadyAndSharedSourceUseExactVisit(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	info := runtimeInfo("shared-source")
	info.RunID, info.NodeID, info.Iteration, info.RunAttempt, info.Shared = "source-run", "work", 2, 1, true
	old := models.StateRun{RunID: info.RunID, NodeID: info.NodeID, Iteration: 2, Attempt: 1, Status: "running"}
	newer := models.StateRun{RunID: info.RunID, NodeID: info.NodeID, Iteration: 3, Attempt: 2, Status: "running"}
	for _, row := range []*models.StateRun{&old, &newer} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	info.StateRunID = old.ID
	s.RegisterRunSandbox(info)
	if err := db.First(&old, old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&newer, newer.ID).Error; err != nil {
		t.Fatal(err)
	}
	if old.SandboxReadyAt == nil || newer.SandboxReadyAt != nil {
		t.Fatal("registration stamped the latest visit instead of its exact source")
	}
	info.Iteration, info.RunAttempt, info.StateRunID = 3, 2, newer.ID
	s.RegisterRunSandbox(info)
	var source models.Sandbox
	if err := db.Where("name = ?", info.Name).First(&source).Error; err != nil {
		t.Fatal(err)
	}
	if source.StateRunID != newer.ID || source.Iteration != 3 || source.RunAttempt != 2 {
		t.Fatalf("shared workspace did not move to its real current source: %+v", source)
	}
	// A mismatched explicit source must not stamp a different visit.
	if err := db.Model(&newer).Update("sandbox_ready_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	info.RunAttempt = 99
	s.RegisterRunSandbox(info)
	newer.SandboxReadyAt = nil
	if err := db.First(&newer, newer.ID).Error; err != nil {
		t.Fatal(err)
	}
	if newer.SandboxReadyAt != nil {
		t.Fatal("mismatched run attempt stamped a visit")
	}
}
