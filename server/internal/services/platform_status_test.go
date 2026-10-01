package services

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	projects := NewProjectService(db)
	dash := NewDashboardService(db, projects)

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

	dash.ClearPlatformStatusCacheForTest()
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
	projects := NewProjectService(db)
	dash := NewDashboardService(db, projects)

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 8, 12, 14, 7, 0, 0, loc)
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	p, err := projects.Create("P", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(&models.WorkflowDef{ID: "wf1", ProjectID: p.ID, Name: "w", Status: "draft", Version: 1})

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

func TestPlatformStatus_crossDayTodayReset(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_status_day.db"))
	if err != nil {
		t.Fatal(err)
	}
	projects := NewProjectService(db)
	dash := NewDashboardService(db, projects)
	loc := time.FixedZone("UTC+8", 8*3600)
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}

	p, err := projects.Create("P", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(&models.WorkflowDef{ID: "wf1", ProjectID: p.ID, Name: "w", Status: "draft", Version: 1})

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

func TestPlatformStatus_cacheHitSkipsRescan(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_status_cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	projects := NewProjectService(db)
	dash := NewDashboardService(db, projects)
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 8, 12, 14, 7, 0, 0, loc)
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	p, err := projects.Create("P", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(&models.WorkflowDef{ID: "wf1", ProjectID: p.ID, Name: "w", Status: "draft", Version: 1})
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

	// Add more usage in same bucket; cache should still return 10 until TTL.
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
	if second.TodayTokens == nil || *second.TodayTokens != 10 {
		t.Fatalf("cache miss? today=%v want stale 10", second.TodayTokens)
	}

	// After TTL, recompute.
	dash.ClearPlatformStatusCacheForTest()
	syncTokenLedger(t, db)
	third, err := dash.PlatformStatus(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if third.TodayTokens == nil || *third.TodayTokens != 100 {
		t.Fatalf("after clear today=%v want 100", third.TodayTokens)
	}
}

func TestPlatformStatus_singleflightMerges(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "platform_status_sf.db"))
	if err != nil {
		t.Fatal(err)
	}
	projects := NewProjectService(db)
	dash := NewDashboardService(db, projects)
	now := time.Date(2026, 8, 12, 14, 7, 0, 0, time.UTC)

	var started int32
	var release sync.WaitGroup
	release.Add(1)
	orig := dash.loadPlatformUsageHook
	dash.loadPlatformUsageHook = func() {
		atomic.AddInt32(&started, 1)
		release.Wait()
	}
	defer func() { dash.loadPlatformUsageHook = orig }()

	q := PlatformStatusQuery{Timezone: "UTC", Now: now}
	var wg sync.WaitGroup
	const n = 8
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = dash.PlatformStatus(context.Background(), q)
		}(i)
	}
	// Wait until the singleflight leader enters the hook (avoid fixed-sleep flake).
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&started) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&started); got != 1 {
		release.Done()
		t.Fatalf("expected 1 in-flight compute, got %d", got)
	}
	release.Done()
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d: %v", i, e)
		}
	}
}

func intPtr(v int) *int { return &v }
