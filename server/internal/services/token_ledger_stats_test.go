package services

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func seedLedger(t *testing.T, db *gorm.DB, rows ...models.TokenUsageEvent) {
	t.Helper()
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
}

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestGlobalTokenStatsLedgerDimensionsAndCost(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ledger_dims.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	live, err := s.Create("Live", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) // Saturday
	at := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)  // Friday 09:xx UTC
	seedLedger(t, db,
		models.TokenUsageEvent{CreatedAt: at, Source: "workflow", Phase: "production", Status: "ok",
			ProjectID: live.ID, ProjectName: "Live", WorkflowID: "wf1", WorkflowName: "main",
			RunID: "r1", RunTitle: "Run 1", NodeID: "n1", NodeType: "agent", ModelKey: "m-priced",
			InputTokens: 1_000_000, OutputTokens: 500_000},
		models.TokenUsageEvent{CreatedAt: at, Source: "workflow", Phase: "interactive", Status: "failed",
			ProjectID: live.ID, ProjectName: "Live", WorkflowID: "wf1", WorkflowName: "main",
			RunID: "r1", NodeID: "n2", NodeType: "clarify", ModelKey: "m-free", InputTokens: 100},
		models.TokenUsageEvent{CreatedAt: at, Source: "studio", Phase: "chat", Status: "ok",
			ProjectID: live.ID, ProjectName: "Live", SandboxID: 3, ModelKey: "m-priced", CacheReadTokens: 1_000_000},
		models.TokenUsageEvent{CreatedAt: at, Source: "pm", Phase: "chat", Status: "cancelled",
			ProjectID: "p-deleted", ProjectName: "Gone", ThreadID: "th1", ModelKey: "m-priced", OutputTokens: 10},
	)
	if _, err := s.SaveTokenPricing(TokenPricing{Currency: "usd", Models: map[string]TokenModelPrice{
		"m-priced": {Input: 2, Output: 10, CacheRead: 0.5},
	}}); err != nil {
		t.Fatal(err)
	}

	res, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{Window: TokenStatsWindow7d, Timezone: "UTC", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	k := res.KPI
	if k.Total != 2_500_110 || k.StudioTotal != 1_000_000 || k.PmTotal != 10 || k.FailedTotal != 110 {
		t.Fatalf("kpi=%+v", k)
	}
	// 1M in*2 + 0.5M out*10 + 1M cacheRead*0.5 + 10 out*10/1e6
	wantCost := 2.0 + 5.0 + 0.5 + 0.0001
	if !approxEqual(k.Cost, wantCost) || res.Currency != "USD" {
		t.Fatalf("cost=%v currency=%s want %v USD", k.Cost, res.Currency, wantCost)
	}
	if k.UnpricedTotal != 100 || len(res.UnpricedModels) != 1 || res.UnpricedModels[0] != "m-free" {
		t.Fatalf("unpriced total=%d models=%v", k.UnpricedTotal, res.UnpricedModels)
	}
	if !approxEqual(k.CacheHitRate, 1_000_000.0/2_000_100.0) {
		t.Fatalf("cacheHitRate=%v", k.CacheHitRate)
	}
	if k.RunCount != 1 || k.ThreadCount != 1 || k.EventCount != 4 {
		t.Fatalf("counts run=%d thread=%d events=%d", k.RunCount, k.ThreadCount, k.EventCount)
	}

	byKey := func(list []GlobalTokenStatsNamedBucket) map[string]int64 {
		m := map[string]int64{}
		for _, b := range list {
			m[b.Key] = b.Total
		}
		return m
	}
	if src := byKey(res.Sources); src["studio"] != 1_000_000 || src["pm"] != 10 || src["workflow"] != 1_500_100 {
		t.Fatalf("sources=%v", src)
	}
	if st := byKey(res.Statuses); st["failed"] != 100 || st["cancelled"] != 10 {
		t.Fatalf("statuses=%v", st)
	}
	if ph := byKey(res.Phases); ph["interactive"] != 100 || ph["chat"] != 1_000_010 {
		t.Fatalf("phases=%v", ph)
	}
	if res.WeekHour[4][9] != k.Total {
		t.Fatalf("weekHour[Fri][9]=%d want %d", res.WeekHour[4][9], k.Total)
	}

	var deleted *GlobalTokenStatsProjectRow
	for i := range res.Projects {
		if res.Projects[i].ProjectID == "p-deleted" {
			deleted = &res.Projects[i]
		}
	}
	if deleted == nil || !deleted.Deleted || deleted.Name != "Gone" {
		t.Fatalf("deleted project row=%+v", deleted)
	}
	if len(res.TopRuns) != 1 || res.TopRuns[0].Status != "failed" || res.TopRuns[0].NodeCount != 2 || res.TopRuns[0].ModelKey != "m-priced" {
		t.Fatalf("topRuns=%+v", res.TopRuns)
	}
	if len(res.Tree) != 2 || res.Tree[0].Key != live.ID || len(res.Tree[0].Children) != 2 {
		t.Fatalf("tree=%+v", res.Tree)
	}

	studio, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		Window: TokenStatsWindow7d, Timezone: "UTC", Now: now, Source: GlobalTokenStatsSourceStudio,
	})
	if err != nil {
		t.Fatal(err)
	}
	if studio.KPI.Total != 1_000_000 {
		t.Fatalf("studio filter total=%d", studio.KPI.Total)
	}
	failed, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		Window: TokenStatsWindow7d, Timezone: "UTC", Now: now, Status: "failed", NodeType: "clarify",
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.KPI.Total != 100 {
		t.Fatalf("status+nodeType filter total=%d", failed.KPI.Total)
	}
}

