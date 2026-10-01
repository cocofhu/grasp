package services

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func int64Ptr(v int64) *int64 { return &v }

func TestAggregatePlatformTokenBreakdown(t *testing.T) {
	t.Run("all_unreported_nil", func(t *testing.T) {
		got := AggregatePlatformTokenBreakdown(map[string]ProjectTokenBreakdown{
			"a": {},
			"b": {},
		})
		if got.Total != nil || got.Workflow != nil || got.PM != nil {
			t.Fatalf("want all nil, got %+v", got)
		}
	})

	t.Run("empty_map_nil", func(t *testing.T) {
		got := AggregatePlatformTokenBreakdown(nil)
		if got.Total != nil || got.Workflow != nil || got.PM != nil {
			t.Fatalf("want all nil, got %+v", got)
		}
	})

	t.Run("partial_projects_sum", func(t *testing.T) {
		got := AggregatePlatformTokenBreakdown(map[string]ProjectTokenBreakdown{
			"none": {},
			"wf":   {Workflow: int64Ptr(100), Total: int64Ptr(100)},
			"pm":   {PM: int64Ptr(40), Total: int64Ptr(40)},
			"both": {Workflow: int64Ptr(10), PM: int64Ptr(5), Total: int64Ptr(15)},
		})
		if got.Workflow == nil || *got.Workflow != 110 {
			t.Fatalf("workflow=%v want 110", got.Workflow)
		}
		if got.PM == nil || *got.PM != 45 {
			t.Fatalf("pm=%v want 45", got.PM)
		}
		if got.Total == nil || *got.Total != 155 {
			t.Fatalf("total=%v want 155", got.Total)
		}
	})

	t.Run("reported_zero", func(t *testing.T) {
		got := AggregatePlatformTokenBreakdown(map[string]ProjectTokenBreakdown{
			"z": {Workflow: int64Ptr(0), PM: int64Ptr(0), Total: int64Ptr(0)},
		})
		if got.Workflow == nil || *got.Workflow != 0 {
			t.Fatalf("workflow=%v want 0", got.Workflow)
		}
		if got.PM == nil || *got.PM != 0 {
			t.Fatalf("pm=%v want 0", got.PM)
		}
		if got.Total == nil || *got.Total != 0 {
			t.Fatalf("total=%v want 0", got.Total)
		}
	})

	t.Run("one_side_only", func(t *testing.T) {
		got := AggregatePlatformTokenBreakdown(map[string]ProjectTokenBreakdown{
			"a": {Workflow: int64Ptr(7), Total: int64Ptr(7)},
			"b": {},
		})
		if got.Workflow == nil || *got.Workflow != 7 {
			t.Fatalf("workflow=%v want 7", got.Workflow)
		}
		if got.PM != nil {
			t.Fatalf("pm should be nil, got %v", got.PM)
		}
		if got.Total == nil || *got.Total != 7 {
			t.Fatalf("total=%v want 7", got.Total)
		}
	})
}

func TestPlatformTokenBreakdown_matchesPerProjectSum(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	dash := NewDashboardService(db, s)

	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	// No usage anywhere → null (not 0).
	_, err = s.Create("EmptyA", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Create("EmptyB", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := dash.Compute()
	if st.TotalTokens != nil || st.WorkflowTokens != nil || st.PMTokens != nil {
		t.Fatalf("no usage: want null tokens, got total=%v wf=%v pm=%v",
			st.TotalTokens, st.WorkflowTokens, st.PMTokens)
	}

	partial, err := s.Create("Partial", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := s.Create("Zero", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pmOnly, err := s.Create("PMOnly", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	mustCreate(&models.WorkflowDef{ID: "wf-partial", ProjectID: partial.ID, Name: "w", Status: "draft", Version: 1})
	mustCreate(&models.Run{ID: "run-partial", WorkflowID: "wf-partial", Status: "completed"})
	mustCreate(&models.StateRun{
		RunID: "run-partial", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 100, OutputTokens: 28},
	})

	mustCreate(&models.WorkflowDef{ID: "wf-zero", ProjectID: zero.ID, Name: "w", Status: "draft", Version: 1})
	mustCreate(&models.Run{ID: "run-zero", WorkflowID: "wf-zero", Status: "cancelled"})
	mustCreate(&models.StateRun{
		RunID: "run-zero", NodeID: "n1", Status: "cancelled",
		Usage: &models.TokenUsage{},
	})

	mustCreate(&models.ChatThread{ID: "th-pm", ProjectID: pmOnly.ID, UserID: "u1", Title: "t"})
	mustCreate(&models.ChatMessage{
		ID: "msg-pm", ThreadID: "th-pm", Role: "assistant", Content: "hi",
		Status: "ok", CreatedAt: time.Now(),
		Usage: &models.TokenUsage{InputTokens: 10, OutputTokens: 5},
	})

	syncTokenLedger(t, db)
	platform := s.PlatformTokenBreakdown()
	// per-project: partial=128 wf, zero=0 wf, pmOnly=15 pm → platform wf=128, pm=15, total=143
	if platform.Workflow == nil || *platform.Workflow != 128 {
		t.Fatalf("platform workflow=%v want 128", platform.Workflow)
	}
	if platform.PM == nil || *platform.PM != 15 {
		t.Fatalf("platform pm=%v want 15", platform.PM)
	}
	if platform.Total == nil || *platform.Total != 143 {
		t.Fatalf("platform total=%v want 143", platform.Total)
	}

	// Same data: sum of each project's totalTokens equals platform total.
	ids := []string{partial.ID, zero.ID, pmOnly.ID}
	syncTokenLedger(t, db)
	bd := s.TokenBreakdownByProjectIDs(ids)
	var sum int64
	for _, id := range ids {
		if bd[id].Total != nil {
			sum += *bd[id].Total
		}
	}
	if platform.Total == nil || *platform.Total != sum {
		t.Fatalf("platform total %v != per-project sum %d", platform.Total, sum)
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
	s2 := NewProjectService(db2)
	z, err := s2.Create("OnlyZero", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db2.Create(&models.WorkflowDef{ID: "wf-z", ProjectID: z.ID, Name: "w", Status: "draft", Version: 1}).Error; err != nil {
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
