package services

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
)

// Deleting a run, its workflow or a Solo session removes their records, never
// the usage they consumed nor the names it was recorded under.
func TestTokenUsageOutlivesDeletedRunsWorkflowsAndSoloSessions(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	ctx := context.Background()
	wf := NewWorkflowService(db)
	if err := wf.Save(&models.WorkflowDef{ID: "wf-gone", Name: "Gone workflow", Graph: validGraph()}); err != nil {
		t.Fatal(err)
	}
	for _, run := range []models.Run{
		{ID: "run-deleted", WorkflowID: "wf-gone", WorkflowName: "Gone workflow", Title: "Deleted run", Status: "completed"},
		{ID: "run-cascaded", WorkflowID: "wf-gone", WorkflowName: "Gone workflow", Title: "Cascaded run", Status: "completed"},
	} {
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.SoloSession{ID: "solo-gone", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	usage := func(input int64) models.TokenUsageByModel {
		return models.TokenUsageByModel{"m": {InputTokens: input, Source: models.TokenUsageSourceUpstream}}
	}
	for _, e := range []tokenledger.Entry{
		{ID: "op-1", At: at, Source: models.TokenLedgerSourceWorkflow, RunID: "run-deleted", NodeID: "n", ByModel: usage(10)},
		{ID: "op-2", At: at, Source: models.TokenLedgerSourceWorkflow, RunID: "run-cascaded", NodeID: "n", ByModel: usage(20)},
		{ID: "op-3", At: at, Source: models.TokenLedgerSourceSolo, Phase: models.TokenLedgerPhaseChat, ThreadID: "solo-gone", ByModel: usage(30)},
	} {
		tokenledger.Record(db, e)
	}
	ws := NewWorkspaceService(db)
	stats := func() GlobalTokenStatsKPI {
		t.Helper()
		out, err := ws.GlobalTokenStats(ctx, GlobalTokenStatsQuery{Window: TokenStatsWindowAll, Timezone: "UTC", Now: at.Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		return out.KPI
	}
	before := stats()
	if before.Total != 60 || before.WorkflowTotal != 30 || before.SoloTotal != 30 {
		t.Fatalf("seeded stats = %+v", before)
	}

	if err := NewRunService(db).Delete("run-deleted"); err != nil {
		t.Fatal(err)
	}
	if err := wf.Delete("wf-gone"); err != nil {
		t.Fatal(err)
	}
	if err := solo.Delete(ctx, "solo-gone", "alice"); err != nil {
		t.Fatal(err)
	}
	var runs, sessions int64
	db.Model(&models.Run{}).Count(&runs)
	db.Model(&models.SoloSession{}).Count(&sessions)
	if runs != 0 || sessions != 0 {
		t.Fatalf("records not deleted: runs=%d sessions=%d", runs, sessions)
	}

	if after := stats(); after.Total != before.Total || after.WorkflowTotal != before.WorkflowTotal || after.SoloTotal != before.SoloTotal || after.RunCount != before.RunCount {
		t.Fatalf("stats changed after deletion: before=%+v after=%+v", before, after)
	}
	events, err := ws.TokenUsageEvents(ctx, TokenUsageEventsQuery{GlobalTokenStatsQuery: GlobalTokenStatsQuery{Window: TokenStatsWindowAll, Timezone: "UTC", Now: at.Add(time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, e := range events.Items {
		if e.RunID != "" {
			if e.WorkflowID != "wf-gone" || e.WorkflowName != "Gone workflow" {
				t.Fatalf("workflow snapshot lost: %+v", e)
			}
			titles[e.RunID] = e.RunTitle
		}
	}
	if len(events.Items) != 3 || titles["run-deleted"] != "Deleted run" || titles["run-cascaded"] != "Cascaded run" {
		t.Fatalf("events after deletion = %+v", events.Items)
	}
}
