package tokenledger_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
)

func sumRows(rows []models.TokenUsageEvent) int64 {
	var n int64
	for _, r := range rows {
		n += r.Total()
	}
	return n
}

func TestRowsAttributesUncoveredRemainderToUnknown(t *testing.T) {
	rows := tokenledger.Rows(tokenledger.Entry{
		Source: models.TokenLedgerSourceWorkflow,
		Usage:  &models.TokenUsage{InputTokens: 100, OutputTokens: 40},
		ByModel: models.TokenUsageByModel{
			"gpt-x": {InputTokens: 70, OutputTokens: 40, Source: models.TokenUsageSourceUpstream},
		},
	})
	if len(rows) != 2 {
		t.Fatalf("rows=%d want 2: %+v", len(rows), rows)
	}
	if sumRows(rows) != 140 {
		t.Fatalf("sum=%d want 140", sumRows(rows))
	}
	for _, r := range rows {
		if r.ModelKey == models.TokenUsageModelUnknown && r.InputTokens != 30 {
			t.Fatalf("unknown remainder input=%d want 30", r.InputTokens)
		}
		if r.Status != models.TokenLedgerStatusOK {
			t.Fatalf("default status=%q", r.Status)
		}
	}
}

func TestRowsLegacyFlatUsageAndExplicitZero(t *testing.T) {
	rows := tokenledger.Rows(tokenledger.Entry{Usage: &models.TokenUsage{InputTokens: 5}})
	if len(rows) != 1 || rows[0].ModelKey != models.TokenUsageModelUnknown || rows[0].Total() != 5 {
		t.Fatalf("legacy rows=%+v", rows)
	}
	zero := tokenledger.Rows(tokenledger.Entry{Usage: &models.TokenUsage{}})
	if len(zero) != 1 || zero[0].Total() != 0 {
		t.Fatalf("explicit zero should keep one marker row, got %+v", zero)
	}
	if got := tokenledger.Rows(tokenledger.Entry{}); len(got) != 0 {
		t.Fatalf("nil usage should produce no rows, got %+v", got)
	}
}

func TestRecordResolvesRunWorkflowAndProject(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ledger_record.db"))
	if err != nil {
		t.Fatal(err)
	}
	must := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	must(&models.Project{ID: "p1", Name: "Alpha"})
	must(&models.WorkflowDef{ID: "wf1", ProjectID: "p1", Name: "main", Status: "draft", Version: 1})
	must(&models.Run{ID: "r1", WorkflowID: "wf1", WorkflowName: "main", Title: "Run One", Status: "running"})

	tokenledger.Record(db, tokenledger.Entry{
		Source: models.TokenLedgerSourceWorkflow, RunID: "r1", NodeID: "n1", NodeType: "agent",
		Status: models.TokenLedgerStatusFailed,
		Usage:  &models.TokenUsage{InputTokens: 10, OutputTokens: 2},
	})
	var rows []models.TokenUsageEvent
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	r := rows[0]
	if r.ProjectID != "p1" || r.ProjectName != "Alpha" || r.WorkflowID != "wf1" || r.RunTitle != "Run One" {
		t.Fatalf("metadata not resolved: %+v", r)
	}
	if r.Status != models.TokenLedgerStatusFailed || r.Total() != 12 {
		t.Fatalf("row=%+v", r)
	}
}

func TestRecordResolvesThreadProject(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ledger_thread.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Project{ID: "p2", Name: "Beta"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ChatThread{ID: "th1", ProjectID: "p2", UserID: "u", Title: "t"}).Error; err != nil {
		t.Fatal(err)
	}
	tokenledger.Record(db, tokenledger.Entry{Source: models.TokenLedgerSourcePM, ThreadID: "th1",
		Status: models.TokenLedgerStatusCancelled, Usage: &models.TokenUsage{OutputTokens: 9}})
	var r models.TokenUsageEvent
	if err := db.First(&r).Error; err != nil {
		t.Fatal(err)
	}
	if r.ProjectID != "p2" || r.ProjectName != "Beta" || r.Status != models.TokenLedgerStatusCancelled {
		t.Fatalf("row=%+v", r)
	}
}

func TestBackfillIsIdempotentAndKeepsLiveRows(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ledger_backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	must := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	ts := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	must(&models.Project{ID: "p1", Name: "Alpha"})
	must(&models.WorkflowDef{ID: "wf1", ProjectID: "p1", Name: "main", Status: "draft", Version: 1})
	must(&models.Run{ID: "r1", WorkflowID: "wf1", WorkflowName: "main", Status: "completed", StartedAt: ts})
	must(&models.StateRun{RunID: "r1", NodeID: "n1", NodeType: "agent", Status: "failed",
		Usage:        &models.TokenUsage{InputTokens: 50},
		UsageByModel: models.TokenUsageByModel{"m1": {InputTokens: 50}}})
	// Orphan run: workflow deleted → still imported, project unassigned.
	must(&models.Run{ID: "r-orphan", WorkflowID: "wf-gone", WorkflowName: "gone", Status: "completed", StartedAt: ts})
	must(&models.StateRun{RunID: "r-orphan", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{OutputTokens: 7}})
	must(&models.ChatThread{ID: "th1", ProjectID: "p1", UserID: "u", Title: "t"})
	must(&models.ChatMessage{ID: "m1", ThreadID: "th1", Role: "assistant", Content: "x", Status: "ok",
		CreatedAt: ts, Usage: &models.TokenUsage{InputTokens: 3}})

	tokenledger.Record(db, tokenledger.Entry{Source: models.TokenLedgerSourceStudio, ProjectID: "p1", Usage: &models.TokenUsage{InputTokens: 1000}})

	for i := 0; i < 2; i++ {
		if err := tokenledger.Backfill(db); err != nil {
			t.Fatal(err)
		}
	}
	var rows []models.TokenUsageEvent
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if got := sumRows(rows); got != 1060 {
		t.Fatalf("sum after double backfill=%d want 1060 (rows=%+v)", got, rows)
	}
	var orphan, failed bool
	for _, r := range rows {
		if r.RunID == "r-orphan" && r.ProjectID == "" && r.WorkflowName == "gone" {
			orphan = true
		}
		if r.RunID == "r1" && r.Status == models.TokenLedgerStatusFailed && r.ModelKey == "m1" && r.ProjectName == "Alpha" {
			failed = true
		}
	}
	if !orphan || !failed {
		t.Fatalf("orphan=%v failed=%v rows=%+v", orphan, failed, rows)
	}
}

func TestBackfillOnceRunsOnlyOnce(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ledger_once.db"))
	if err != nil {
		t.Fatal(err)
	}
	// The template DB already ran tokenledger.BackfillOnce during migrate → marker present.
	must := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	ts := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	must(&models.Run{ID: "r1", WorkflowID: "wf", Status: "completed", StartedAt: ts})
	must(&models.StateRun{RunID: "r1", NodeID: "n1", Status: "completed", Usage: &models.TokenUsage{InputTokens: 5}})
	tokenledger.BackfillOnce(db)
	var n int64
	db.Model(&models.TokenUsageEvent{}).Count(&n)
	if n != 0 {
		t.Fatalf("tokenledger.BackfillOnce re-ran after marker: %d rows", n)
	}
	db.Where(&models.Setting{Key: tokenledger.BackfillMarkerKey}).Delete(&models.Setting{})
	tokenledger.BackfillOnce(db)
	db.Model(&models.TokenUsageEvent{}).Count(&n)
	if n != 1 {
		t.Fatalf("tokenledger.BackfillOnce without marker rows=%d want 1", n)
	}
}
