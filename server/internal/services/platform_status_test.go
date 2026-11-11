package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestPlatformStatus_emptyNullAndTrueZero(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_status_empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	dash := NewDashboardService(db, workspace)

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 8, 12, 14, 7, 0, 0, loc)

	syncTokenLedger(t, db)
	got, err := dash.PlatformStatus(context.Background(), PlatformStatusQuery{
		UTCOffsetMinutes: intPtr(8 * 60),
		Now:              now.UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CumulativeTokens != nil || got.TodayTokens != nil {
		t.Fatalf("want null token fields, got cum=%v today=%v",
			got.CumulativeTokens, got.TodayTokens)
	}
	if got.RunningCount != 0 || got.QueuedCount != 0 {
		t.Fatalf("want running/queued 0, got %d/%d", got.RunningCount, got.QueuedCount)
	}

	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	mustCreate(&models.Run{ID: "r-run", Status: "running", StartedAt: now.UTC(), CreatedAt: now.UTC()})
	mustCreate(&models.Run{ID: "r-q", Status: "queued", CreatedAt: now.UTC()})
	mustCreate(&models.Run{ID: "r-wh", Status: "waiting_human", StartedAt: now.UTC(), CreatedAt: now.UTC()})

	syncTokenLedger(t, db)
	got, err = dash.PlatformStatus(context.Background(), PlatformStatusQuery{
		UTCOffsetMinutes: intPtr(8 * 60),
		Now:              now.UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.RunningCount != 1 || got.QueuedCount != 1 {
		t.Fatalf("running/queued want 1/1 (waiting_human excluded), got %d/%d", got.RunningCount, got.QueuedCount)
	}
}

func TestPlatformStatus_todayTokensSum(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_status_today.db"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	dash := NewDashboardService(db, workspace)

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 8, 12, 14, 7, 0, 0, loc)
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	_, err = workspace.CreateFixture("P", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(&models.WorkflowDef{ID: "wf1", Name: "w", Version: 1})

	morning := time.Date(2026, 8, 12, 11, 22, 0, 0, loc)
	mustCreate(&models.Run{ID: "run-a", WorkflowID: "wf1", Status: "completed", StartedAt: morning.UTC(), CreatedAt: morning.UTC()})
	srA := morning.UTC()
	mustCreate(&models.StateRun{
		RunID: "run-a", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 12104}, StartedAt: &srA,
	})

	mid := time.Date(2026, 8, 12, 10, 1, 0, 0, loc)
	mustCreate(&models.Run{ID: "run-b", WorkflowID: "wf1", Status: "completed", StartedAt: mid.UTC(), CreatedAt: mid.UTC()})
	srB := mid.UTC()
	mustCreate(&models.StateRun{
		RunID: "run-b", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 100}, StartedAt: &srB,
	})

	cur := time.Date(2026, 8, 12, 14, 6, 0, 0, loc)
	mustCreate(&models.Run{ID: "run-c", WorkflowID: "wf1", Status: "running", StartedAt: cur.UTC(), CreatedAt: cur.UTC()})
	srC := cur.UTC()
	mustCreate(&models.StateRun{
		RunID: "run-c", NodeID: "n1", Status: "running",
		Usage: &models.TokenUsage{InputTokens: 4812}, StartedAt: &srC,
	})

	// Yesterday must not enter todayTokens (g1.2/g1.3).
	yest := time.Date(2026, 8, 11, 15, 0, 0, 0, loc)
	mustCreate(&models.Run{ID: "run-yest", WorkflowID: "wf1", Status: "completed", StartedAt: yest.UTC(), CreatedAt: yest.UTC()})
	srY := yest.UTC()
	mustCreate(&models.StateRun{
		RunID: "run-yest", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 999999}, StartedAt: &srY,
	})

	syncTokenLedger(t, db)
	got, err := dash.PlatformStatus(context.Background(), PlatformStatusQuery{
		UTCOffsetMinutes: intPtr(8 * 60),
		Now:              now.UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CumulativeTokens == nil || *got.CumulativeTokens != 12104+100+4812+999999 {
		t.Fatalf("cumulative=%v", got.CumulativeTokens)
	}
	wantToday := int64(12104 + 100 + 4812)
	if got.TodayTokens == nil || *got.TodayTokens != wantToday {
		t.Fatalf("todayTokens=%v want %d", got.TodayTokens, wantToday)
	}
}

func TestPlatformStatus_allLedgerSources(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources []string
		units   int64
	}{
		{name: "solo_only", sources: []string{models.TokenLedgerSourceSolo}, units: 1},
		{name: "studio_only", sources: []string{models.TokenLedgerSourceStudio}, units: 1},
		{name: "all_sources", sources: []string{
			models.TokenLedgerSourceWorkflow, models.TokenLedgerSourcePM,
			models.TokenLedgerSourceSolo, models.TokenLedgerSourceStudio,
		}, units: 1},
		{name: "solo_reported_zero", sources: []string{models.TokenLedgerSourceSolo}},
		{name: "studio_reported_zero", sources: []string{models.TokenLedgerSourceStudio}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_sources.db"))
			if err != nil {
				t.Fatal(err)
			}
			workspace := NewWorkspaceService(db)
			dash := NewDashboardService(db, workspace)
			loc := time.FixedZone("UTC+8", 8*3600)
			now := time.Date(2026, 8, 12, 0, 30, 0, 0, loc)
			midnight := truncateLocalDay(now)
			for _, source := range tc.sources {
				seedLedger(t, db,
					models.TokenUsageEvent{
						CreatedAt: midnight.Add(-time.Second).UTC(), Source: source,
						Status: models.TokenLedgerStatusCancelled, InputTokens: 5 * tc.units,
					},
					models.TokenUsageEvent{
						CreatedAt: now.Add(-time.Minute).UTC(), Source: source,
						Status:      models.TokenLedgerStatusFailed,
						InputTokens: 10 * tc.units, OutputTokens: 20 * tc.units,
						CacheReadTokens: 30 * tc.units, CacheWriteTokens: 40 * tc.units,
					},
				)
			}
			got, err := dash.PlatformStatus(context.Background(), PlatformStatusQuery{
				UTCOffsetMinutes: intPtr(8 * 60), Now: now.UTC(),
			})
			if err != nil {
				t.Fatal(err)
			}
			wantTotal := int64(len(tc.sources)) * 105 * tc.units
			wantToday := int64(len(tc.sources)) * 100 * tc.units
			if got.CumulativeTokens == nil || *got.CumulativeTokens != wantTotal {
				t.Fatalf("cumulative=%v want %d", got.CumulativeTokens, wantTotal)
			}
			if got.TodayTokens == nil || *got.TodayTokens != wantToday {
				t.Fatalf("today=%v want %d", got.TodayTokens, wantToday)
			}
			if len(tc.sources) == 1 {
				breakdown := workspace.TokenBreakdown()
				if breakdown.Workflow != nil || breakdown.PM != nil {
					t.Fatalf("Solo/Studio usage must not enter workflow or PM subtotals: %+v", breakdown)
				}
			}
			global, err := workspace.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{
				Window: TokenStatsWindowAll, UTCOffsetMinutes: intPtr(8 * 60), Now: now.UTC(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if *got.CumulativeTokens != global.KPI.Total {
				t.Fatalf("platform total=%d global total=%d", *got.CumulativeTokens, global.KPI.Total)
			}
		})
	}
}

func TestPlatformStatus_activeSoloCountsStayFresh(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_solo.db"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := []models.SoloSession{
		{ID: "solo-starting", Status: models.SoloStatusStarting},
		{ID: "solo-running-a", Status: models.SoloStatusRunning},
		{ID: "solo-running-b", Status: models.SoloStatusRunning},
		{ID: "solo-closed", Status: models.SoloStatusClosed},
		{ID: "solo-error", Status: models.SoloStatusError},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	queues := []models.ChatQueue{
		{ID: "q-a", Surface: soloPendingSurface, SessionKey: "solo-running-a", ActiveTurnID: "turn-1"},
		{ID: "q-b", Surface: soloPendingSurface, SessionKey: "solo-running-b"},
		{ID: "q-closed", Surface: soloPendingSurface, SessionKey: "solo-closed", ActiveTurnID: "turn-2"},
		{ID: "q-other", Surface: "pm", SessionKey: "solo-running-b", ActiveTurnID: "turn-3"},
	}
	if err := db.Create(&queues).Error; err != nil {
		t.Fatal(err)
	}
	dash := NewDashboardService(db, NewWorkspaceService(db))
	q := PlatformStatusQuery{Timezone: "UTC", Now: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)}
	assertCounts := func(running, queued int64) {
		t.Helper()
		got, err := dash.PlatformStatus(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if got.RunningCount != running || got.QueuedCount != queued {
			t.Fatalf("running/queued=%d/%d want %d/%d", got.RunningCount, got.QueuedCount, running, queued)
		}
	}
	// starting + busy running-a; idle running-b and closed sessions do not count.
	assertCounts(2, 0)
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", "q-b").Update("active_turn_id", "turn-4").Error; err != nil {
		t.Fatal(err)
	}
	assertCounts(3, 0)
	runs := []models.Run{
		{ID: "run-running", Status: "running"},
		{ID: "run-queued", Status: "queued"},
		{ID: "run-waiting", Status: "waiting_human"},
		{ID: "run-completed", Status: "completed"},
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	assertCounts(4, 1)
	if err := db.Model(&models.SoloSession{}).Where("id = ?", "solo-starting").Update("status", models.SoloStatusClosed).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.SoloSession{}).Where("id = ?", "solo-running-a").Update("status", models.SoloStatusError).Error; err != nil {
		t.Fatal(err)
	}
	assertCounts(2, 1)
}

func TestPlatformStatus_crossDayTodayReset(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_status_day.db"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	dash := NewDashboardService(db, workspace)
	loc := time.FixedZone("UTC+8", 8*3600)
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	_, err = workspace.CreateFixture("P", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(&models.WorkflowDef{ID: "wf1", Name: "w", Version: 1})

	day1 := time.Date(2026, 8, 11, 12, 2, 0, 0, loc)
	mustCreate(&models.Run{ID: "run-d1", WorkflowID: "wf1", Status: "completed", StartedAt: day1.UTC(), CreatedAt: day1.UTC()})
	sr1 := day1.UTC()
	mustCreate(&models.StateRun{
		RunID: "run-d1", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 5000}, StartedAt: &sr1,
	})

	// New day 00:07 — yesterday excluded; today zero while cumulative exists (g1.3).
	day2 := time.Date(2026, 8, 12, 0, 7, 0, 0, loc)
	syncTokenLedger(t, db)
	got, err := dash.PlatformStatus(context.Background(), PlatformStatusQuery{
		UTCOffsetMinutes: intPtr(8 * 60),
		Now:              day2.UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CumulativeTokens == nil || *got.CumulativeTokens != 5000 {
		t.Fatalf("cumulative=%v want 5000", got.CumulativeTokens)
	}
	if got.TodayTokens == nil || *got.TodayTokens != 0 {
		t.Fatalf("cross-day todayTokens want 0, got %v", got.TodayTokens)
	}
}

func TestPlatformStatus_readsNewUsageWithoutCache(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_status_cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	dash := NewDashboardService(db, workspace)
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 8, 12, 14, 7, 0, 0, loc)
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, err = workspace.CreateFixture("P", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(&models.WorkflowDef{ID: "wf1", Name: "w", Version: 1})
	ts := time.Date(2026, 8, 12, 14, 6, 0, 0, loc).UTC()
	mustCreate(&models.Run{ID: "run1", WorkflowID: "wf1", Status: "completed", StartedAt: ts, CreatedAt: ts})
	mustCreate(&models.StateRun{
		RunID: "run1", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 10}, StartedAt: &ts,
	})

	q := PlatformStatusQuery{UTCOffsetMinutes: intPtr(8 * 60), Now: now.UTC()}
	syncTokenLedger(t, db)
	first, err := dash.PlatformStatus(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if first.TodayTokens == nil || *first.TodayTokens != 10 {
		t.Fatalf("first today=%v", first.TodayTokens)
	}

	// A later report must update cumulative and today together on the next poll.
	ts2 := time.Date(2026, 8, 12, 14, 6, 30, 0, loc).UTC()
	mustCreate(&models.Run{ID: "run2", WorkflowID: "wf1", Status: "completed", StartedAt: ts2, CreatedAt: ts2})
	mustCreate(&models.StateRun{
		RunID: "run2", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 90}, StartedAt: &ts2,
	})
	syncTokenLedger(t, db)
	second, err := dash.PlatformStatus(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if second.TodayTokens == nil || *second.TodayTokens != 100 || second.CumulativeTokens == nil || *second.CumulativeTokens != 100 {
		t.Fatalf("stale totals: cumulative=%v today=%v want 100", second.CumulativeTokens, second.TodayTokens)
	}

}

func intPtr(v int) *int { return &v }
