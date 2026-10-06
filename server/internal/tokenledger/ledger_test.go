package tokenledger_test

import (
	"path/filepath"
	"testing"

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
	must(&models.WorkflowDef{ID: "wf1", ProjectID: "p1", Name: "main", Version: 1})
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