func TestGlobalTokenStatsCustomRangeAndGranularity(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ledger_range.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	seedLedger(t, db,
		models.TokenUsageEvent{CreatedAt: time.Date(2026, 7, 10, 3, 0, 0, 0, time.UTC), Source: "workflow", Status: "ok", ModelKey: "m", InputTokens: 10},
		models.TokenUsageEvent{CreatedAt: time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC), Source: "workflow", Status: "ok", ModelKey: "m", InputTokens: 20},
		models.TokenUsageEvent{CreatedAt: time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC), Source: "workflow", Status: "ok", ModelKey: "m", InputTokens: 5},
	)
	res, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		From: "2026-07-10", To: "2026-07-12", Timezone: "UTC", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Window != TokenStatsWindowCustom || res.BucketWidth != TokenStatsBucketDay {
		t.Fatalf("window=%s bw=%s", res.Window, res.BucketWidth)
	}
	if res.KPI.Total != 30 || len(res.Trend) != 3 {
		t.Fatalf("total=%d trend=%d", res.KPI.Total, len(res.Trend))
	}
	if res.KPI.PrevTotal == nil || *res.KPI.PrevTotal != 5 {
		t.Fatalf("prevTotal=%v want 5", res.KPI.PrevTotal)
	}

	hourly, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		From: "2026-07-10", To: "2026-07-12", Granularity: TokenStatsBucketHour, Timezone: "UTC", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hourly.BucketWidth != TokenStatsBucketHour || len(hourly.Trend) != 72 {
		t.Fatalf("hourly bw=%s len=%d", hourly.BucketWidth, len(hourly.Trend))
	}
	allHour, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		Window: TokenStatsWindowAll, Granularity: TokenStatsBucketHour, Timezone: "UTC", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if allHour.BucketWidth != TokenStatsBucketWeek {
		t.Fatalf("hour over unbounded window must fall back to week, got %s", allHour.BucketWidth)
	}

	if _, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{From: "2026-07-12", To: "2026-07-10", Timezone: "UTC", Now: now}); !errors.Is(err, ErrInvalidTokenStatsRange) {
		t.Fatalf("reversed range err=%v", err)
	}
	if _, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{To: "2026-07-10", Timezone: "UTC", Now: now}); !errors.Is(err, ErrInvalidTokenStatsRange) {
		t.Fatalf("to without from err=%v", err)
	}
	if _, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{Granularity: "minute", Timezone: "UTC", Now: now}); !errors.Is(err, ErrInvalidTokenStatsWindow) {
		t.Fatalf("bad granularity err=%v", err)
	}
}

