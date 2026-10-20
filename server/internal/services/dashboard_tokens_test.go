package services

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkspaceTokenBreakdownMatchesDashboard(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	dash := NewDashboardService(db, s)

	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	// No usage anywhere → null (not 0).
	st := dash.Compute()
	if st.TotalTokens != nil || st.WorkflowTokens != nil || st.PMTokens != nil {
		t.Fatalf("no usage: want null tokens, got total=%v wf=%v pm=%v",
			st.TotalTokens, st.WorkflowTokens, st.PMTokens)
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

	mustCreate(&models.ChatThread{ID: "th-pm", UserID: "u1", Title: "t"})
	mustCreate(&models.ChatMessage{
		ID: "msg-pm", ThreadID: "th-pm", Role: "assistant", Content: "hi",
		Status: "ok", CreatedAt: time.Now(),
		Usage: &models.TokenUsage{InputTokens: 10, OutputTokens: 5},
	})

	syncTokenLedger(t, db)
	platform := s.TokenBreakdown()
	// partial=128 wf, zero=0 wf, pm=15 → wf=128, pm=15, total=143
	if platform.Workflow == nil || *platform.Workflow != 128 {
		t.Fatalf("platform workflow=%v want 128", platform.Workflow)
	}
	if platform.PM == nil || *platform.PM != 15 {
		t.Fatalf("platform pm=%v want 15", platform.PM)
	}
	if platform.Total == nil || *platform.Total != 143 {
		t.Fatalf("platform total=%v want 143", platform.Total)
	}

	st = dash.Compute()
	if st.TotalTokens == nil || *st.TotalTokens != 143 {
		t.Fatalf("dashboard totalTokens=%v want 143", st.TotalTokens)
	}
	if st.WorkflowTokens == nil || *st.WorkflowTokens != 128 {
		t.Fatalf("dashboard workflowTokens=%v want 128", st.WorkflowTokens)
	}
	if st.PMTokens == nil || *st.PMTokens != 15 {
		t.Fatalf("dashboard pmTokens=%v want 15", st.PMTokens)
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
	got := NewDashboardService(db2, s2).Compute()
	if got.TotalTokens == nil || *got.TotalTokens != 0 {
		t.Fatalf("reported zero total=%v want 0", got.TotalTokens)
	}
	if got.WorkflowTokens == nil || *got.WorkflowTokens != 0 {
		t.Fatalf("reported zero workflow=%v want 0", got.WorkflowTokens)
	}
}
