package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

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