func TestTokenUsageEventsPagingAndFilters(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ledger_events.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var rows []models.TokenUsageEvent
	for i := 0; i < 5; i++ {
		rows = append(rows, models.TokenUsageEvent{
			CreatedAt: now.Add(-time.Duration(i) * time.Hour), Source: "workflow", Status: "ok",
			ProjectID: "p1", RunID: "r1", ModelKey: "m", InputTokens: int64(10 * (i + 1)),
		})
	}
	rows = append(rows, models.TokenUsageEvent{CreatedAt: now, Source: "pm", Status: "ok", ProjectID: "p2", ModelKey: "m", InputTokens: 999})
	seedLedger(t, db, rows...)

	page, err := s.TokenUsageEvents(context.Background(), TokenUsageEventsQuery{
		GlobalTokenStatsQuery: GlobalTokenStatsQuery{Window: TokenStatsWindow24h, Timezone: "UTC", Now: now, RunID: "r1"},
		Page:                  2, PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Items) != 2 || page.Items[0].Total != 30 {
		t.Fatalf("page=%+v", page)
	}
	byTotal, err := s.TokenUsageEvents(context.Background(), TokenUsageEventsQuery{
		GlobalTokenStatsQuery: GlobalTokenStatsQuery{Window: TokenStatsWindow24h, Timezone: "UTC", Now: now},
		Sort:                  "total", PageSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if byTotal.Total != 6 || byTotal.Items[0].Total != 999 || byTotal.Items[0].ProjectName != globalUnassignedProjectName {
		t.Fatalf("byTotal=%+v", byTotal)
	}
}

// TestProjectBoardUsesSameAggregation locks a project onto GlobalTokenStats
// and checks KPI, four components, and every breakdown match. Studio rows
// without a project id stay on the platform total only.
func TestProjectBoardUsesSameAggregation(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "project_board_same_agg.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	p1, err := s.Create("Board", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.Create("Other", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveTokenPricing(db, TokenPricing{
		Currency: "USD",
		Models:   map[string]TokenModelPrice{"priced": {Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.2}},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	at := now.Add(-2 * time.Hour)
	seedLedger(t, db,
		models.TokenUsageEvent{CreatedAt: at, Source: "workflow", Phase: "production", Status: "ok",
			ProjectID: p1.ID, ProjectName: "Board", WorkflowID: "wf-a", WorkflowName: "main",
			RunID: "r-a", RunTitle: "Run A", NodeType: "agent", ModelKey: "priced",
			InputTokens: 100, OutputTokens: 40, CacheReadTokens: 10, CacheWriteTokens: 5},
		models.TokenUsageEvent{CreatedAt: at, Source: "pm", Phase: "interactive", Status: "failed",
			ProjectID: p1.ID, ProjectName: "Board", ThreadID: "th-1", ModelKey: "priced",
			InputTokens: 20, OutputTokens: 10},
		models.TokenUsageEvent{CreatedAt: at, Source: "studio", Phase: "chat", Status: "cancelled",
			ProjectID: p1.ID, ProjectName: "Board", RunID: "r-studio", RunTitle: "Studio", ModelKey: "priced",
			InputTokens: 8, OutputTokens: 2},
		models.TokenUsageEvent{CreatedAt: at, Source: "studio", Phase: "chat", Status: "ok",
			ProjectID: "", ProjectName: "", ModelKey: "unpriced-studio",
			InputTokens: 1000, OutputTokens: 1000},
		models.TokenUsageEvent{CreatedAt: at, Source: "workflow", Phase: "production", Status: "ok",
			ProjectID: p2.ID, ProjectName: "Other", WorkflowID: "wf-b", WorkflowName: "other",
			RunID: "r-b", RunTitle: "Run B", NodeType: "tool", ModelKey: "priced",
			InputTokens: 7},
	)

	q := GlobalTokenStatsQuery{Window: TokenStatsWindowAll, Timezone: "UTC", Now: now, ProjectID: p1.ID}
	locked, err := s.GlobalTokenStats(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.GlobalTokenStats(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if locked.KPI.Total != again.KPI.Total || locked.KPI.Cost != again.KPI.Cost {
		t.Fatalf("same query diverged: %+v vs %+v", locked.KPI, again.KPI)
	}
	// workflow 155 + pm 30 + studio 10 = 195. Unassigned studio (2000) and p2 (7) stay out.
	if locked.KPI.Total != 195 || locked.KPI.StudioTotal != 10 || locked.KPI.WorkflowTotal != 155 || locked.KPI.PmTotal != 30 {
		t.Fatalf("locked kpi=%+v", locked.KPI)
	}
	if locked.Composition.Total != locked.KPI.Total {
		t.Fatalf("composition %d != kpi %d", locked.Composition.Total, locked.KPI.Total)
	}
	parts := locked.Composition.InputTokens + locked.Composition.OutputTokens + locked.Composition.CacheReadTokens + locked.Composition.CacheWriteTokens
	if parts != locked.KPI.Total || locked.Composition.InputTokens != 128 || locked.Composition.OutputTokens != 52 {
		t.Fatalf("parts=%d composition=%+v", parts, locked.Composition)
	}
	sumNamed := func(rows []GlobalTokenStatsNamedBucket) int64 {
		var n int64
		for _, r := range rows {
			n += r.Total
		}
		return n
	}
	if sumNamed(locked.Sources) != locked.KPI.Total || sumNamed(locked.Statuses) != locked.KPI.Total || sumNamed(locked.Phases) != locked.KPI.Total {
		t.Fatalf("dimension sums sources=%d statuses=%d phases=%d kpi=%d", sumNamed(locked.Sources), sumNamed(locked.Statuses), sumNamed(locked.Phases), locked.KPI.Total)
	}
	if locked.KPI.FailedTotal != 40 { // failed 30 + cancelled 10
		t.Fatalf("failedTotal=%d", locked.KPI.FailedTotal)
	}
	if locked.KPI.UnpricedTotal != 0 {
		t.Fatalf("priced project rows should not be unpriced: %d", locked.KPI.UnpricedTotal)
	}
	for _, p := range locked.Projects {
		if p.ProjectID != p1.ID {
			t.Fatalf("locked projects leaked %s", p.ProjectID)
		}
	}

	wfOnly, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		Window: TokenStatsWindowAll, Timezone: "UTC", Now: now, ProjectID: p1.ID, Source: GlobalTokenStatsSourceWorkflow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if wfOnly.KPI.Total != 155 || wfOnly.KPI.StudioTotal != 0 || wfOnly.KPI.PmTotal != 0 {
		t.Fatalf("workflow-only kpi=%+v", wfOnly.KPI)
	}

	platform, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		Window: TokenStatsWindowAll, Timezone: "UTC", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if platform.KPI.Total != 195+2000+7 {
		t.Fatalf("platform total=%d", platform.KPI.Total)
	}
	p2stats, err := s.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
		Window: TokenStatsWindowAll, Timezone: "UTC", Now: now, ProjectID: p2.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p2stats.KPI.Total != 7 || p2stats.KPI.StudioTotal != 0 {
		t.Fatalf("other project picked up unassigned studio: %+v", p2stats.KPI)
	}
}

func TestSaveTokenPricingValidation(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "pricing.db"))
	if err != nil {
		t.Fatal(err)
	}
	if got := LoadTokenPricing(db); got.Currency != TokenPricingCurrencyUSD || len(got.Models) != 0 {
		t.Fatalf("default pricing=%+v", got)
	}
	if _, err := SaveTokenPricing(db, TokenPricing{Currency: "EUR"}); !errors.Is(err, ErrInvalidTokenPricing) {
		t.Fatalf("currency err=%v", err)
	}
	if _, err := SaveTokenPricing(db, TokenPricing{Models: map[string]TokenModelPrice{"m": {Input: -1}}}); !errors.Is(err, ErrInvalidTokenPricing) {
		t.Fatalf("negative err=%v", err)
	}
	saved, err := SaveTokenPricing(db, TokenPricing{Currency: "cny", Models: map[string]TokenModelPrice{" Kimi ": {Output: 3}, "": {Input: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Currency != "CNY" || len(saved.Models) != 1 || saved.UpdatedAt == nil {
		t.Fatalf("saved=%+v", saved)
	}
	if c, ok := saved.Cost("kimi", models.TokenUsage{OutputTokens: 2_000_000}); !ok || !approxEqual(c, 6) {
		t.Fatalf("case-insensitive cost=%v ok=%v", c, ok)
	}
}
