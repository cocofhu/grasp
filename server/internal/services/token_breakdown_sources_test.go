package services

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkspaceTokenBreakdownSourcesAndZero(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)

	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	// No usage anywhere → null (not 0).
	if bd := s.TokenBreakdown(); bd.Total != nil || bd.Workflow != nil {
		t.Fatalf("no usage: want null tokens, got %+v", bd)
	}

	mustCreate(&models.WorkflowDef{ID: "wf-partial", Name: "w", Version: 1})
	mustCreate(&models.Run{ID: "run-partial", WorkflowID: "wf-partial", Status: "completed"})
	mustCreate(&models.StateRun{
		RunID: "run-partial", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 100, OutputTokens: 28},
	})

	mustCreate(&models.WorkflowDef{ID: "wf-zero", Name: "w-zero", Version: 1})
	mustCreate(&models.Run{ID: "run-zero", WorkflowID: "wf-zero", Status: "cancelled"})
	mustCreate(&models.StateRun{
		RunID: "run-zero", NodeID: "n1", Status: "cancelled",
		Usage: &models.TokenUsage{},
	})

	syncTokenLedger(t, db)
	platform := s.TokenBreakdown()
	// partial=128 wf, zero=0 wf → wf=128, total=128
	if platform.Workflow == nil || *platform.Workflow != 128 {
		t.Fatalf("platform workflow=%v want 128", platform.Workflow)
	}
	if platform.Total == nil || *platform.Total != 128 {
		t.Fatalf("platform total=%v want 128", platform.Total)
	}

	seedLedger(t, db,
		models.TokenUsageEvent{Source: models.TokenLedgerSourceSolo,
			InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 40},
		models.TokenUsageEvent{Source: models.TokenLedgerSourceStudio, InputTokens: 2000},
	)
	platform = s.TokenBreakdown()
	if platform.Total == nil || *platform.Total != 2228 || platform.Workflow == nil || *platform.Workflow != 128 {
		t.Fatalf("all-source breakdown=%+v want total=2228 workflow=128", platform)
	}

	// All reported zeros → 0 (not null).
	db2, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_zero.db"))
	if err != nil {
		t.Fatal(err)
	}
	s2 := NewWorkspaceService(db2)
	if err := db2.Create(&models.WorkflowDef{ID: "wf-z", Name: "w", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db2.Create(&models.Run{ID: "run-z", WorkflowID: "wf-z", Status: "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db2.Create(&models.StateRun{
		RunID: "run-z", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{},
	}).Error; err != nil {
		t.Fatal(err)
	}
	syncTokenLedger(t, db2)
	got := s2.TokenBreakdown()
	if got.Total == nil || *got.Total != 0 {
		t.Fatalf("reported zero total=%v want 0", got.Total)
	}
	if got.Workflow == nil || *got.Workflow != 0 {
		t.Fatalf("reported zero workflow=%v want 0", got.Workflow)
	}
}
