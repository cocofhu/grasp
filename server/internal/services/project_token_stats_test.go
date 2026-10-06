package services

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestTokenStatsAggregation(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "token_stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)

	proj, err := s.Create("TokStats", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyProj, err := s.Create("EmptyTok", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	must := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	// Fixed "now" in UTC; client tz = Asia/Shanghai (UTC+8).
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	ptr := func(tt time.Time) *time.Time { return &tt }

	must(&models.WorkflowDef{ID: "wf-a", ProjectID: proj.ID, Name: "approve-main", Version: 1})
	must(&models.WorkflowDef{ID: "wf-b", ProjectID: proj.ID, Name: "doc-review", Version: 1})
	must(&models.WorkflowDef{ID: "wf-c", ProjectID: proj.ID, Name: "misc", Version: 1})

	// Day in window (2026-07-24 local): reported usage on completed run.
	dayIn := time.Date(2026, 7, 24, 10, 0, 0, 0, loc).UTC()
	must(&models.Run{
		ID: "run-a1", WorkflowID: "wf-a", WorkflowName: "approve-main",
		Status: "completed", StartedAt: dayIn,
	})
	must(&models.StateRun{
		RunID: "run-a1", NodeID: "n1", Status: "completed",
		StartedAt: ptr(dayIn),
		Usage:     &models.TokenUsage{InputTokens: 100, OutputTokens: 20},
	})
	// Nil usage on same run must not count as 0.
	must(&models.StateRun{
		RunID: "run-a1", NodeID: "n2", Status: "completed",
		StartedAt: ptr(dayIn),
	})

	// In-progress run with reported usage — must count.
	dayToday := time.Date(2026, 7, 25, 8, 0, 0, 0, loc).UTC()
	must(&models.Run{
		ID: "run-a2", WorkflowID: "wf-a", WorkflowName: "approve-main",
		Status: "running", StartedAt: dayToday,
	})
	must(&models.StateRun{
		RunID: "run-a2", NodeID: "n1", Status: "running",
		StartedAt: ptr(dayToday),
		Usage:     &models.TokenUsage{InputTokens: 5, CacheReadTokens: 3},
	})

	// Second workflow.
	must(&models.Run{
		ID: "run-b1", WorkflowID: "wf-b", WorkflowName: "doc-review",
		Status: "completed", StartedAt: dayIn,
	})
	must(&models.StateRun{
		RunID: "run-b1", NodeID: "n1", Status: "completed",
		StartedAt: ptr(dayIn),
		Usage:     &models.TokenUsage{OutputTokens: 40, CacheWriteTokens: 10},
	})

	// Outside 7d window but inside all-history (2026-06-01).
	old := time.Date(2026, 6, 1, 12, 0, 0, 0, loc).UTC()
	must(&models.Run{
		ID: "run-c1", WorkflowID: "wf-c", WorkflowName: "misc",
		Status: "completed", StartedAt: old,
	})
	must(&models.StateRun{
		RunID: "run-c1", NodeID: "n1", Status: "completed",
		StartedAt: ptr(old),
		Usage:     &models.TokenUsage{InputTokens: 1000},
	})

	// Explicit zero report — present, totals 0 contribution but marks reported.
	must(&models.Run{
		ID: "run-zero", WorkflowID: "wf-b", WorkflowName: "doc-review",
		Status: "cancelled", StartedAt: dayToday,
	})
	must(&models.StateRun{
		RunID: "run-zero", NodeID: "n1", Status: "cancelled",
		StartedAt: ptr(dayToday),
		Usage:     &models.TokenUsage{},
	})

	t.Run("empty_project_no_forged_series", func(t *testing.T) {
		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), emptyProj.ID, TokenStatsQuery{
			Window: TokenStatsWindow30d, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Empty {
			t.Fatal("expected empty")
		}
		if len(res.Trend) != 0 {
			t.Fatalf("empty window must not forge zero trend, got %d buckets", len(res.Trend))
		}
		if res.Composition.Total != 0 || len(res.Workflows) != 0 {
			t.Fatalf("empty composition/workflows: %+v", res)
		}
		if len(res.ModelRanking) != 0 || len(res.ModelComposition) != 0 {
			t.Fatalf("empty window must not forge model rank/composition: rank=%+v comp=%+v", res.ModelRanking, res.ModelComposition)
		}
	})

	t.Run("7d_day_buckets_and_null_not_zero", func(t *testing.T) {
		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), proj.ID, TokenStatsQuery{
			Window: TokenStatsWindow7d, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Empty {
			t.Fatal("expected non-empty")
		}
		if res.BucketWidth != TokenStatsBucketDay {
			t.Fatalf("bucketWidth=%s", res.BucketWidth)
		}
		if len(res.Trend) != 7 {
			t.Fatalf("want 7 day buckets, got %d", len(res.Trend))
		}
		// 120 (a1) + 8 (a2 in-progress) + 50 (b1) + 0 (zero report) = 178; old 1000 excluded.
		if res.Composition.Total != 178 {
			t.Fatalf("composition.total=%d want 178", res.Composition.Total)
		}
		if res.Composition.InputTokens != 105 || res.Composition.OutputTokens != 60 ||
			res.Composition.CacheReadTokens != 3 || res.Composition.CacheWriteTokens != 10 {
			t.Fatalf("composition parts: %+v", res.Composition)
		}
		var day24, day25 *TokenStatsBucket
		for i := range res.Trend {
			b := &res.Trend[i]
			switch b.Bucket {
			case "2026-07-24":
				day24 = b
			case "2026-07-25":
				day25 = b
			}
		}
		if day24 == nil || day24.Total != 170 {
			t.Fatalf("2026-07-24 = %+v want total 170", day24)
		}
		if day25 == nil || day25.Total != 8 {
			t.Fatalf("2026-07-25 = %+v want total 8 (in-progress counted)", day25)
		}
	})

	t.Run("all_matches_TotalTokens_and_weekly", func(t *testing.T) {
		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), proj.ID, TokenStatsQuery{
			Window: TokenStatsWindowAll, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.BucketWidth != TokenStatsBucketWeek {
			t.Fatalf("bucketWidth=%s", res.BucketWidth)
		}
		syncTokenLedger(t, db)
		tt := s.totalTokens(proj.ID)
		if tt == nil {
			t.Fatal("TotalTokens nil")
		}
		if res.Composition.Total != *tt {
			t.Fatalf("window=all composition.total=%d TotalTokens=%d", res.Composition.Total, *tt)
		}
		// 178 + 1000 = 1178
		if res.Composition.Total != 1178 {
			t.Fatalf("all total=%d want 1178", res.Composition.Total)
		}
		if len(res.Trend) == 0 {
			t.Fatal("all trend should have week buckets")
		}
	})

	t.Run("top10_plus_other_sums_to_total", func(t *testing.T) {
		// Seed 12 workflows with distinct totals so other appears.
		extraProj, err := s.Create("TopN", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		base := time.Date(2026, 7, 20, 9, 0, 0, 0, loc).UTC()
		var sum int64
		for i := 1; i <= 12; i++ {
			wfID := "wf-top-" + itoa(i)
			must(&models.WorkflowDef{ID: wfID, ProjectID: extraProj.ID, Name: "w" + itoa(i), Version: 1})
			runID := "run-top-" + itoa(i)
			tokens := int64(i * 100)
			sum += tokens
			must(&models.Run{
				ID: runID, WorkflowID: wfID, WorkflowName: "w" + itoa(i),
				Status: "completed", StartedAt: base,
			})
			must(&models.StateRun{
				RunID: runID, NodeID: "n1", Status: "completed",
				StartedAt: ptr(base),
				Usage:     &models.TokenUsage{InputTokens: tokens},
			})
		}
		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), extraProj.ID, TokenStatsQuery{
			Window: TokenStatsWindow30d, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Composition.Total != sum {
			t.Fatalf("total=%d want %d", res.Composition.Total, sum)
		}
		if len(res.Workflows) != 11 { // Top10 + other
			t.Fatalf("workflows len=%d want 11", len(res.Workflows))
		}
		var gotSum int64
		var otherCount int
		for _, w := range res.Workflows {
			gotSum += w.Total
			if w.Other {
				otherCount++
			}
		}
		if otherCount != 1 {
			t.Fatalf("other count=%d", otherCount)
		}
		if gotSum != sum {
			t.Fatalf("Top10+other=%d want %d", gotSum, sum)
		}
		// Highest should be w12 = 1200
		if res.Workflows[0].Total != 1200 || res.Workflows[0].Other {
			t.Fatalf("top1=%+v", res.Workflows[0])
		}
		// Other = w1+w2 = 100+200 = 300
		last := res.Workflows[len(res.Workflows)-1]
		if !last.Other || last.Total != 300 {
			t.Fatalf("other=%+v want 300", last)
		}
	})

	t.Run("started_at_fallback_to_run", func(t *testing.T) {
		p, err := s.Create("Fallback", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ts := time.Date(2026, 7, 22, 15, 0, 0, 0, loc).UTC()
		must(&models.WorkflowDef{ID: "wf-fb", ProjectID: p.ID, Name: "fb", Version: 1})
		must(&models.Run{
			ID: "run-fb", WorkflowID: "wf-fb", WorkflowName: "fb",
			Status: "completed", StartedAt: ts,
		})
		// StateRun without StartedAt → use Run.StartedAt
		must(&models.StateRun{
			RunID: "run-fb", NodeID: "n1", Status: "completed",
			Usage: &models.TokenUsage{InputTokens: 42},
		})
		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), p.ID, TokenStatsQuery{
			Window: TokenStatsWindow7d, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Composition.Total != 42 {
			t.Fatalf("total=%d", res.Composition.Total)
		}
		found := false
		for _, b := range res.Trend {
			if b.Bucket == "2026-07-22" && b.Total == 42 {
				found = true
			}
		}
		if !found {
			t.Fatalf("fallback bucket missing: %+v", res.Trend)
		}
	})

	t.Run("invalid_window", func(t *testing.T) {
		syncTokenLedger(t, db)
		_, err := s.TokenStats(context.Background(), proj.ID, TokenStatsQuery{
			Window: "1y", Timezone: "UTC", Now: now,
		})
		if err != ErrInvalidTokenStatsWindow {
			t.Fatalf("err=%v", err)
		}
		syncTokenLedger(t, db)
		_, err = s.TokenStats(context.Background(), proj.ID, TokenStatsQuery{
			Window: "1d", Timezone: "UTC", Now: now,
		})
		if err != ErrInvalidTokenStatsWindow {
			t.Fatalf("1d err=%v", err)
		}
	})

	t.Run("24h_rolling_hour_buckets_excludes_older", func(t *testing.T) {
		// now = 2026-07-25 20:00 Shanghai; 24h start = 2026-07-24 20:00.
		// dayIn (07-24 10:00) is outside; dayToday (07-25 08:00) is inside.
		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), proj.ID, TokenStatsQuery{
			Window: TokenStatsWindow24h, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Empty {
			t.Fatal("expected non-empty 24h window")
		}
		if res.Window != TokenStatsWindow24h {
			t.Fatalf("window=%s", res.Window)
		}
		if res.BucketWidth != TokenStatsBucketHour {
			t.Fatalf("bucketWidth=%s want hour", res.BucketWidth)
		}
		if res.Composition.Total != 8 {
			t.Fatalf("24h composition.total=%d want 8 (only in-progress a2)", res.Composition.Total)
		}
		if len(res.Trend) != 25 {
			t.Fatalf("want 25 hour buckets for rolling 24h, got %d", len(res.Trend))
		}
		if res.Trend[0].Bucket != "2026-07-24T20" {
			t.Fatalf("first hour bucket=%s want 2026-07-24T20", res.Trend[0].Bucket)
		}
		if res.Trend[len(res.Trend)-1].Bucket != "2026-07-25T20" {
			t.Fatalf("last hour bucket=%s want 2026-07-25T20", res.Trend[len(res.Trend)-1].Bucket)
		}
		var hour08 *TokenStatsBucket
		var nonzero int
		for i := range res.Trend {
			b := &res.Trend[i]
			if b.Total != 0 {
				nonzero++
			}
			if b.Bucket == "2026-07-25T08" {
				hour08 = b
			}
		}
		if hour08 == nil || hour08.Total != 8 {
			t.Fatalf("2026-07-25T08 = %+v want total 8", hour08)
		}
		if nonzero != 1 {
			t.Fatalf("missing hours should fill 0; nonzero buckets=%d", nonzero)
		}
	})

	t.Run("24h_empty_no_forged_series", func(t *testing.T) {
		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), emptyProj.ID, TokenStatsQuery{
			Window: TokenStatsWindow24h, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Empty {
			t.Fatal("expected empty")
		}
		if len(res.Trend) != 0 {
			t.Fatalf("empty 24h must not forge zero trend, got %d buckets", len(res.Trend))
		}
	})

	t.Run("pm_usage_in_trend_composition_rank", func(t *testing.T) {
		// g2.1–g2.4: PM ChatMessage.Usage merges into total/trend/composition/rank.
		p, err := s.Create("WithPM", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ts := time.Date(2026, 7, 24, 12, 0, 0, 0, loc).UTC()
		must(&models.WorkflowDef{ID: "wf-pm", ProjectID: p.ID, Name: "wf-pm", Version: 1})
		must(&models.Run{
			ID: "run-pm-wf", WorkflowID: "wf-pm", WorkflowName: "wf-pm",
			Status: "completed", StartedAt: ts,
		})
		must(&models.StateRun{
			RunID: "run-pm-wf", NodeID: "n1", Status: "completed",
			StartedAt: ptr(ts),
			Usage:     &models.TokenUsage{InputTokens: 100, OutputTokens: 20},
		})
		must(&models.ChatThread{ID: "th-pm", ProjectID: p.ID, UserID: "u1", Title: "t"})
		// Historical assistant without Usage — must NOT backfill.
		must(&models.ChatMessage{
			ID: "msg-old", ThreadID: "th-pm", Role: "assistant", Content: "old",
			Status: "ok", CreatedAt: ts,
		})
		must(&models.ChatMessage{
			ID: "msg-pm", ThreadID: "th-pm", Role: "assistant", Content: "hi",
			Status: "ok", CreatedAt: ts,
			Usage: &models.TokenUsage{InputTokens: 40, OutputTokens: 10},
		})
		// Nil usage (unreported) assistant — not counted.
		must(&models.ChatMessage{
			ID: "msg-nil", ThreadID: "th-pm", Role: "assistant", Content: "nil",
			Status: "ok", CreatedAt: ts,
		})

		syncTokenLedger(t, db)
		res, err := s.TokenStats(context.Background(), p.ID, TokenStatsQuery{
			Window: TokenStatsWindow7d, Timezone: "Asia/Shanghai", Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Empty {
			t.Fatal("expected non-empty with PM+workflow")
		}
		// 120 workflow + 50 PM = 170
		if res.Composition.Total != 170 {
			t.Fatalf("composition.total=%d want 170", res.Composition.Total)
		}
		var day24 *TokenStatsBucket
		for i := range res.Trend {
			if res.Trend[i].Bucket == "2026-07-24" {
				day24 = &res.Trend[i]
				break
			}
		}
		if day24 == nil {
			t.Fatal("missing 2026-07-24 bucket")
		}
		if day24.WorkflowTotal != 120 || day24.PmTotal != 50 || day24.Total != 170 {
			t.Fatalf("day24=%+v want wf=120 pm=50 total=170", day24)
		}
		var sawPM, sawWF bool
		for _, w := range res.Workflows {
			switch w.Kind {
			case TokenStatsKindPM:
				sawPM = true
				if w.Total != 50 || w.Other {
					t.Fatalf("pm row=%+v", w)
				}
			case TokenStatsKindWorkflow:
				sawWF = true
			case TokenStatsKindOther:
				t.Fatalf("other must not absorb PM: %+v", w)
			}
		}
		if !sawPM || !sawWF {
			t.Fatalf("rank missing pm/workflow: %+v", res.Workflows)
		}

		syncTokenLedger(t, db)
		bd := s.TokenBreakdown(p.ID)
		if bd.Total == nil || *bd.Total != 170 {
			t.Fatalf("breakdown.total=%v want 170", bd.Total)
		}
		if bd.Workflow == nil || *bd.Workflow != 120 {
			t.Fatalf("breakdown.workflow=%v want 120", bd.Workflow)
		}
		if bd.PM == nil || *bd.PM != 50 {
			t.Fatalf("breakdown.pm=%v want 50", bd.PM)
		}
	})
}

// TestBuildConsumptionRankOrder locks the fixed slot order: Top workflows → PM → other.
// Even when PM.total exceeds some workflow totals, PM must not insert mid-workflow (no 12PM34).
func TestBuildConsumptionRankOrder(t *testing.T) {
	assertOrder := func(t *testing.T, rows []TokenStatsWorkflow) {
		t.Helper()
		var sawPM, sawOther bool
		for i, w := range rows {
			switch w.Kind {
			case TokenStatsKindWorkflow:
				if sawPM || sawOther {
					t.Fatalf("workflow after pm/other at [%d]: %+v", i, rows)
				}
			case TokenStatsKindPM:
				if sawOther {
					t.Fatalf("pm after other at [%d]: %+v", i, rows)
				}
				if sawPM {
					t.Fatalf("duplicate pm at [%d]: %+v", i, rows)
				}
				sawPM = true
			case TokenStatsKindOther:
				if !w.Other {
					t.Fatalf("other kind without Other flag: %+v", w)
				}
				if sawOther {
					t.Fatalf("duplicate other at [%d]: %+v", i, rows)
				}
				sawOther = true
				if i != len(rows)-1 {
					t.Fatalf("other must be last: %+v", rows)
				}
			default:
				t.Fatalf("unexpected kind %q at [%d]", w.Kind, i)
			}
		}
	}

	t.Run("pm_between_top_and_other_even_when_pm_total_high", func(t *testing.T) {
		totals := map[string]int64{
			"wf-a": 1000,
			"wf-b": 100,
			"wf-c": 50,
		}
		names := map[string]string{
			"wf-a": "approve-main",
			"wf-b": "doc-review",
			"wf-c": "调研",
		}
		// PM total (800) sits between wf-a and wf-b; old full-sort would yield 12PM34.
		rows := buildConsumptionRank(totals, names, 800, true)
		assertOrder(t, rows)
		if len(rows) != 4 {
			t.Fatalf("len=%d want 4 (3 wf + pm)", len(rows))
		}
		if rows[0].Kind != TokenStatsKindWorkflow || rows[0].Total != 1000 {
			t.Fatalf("top1=%+v", rows[0])
		}
		if rows[1].Kind != TokenStatsKindWorkflow || rows[1].Total != 100 {
			t.Fatalf("top2=%+v", rows[1])
		}
		if rows[2].Kind != TokenStatsKindWorkflow || rows[2].Total != 50 {
			t.Fatalf("top3=%+v", rows[2])
		}
		if rows[3].Kind != TokenStatsKindPM || rows[3].Total != 800 {
			t.Fatalf("pm=%+v want last before other", rows[3])
		}
	})

	t.Run("with_other_pm_before_other", func(t *testing.T) {
		totals := map[string]int64{}
		names := map[string]string{}
		for i := 1; i <= 12; i++ {
			id := "w" + itoa(i)
			totals[id] = int64(i * 100)
			names[id] = id
		}
		rows := buildConsumptionRank(totals, names, 9999, true)
		assertOrder(t, rows)
		if len(rows) != 12 { // Top10 + PM + other
			t.Fatalf("len=%d want 12", len(rows))
		}
		if rows[10].Kind != TokenStatsKindPM || rows[10].Total != 9999 {
			t.Fatalf("pm slot=%+v", rows[10])
		}
		if rows[11].Kind != TokenStatsKindOther || !rows[11].Other {
			t.Fatalf("other slot=%+v", rows[11])
		}
		// Top block still desc by total.
		for i := 1; i < 10; i++ {
			if rows[i].Total > rows[i-1].Total {
				t.Fatalf("top not desc at %d: %+v", i, rows[:10])
			}
		}
	})

	t.Run("no_pm_no_pm_row", func(t *testing.T) {
		totals := map[string]int64{"a": 10, "b": 5}
		names := map[string]string{"a": "A", "b": "B"}
		rows := buildConsumptionRank(totals, names, 0, false)
		assertOrder(t, rows)
		for _, w := range rows {
			if w.Kind == TokenStatsKindPM {
				t.Fatalf("unexpected pm: %+v", rows)
			}
		}
	})

	t.Run("pm_without_other", func(t *testing.T) {
		totals := map[string]int64{"a": 10}
		names := map[string]string{"a": "A"}
		rows := buildConsumptionRank(totals, names, 3, true)
		assertOrder(t, rows)
		if len(rows) != 2 || rows[1].Kind != TokenStatsKindPM {
			t.Fatalf("want Top→PM: %+v", rows)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func sumModelTotals(rows []TokenStatsModel) int64 {
	var n int64
	for _, r := range rows {
		n += r.Total
	}
	return n
}

func assertRankConservesComposition(t *testing.T, comp, rank []TokenStatsModel) {
	t.Helper()
	cSum, rSum := sumModelTotals(comp), sumModelTotals(rank)
	if cSum != rSum {
		t.Fatalf("ranking Σ=%d != composition Σ=%d", rSum, cSum)
	}
	for _, r := range rank {
		if r.Other && r.Unknown {
			t.Fatalf("other must not be marked unknown: %+v", r)
		}
	}
}

// TestBuildModelStatsDemoScenes locks Demo s2/s3/s4: unknown competes by total,
// qualifies as「未知/未分桶」, otherwise folds into other; ranking Σ = composition Σ.
func TestBuildModelStatsDemoScenes(t *testing.T) {
	t.Parallel()

	t.Run("s2_unknown_qualifies_with_other", func(t *testing.T) {
		totals := map[string]*tokenModelAgg{}
		for i := 0; i < 12; i++ {
			totals[fmt.Sprintf("m%02d", i)] = &tokenModelAgg{total: int64(100 - i), source: models.TokenUsageSourceUpstream}
		}
		// Unknown + filled bridge totals high enough to enter Top10 independently.
		totals[models.TokenUsageModelUnknown] = &tokenModelAgg{total: 95, source: models.TokenUsageSourceUnknown}
		totals["bridge-m"] = &tokenModelAgg{total: 94, filled: true, source: models.TokenUsageSourceBridge}

		comp, rank := buildModelStats(totals, "")
		if len(comp) != 14 {
			t.Fatalf("composition len=%d", len(comp))
		}
		if len(rank) != 11 { // Top10 + other
			t.Fatalf("rank len=%d want 11", len(rank))
		}
		var unkIdx = -1
		var hasOther, hasFilled bool
		for i, r := range rank {
			if r.Other {
				hasOther = true
				if r.Unknown {
					t.Fatal("other must not be marked unknown")
				}
				if r.Name != "other" {
					t.Fatalf("other name=%q", r.Name)
				}
			}
			if r.Unknown {
				if unkIdx >= 0 {
					t.Fatal("duplicate unknown row")
				}
				unkIdx = i
				if r.Name != models.TokenUsageModelUnknownDisplay {
					t.Fatalf("unknown renamed: %q", r.Name)
				}
				if r.Other {
					t.Fatal("unknown row must not be other")
				}
			}
			if r.Filled {
				hasFilled = true
			}
		}
		if !hasOther {
			t.Fatal("expected other for >10 models")
		}
		if unkIdx < 0 {
			t.Fatal("unknown must appear in ranking independently")
		}
		if unkIdx >= 10 {
			t.Fatalf("unknown must not be forced as extra 11th row, idx=%d", unkIdx)
		}
		if !hasFilled {
			t.Fatal("filled bridge bucket must surface in ranking")
		}
		var compFilled bool
		for _, r := range comp {
			if r.Filled {
				compFilled = true
			}
		}
		if !compFilled {
			t.Fatal("composition must include filled bucket")
		}
		assertRankConservesComposition(t, comp, rank)
	})

	t.Run("s3_unknown_below_top10_folded_into_other", func(t *testing.T) {
		totals := map[string]*tokenModelAgg{}
		for i := 0; i < 12; i++ {
			totals[fmt.Sprintf("m%02d", i)] = &tokenModelAgg{
				total:  int64(120 - i*10), // 120,110,...,10
				source: models.TokenUsageSourceUpstream,
			}
		}
		unkTotal := int64(5)
		totals[models.TokenUsageModelUnknown] = &tokenModelAgg{total: unkTotal, source: models.TokenUsageSourceUnknown}

		comp, rank := buildModelStats(totals, "")
		if len(comp) != 13 {
			t.Fatalf("composition len=%d", len(comp))
		}
		var hasUnk bool
		var other *TokenStatsModel
		for i := range rank {
			r := &rank[i]
			if r.Unknown {
				hasUnk = true
			}
			if r.Other {
				other = r
			}
		}
		if hasUnk {
			t.Fatal("unknown below Top10 must not appear as its own row")
		}
		if other == nil {
			t.Fatal("expected other remainder")
		}
		if other.Unknown {
			t.Fatal("other.Unknown must be false")
		}
		if other.Name != "other" {
			t.Fatalf("other name=%q", other.Name)
		}
		// other = m10(20)+m11(10)+unk(5)=35
		if other.Total != 35 {
			t.Fatalf("other.Total=%d want 35 (must include unknown %d)", other.Total, unkTotal)
		}
		assertRankConservesComposition(t, comp, rank)
	})

	t.Run("s4_at_most_ten_no_other", func(t *testing.T) {
		totals := map[string]*tokenModelAgg{
			"m00":                         {total: 50, source: models.TokenUsageSourceUpstream},
			"m01":                         {total: 40, source: models.TokenUsageSourceUpstream},
			"m02":                         {total: 30, source: models.TokenUsageSourceUpstream},
			models.TokenUsageModelUnknown: {total: 20, source: models.TokenUsageSourceUnknown},
		}
		comp, rank := buildModelStats(totals, "")
		if len(comp) != 4 || len(rank) != 4 {
			t.Fatalf("comp=%d rank=%d want 4", len(comp), len(rank))
		}
		var hasOther, hasUnk bool
		for _, r := range rank {
			if r.Other {
				hasOther = true
			}
			if r.Unknown {
				hasUnk = true
				if r.Name != models.TokenUsageModelUnknownDisplay {
					t.Fatalf("unknown name=%q", r.Name)
				}
			}
		}
		if hasOther {
			t.Fatal("≤10 buckets must not emit other")
		}
		if !hasUnk {
			t.Fatal("unknown must appear under 未知/未分桶 when ≤10")
		}
		assertRankConservesComposition(t, comp, rank)
	})
}

func TestTokenStatsUsageWithoutModelMapsToUnknown(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "token_stats_unknown.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	proj, err := s.Create("LegacyTok", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day := time.Date(2026, 7, 24, 10, 0, 0, 0, loc).UTC()
	ptr := func(tt time.Time) *time.Time { return &tt }
	if err := db.Create(&models.WorkflowDef{ID: "wf-leg", ProjectID: proj.ID, Name: "leg", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Run{ID: "run-leg", WorkflowID: "wf-leg", WorkflowName: "leg", Status: "completed", StartedAt: day}).Error; err != nil {
		t.Fatal(err)
	}
	// Usage reported without a per-model breakdown.
	if err := db.Create(&models.StateRun{
		RunID: "run-leg", NodeID: "n1", Status: "completed", StartedAt: ptr(day),
		Usage: &models.TokenUsage{InputTokens: 40, OutputTokens: 10},
	}).Error; err != nil {
		t.Fatal(err)
	}
	syncTokenLedger(t, db)
	res, err := s.TokenStats(context.Background(), proj.ID, TokenStatsQuery{
		Window: TokenStatsWindow30d, Timezone: "Asia/Shanghai", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ModelComposition) != 1 || res.ModelComposition[0].Name != models.TokenUsageModelUnknownDisplay {
		t.Fatalf("composition=%+v", res.ModelComposition)
	}
	if res.ModelComposition[0].Total != 50 || !res.ModelComposition[0].Unknown {
		t.Fatalf("unknown bucket=%+v", res.ModelComposition[0])
	}
}

func TestBuildModelStatsUnknownAlias(t *testing.T) {
	t.Parallel()
	totals := map[string]*tokenModelAgg{
		"gpt-5":                       {total: 100, source: models.TokenUsageSourceUpstream},
		models.TokenUsageModelUnknown: {total: 50, source: models.TokenUsageSourceUnknown},
	}

	t.Run("alias_replaces_name_keeps_key_and_unknown", func(t *testing.T) {
		comp, rank := buildModelStats(totals, "gpt-5")
		var unkComp, unkRank *TokenStatsModel
		for i := range comp {
			if comp[i].Unknown {
				unkComp = &comp[i]
			}
		}
		for i := range rank {
			if rank[i].Unknown {
				unkRank = &rank[i]
			}
		}
		if unkComp == nil || unkRank == nil {
			t.Fatal("expected unknown row")
		}
		if unkComp.ModelKey != models.TokenUsageModelUnknown || unkRank.ModelKey != models.TokenUsageModelUnknown {
			t.Fatalf("ModelKey mutated: %q / %q", unkComp.ModelKey, unkRank.ModelKey)
		}
		if !unkComp.Unknown || !unkRank.Unknown {
			t.Fatal("Unknown flag cleared")
		}
		if unkComp.Name != "gpt-5" || unkRank.Name != "gpt-5" {
			t.Fatalf("alias Name want gpt-5 got %q / %q", unkComp.Name, unkRank.Name)
		}
		// Same display name as real bucket → two rows, not merged.
		if len(comp) != 2 || len(rank) != 2 {
			t.Fatalf("must keep two rows: comp=%d rank=%d", len(comp), len(rank))
		}
	})

	t.Run("empty_alias_keeps_default_name", func(t *testing.T) {
		comp, _ := buildModelStats(totals, "  ")
		for _, r := range comp {
			if r.Unknown && r.Name != models.TokenUsageModelUnknownDisplay {
				t.Fatalf("empty alias Name=%q", r.Name)
			}
		}
	})
}

func TestParseTokenStatsWindow(t *testing.T) {
	spec, err := parseTokenStatsWindow(TokenStatsWindow24h)
	if err != nil {
		t.Fatal(err)
	}
	if spec.duration != 24*time.Hour || spec.bucketWidth != TokenStatsBucketHour || spec.days != 0 {
		t.Fatalf("24h spec=%+v", spec)
	}
	spec, err = parseTokenStatsWindow(TokenStatsWindow7d)
	if err != nil {
		t.Fatal(err)
	}
	if spec.days != 7 || spec.bucketWidth != TokenStatsBucketDay || spec.duration != 0 {
		t.Fatalf("7d spec=%+v", spec)
	}
	spec, err = parseTokenStatsWindow("")
	if err != ErrInvalidTokenStatsWindow {
		t.Fatalf("empty window err=%v spec=%+v", err, spec)
	}
	for _, bad := range []string{"1d", "1y", "today"} {
		if _, err := parseTokenStatsWindow(bad); err != ErrInvalidTokenStatsWindow {
			t.Fatalf("%s err=%v", bad, err)
		}
	}
}

func TestBuildWindowSlice24h(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 20, 30, 0, 0, loc)
	spec := tokenStatsWindowSpec{duration: 24 * time.Hour, bucketWidth: TokenStatsBucketHour}
	cur := buildWindowSlice(now, spec)
	wantStart := time.Date(2026, 7, 24, 20, 30, 0, 0, loc)
	if !cur.hasStart || !cur.start.Equal(wantStart) || !cur.end.Equal(now) {
		t.Fatalf("24h cur=%+v want start=%v end=%v", cur, wantStart, now)
	}
	prev := buildPrevWindowSlice(cur, spec)
	wantPrevStart := time.Date(2026, 7, 23, 20, 30, 0, 0, loc)
	wantPrevEnd := wantStart.Add(-time.Nanosecond)
	if !prev.hasStart || !prev.start.Equal(wantPrevStart) || !prev.end.Equal(wantPrevEnd) {
		t.Fatalf("24h prev=%+v want start=%v end=%v", prev, wantPrevStart, wantPrevEnd)
	}

	spec7 := tokenStatsWindowSpec{days: 7, bucketWidth: TokenStatsBucketDay}
	cur7 := buildWindowSlice(now, spec7)
	want7Start := time.Date(2026, 7, 19, 0, 0, 0, 0, loc)
	if !cur7.start.Equal(want7Start) {
		t.Fatalf("7d calendar start=%v want %v", cur7.start, want7Start)
	}
}

func TestFillBucketKeysHourCrossMidnight(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 2, 15, 0, 0, loc)
	start := now.Add(-24 * time.Hour)
	keys := fillBucketKeys(now, start, true, TokenStatsBucketHour, nil)
	if len(keys) != 25 {
		t.Fatalf("len=%d want 25", len(keys))
	}
	if keys[0] != "2026-07-24T02" {
		t.Fatalf("first=%s want 2026-07-24T02", keys[0])
	}
	if keys[len(keys)-1] != "2026-07-25T02" {
		t.Fatalf("last=%s want 2026-07-25T02", keys[len(keys)-1])
	}
	seen := map[string]struct{}{}
	var has24, has25 bool
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			t.Fatalf("duplicate hour key %s", k)
		}
		seen[k] = struct{}{}
		if len(k) >= 10 && k[:10] == "2026-07-24" {
			has24 = true
		}
		if len(k) >= 10 && k[:10] == "2026-07-25" {
			has25 = true
		}
	}
	if !has24 || !has25 {
		t.Fatalf("expected cross-midnight hour keys, got %v", keys)
	}
	if _, ok := seen["2026-07-24T23"]; !ok {
		t.Fatal("missing 2026-07-24T23 (zero-fill hour across midnight)")
	}
}

func TestTokenStats24hHourFillAcrossMidnight(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "token_stats_24h.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	proj, err := s.Create("Tok24h", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	must := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	ptr := func(tt time.Time) *time.Time { return &tt }
	// now = 02:15 July 25 local → window [01:15 July 24, 02:15 July 25] wait: 02:15-24h = 02:15 July 24.
	now := time.Date(2026, 7, 24, 18, 15, 0, 0, time.UTC) // 2026-07-25 02:15 Shanghai
	must(&models.WorkflowDef{ID: "wf-24", ProjectID: proj.ID, Name: "hourly", Version: 1})

	before := time.Date(2026, 7, 24, 1, 0, 0, 0, loc).UTC() // 01:00 July 24 — outside
	must(&models.Run{ID: "run-before", WorkflowID: "wf-24", WorkflowName: "hourly", Status: "completed", StartedAt: before})
	must(&models.StateRun{
		RunID: "run-before", NodeID: "n1", Status: "completed", StartedAt: ptr(before),
		Usage: &models.TokenUsage{InputTokens: 999},
	})

	h22 := time.Date(2026, 7, 24, 22, 10, 0, 0, loc).UTC()
	must(&models.Run{ID: "run-22", WorkflowID: "wf-24", WorkflowName: "hourly", Status: "completed", StartedAt: h22})
	must(&models.StateRun{
		RunID: "run-22", NodeID: "n1", Status: "completed", StartedAt: ptr(h22),
		Usage: &models.TokenUsage{InputTokens: 100},
	})

	h01 := time.Date(2026, 7, 25, 1, 5, 0, 0, loc).UTC()
	must(&models.Run{ID: "run-01", WorkflowID: "wf-24", WorkflowName: "hourly", Status: "completed", StartedAt: h01})
	must(&models.StateRun{
		RunID: "run-01", NodeID: "n1", Status: "completed", StartedAt: ptr(h01),
		Usage: &models.TokenUsage{InputTokens: 50},
	})

	after := time.Date(2026, 7, 25, 3, 0, 0, 0, loc).UTC() // after now
	must(&models.Run{ID: "run-after", WorkflowID: "wf-24", WorkflowName: "hourly", Status: "completed", StartedAt: after})
	must(&models.StateRun{
		RunID: "run-after", NodeID: "n1", Status: "completed", StartedAt: ptr(after),
		Usage: &models.TokenUsage{InputTokens: 777},
	})

	syncTokenLedger(t, db)
	res, err := s.TokenStats(context.Background(), proj.ID, TokenStatsQuery{
		Window: TokenStatsWindow24h, Timezone: "Asia/Shanghai", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Empty || res.Composition.Total != 150 {
		t.Fatalf("total=%d empty=%v want 150", res.Composition.Total, res.Empty)
	}
	byKey := map[string]TokenStatsBucket{}
	for _, b := range res.Trend {
		byKey[b.Bucket] = b
	}
	if byKey["2026-07-24T22"].Total != 100 {
		t.Fatalf("hour 22 = %+v", byKey["2026-07-24T22"])
	}
	if byKey["2026-07-25T01"].Total != 50 {
		t.Fatalf("hour 01 = %+v", byKey["2026-07-25T01"])
	}
	if byKey["2026-07-24T23"].Total != 0 {
		t.Fatalf("missing hour 23 should be 0, got %+v", byKey["2026-07-24T23"])
	}
	if _, ok := byKey["2026-07-24T01"]; ok && byKey["2026-07-24T01"].Total == 999 {
		t.Fatal("usage before window start must be excluded")
	}
	if byKey["2026-07-25T03"].Total == 777 {
		t.Fatal("usage after now must be excluded")
	}
}
