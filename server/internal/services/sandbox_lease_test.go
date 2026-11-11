package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func tp(t time.Time) *time.Time { return &t }

func TestComputeDeadline(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	ttl := 30 * time.Minute
	maxRet := 24 * time.Hour
	debug := models.SandboxLease{Kind: models.LeaseDebug, Until: tp(now.Add(ttl)), CreatedAt: now}

	if computeDeadline(nil, now, ttl, maxRet, runPhase{}) != nil {
		t.Fatal("no leases must not produce a deadline")
	}
	if got := computeDeadline([]models.SandboxLease{debug}, now, ttl, maxRet, runPhase{}); !got.Equal(now.Add(ttl)) {
		t.Fatalf("debug only = %v", got)
	}
	// Until-run-end lease slides while the run is active ...
	preview := models.SandboxLease{Kind: models.LeasePreview, CreatedAt: now.Add(-time.Hour)}
	later := now.Add(2 * time.Hour)
	if got := computeDeadline([]models.SandboxLease{debug, preview}, later, ttl, maxRet, runPhase{active: true}); !got.Equal(later.Add(ttl)) {
		t.Fatalf("run-end lease while active = %v", got)
	}
	// ... and runs for the TTL after the run ends.
	ended := now.Add(time.Hour)
	if got := computeDeadline([]models.SandboxLease{debug, preview}, later, ttl, maxRet, runPhase{endedAt: ended}); !got.Equal(ended.Add(ttl)) {
		t.Fatalf("run-end lease after run = %v", got)
	}
	// Max retention caps from the debug lease, not from the older preview lease.
	manual := models.SandboxLease{Kind: models.LeaseManual, Until: tp(now.Add(48 * time.Hour)), CreatedAt: now}
	if got := computeDeadline([]models.SandboxLease{preview, debug, manual}, now, ttl, maxRet, runPhase{}); !got.Equal(now.Add(maxRet)) {
		t.Fatalf("capped = %v", got)
	}
}

func TestUpsertLeaseKeepsLongestAndAnchor(t *testing.T) {
	now := time.Now()
	first := models.SandboxLease{Kind: models.LeaseActivity, Until: tp(now.Add(time.Minute)), CreatedAt: now.Add(-time.Hour)}
	next := models.SandboxLease{Kind: models.LeaseActivity, Until: tp(now.Add(time.Hour)), CreatedAt: now}
	got := upsertLease([]models.SandboxLease{first}, next)
	if len(got) != 1 || !got[0].Until.Equal(*next.Until) || !got[0].CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("renewal = %+v", got)
	}
	shorter := models.SandboxLease{Kind: models.LeaseActivity, Until: tp(now), CreatedAt: now}
	if got2 := upsertLease(got, shorter); !got2[0].Until.Equal(*next.Until) {
		t.Fatalf("shorter lease replaced a longer one: %+v", got2)
	}
	runEnd := models.SandboxLease{Kind: models.LeaseActivity, CreatedAt: now}
	if got3 := upsertLease(got, runEnd); got3[0].Until != nil {
		t.Fatalf("until-run-end must outlast timed: %+v", got3)
	}
}

// The run-89404635 failure mode: a node sandbox retired mid-run must stay up
// while a preview lease holds it, and the sweeper must re-check leases rather
// than trust the cached destroy_at.
func TestRetiredRunSandboxLeases(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	name := "grasp-sb-lease"
	info := runtimeInfo(name)
	db.Create(&models.Run{ID: info.RunID, Status: "running", StartedAt: time.Now()})
	s.RegisterRunSandbox(info)
	if err := s.AddLeaseByName(name, models.SandboxLease{Kind: models.LeasePreview, Reason: "set_preview"}); err != nil {
		t.Fatalf("preview lease: %v", err)
	}
	var row models.Sandbox
	db.Where("name = ?", name).First(&row)
	if row.DestroyAt != nil {
		t.Fatal("a lease must not schedule destruction while the node still uses the sandbox")
	}

	s.RetireRunSandbox(name)
	db.Where("name = ?", name).First(&row)
	if row.DestroyAt == nil || len(row.Leases) != 2 {
		t.Fatalf("retire: destroy_at=%v leases=%+v", row.DestroyAt, row.Leases)
	}

	// Cached deadline passes while the run is still going: the preview lease holds.
	past := time.Now().Add(-time.Minute)
	db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("destroy_at", &past)
	s.sweepOnce(ctx)
	if err := db.Where("name = ?", name).First(&row).Error; err != nil {
		t.Fatal("swept while the preview lease still holds")
	}
	if !row.DestroyAt.After(time.Now()) {
		t.Fatalf("sweeper did not refresh destroy_at: %v", row.DestroyAt)
	}

	// Run ended long ago: every lease has lapsed and the sweeper reclaims it.
	db.Model(&models.Run{}).Where("id = ?", info.RunID).
		Updates(map[string]any{"status": "completed", "started_at": time.Now().Add(-3 * time.Hour), "duration_sec": 60})
	s.SetTTLs(time.Minute, 0)
	db.Model(&models.Sandbox{ID: row.ID}).Select("destroy_at", "leases").Updates(&models.Sandbox{
		DestroyAt: &past,
		Leases:    []models.SandboxLease{{Kind: models.LeasePreview, CreatedAt: past}, {Kind: models.LeaseDebug, Until: &past, CreatedAt: past}},
	})
	s.sweepOnce(ctx)
	var n int64
	db.Model(&models.Sandbox{}).Where("name = ?", name).Count(&n)
	if n != 0 {
		t.Fatal("expired leases must let the sweeper destroy the sandbox")
	}
}

func TestExtendLeaseAndActivity(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)

	name := "grasp-sb-extend"
	info := runtimeInfo(name)
	db.Create(&models.Run{ID: info.RunID, Status: "completed", StartedAt: time.Now()})
	s.RegisterRunSandbox(info)
	var row models.Sandbox
	db.Where("name = ?", name).First(&row)

	if _, err := s.ExtendLease(row.ID, 60, false, "admin"); !errors.Is(err, ErrSandboxNotRetained) {
		t.Fatalf("extend before retirement = %v", err)
	}
	s.RetireRunSandbox(name)
	got, err := s.ExtendLease(row.ID, 120, false, "admin")
	if err != nil {
		t.Fatalf("extend: %v", err)
	}
	if d := time.Until(*got.DestroyAt); d < 119*time.Minute {
		t.Fatalf("extended deadline too short: %v", d)
	}
	// Re-extending replaces the operator's earlier manual lease (can shorten).
	got, _ = s.ExtendLease(row.ID, 5, false, "admin")
	if d := time.Until(*got.DestroyAt); d > 31*time.Minute {
		t.Fatalf("manual lease not replaced: %v", d)
	}

	s.TouchActivity(row.ID)
	db.Where("name = ?", name).First(&row)
	kinds := map[string]bool{}
	for _, l := range row.Leases {
		kinds[l.Kind] = true
	}
	if !kinds[models.LeaseDebug] || !kinds[models.LeaseManual] || !kinds[models.LeaseActivity] {
		t.Fatalf("leases = %+v", row.Leases)
	}
}

func TestConcurrentLeaseWritesKeepEachOther(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)

	name := "grasp-sb-race"
	info := runtimeInfo(name)
	db.Create(&models.Run{ID: info.RunID, Status: "completed", StartedAt: time.Now()})
	s.RegisterRunSandbox(info)
	s.RetireRunSandbox(name)
	var row models.Sandbox
	db.Where("name = ?", name).First(&row)

	for i := 0; i < 20; i++ {
		s.forgetTouch(row.ID)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); s.TouchActivity(row.ID) }()
		go func() { defer wg.Done(); _, _ = s.ExtendLease(row.ID, 60, false, "admin") }()
		go func() {
			defer wg.Done()
			_ = s.AddLeaseByName(name, models.SandboxLease{Kind: models.LeasePreview})
		}()
		wg.Wait()
		db.Where("name = ?", name).First(&row)
		kinds := map[string]bool{}
		for _, l := range row.Leases {
			kinds[l.Kind] = true
		}
		for _, k := range []string{models.LeaseDebug, models.LeaseActivity, models.LeaseManual, models.LeasePreview} {
			if !kinds[k] {
				t.Fatalf("iteration %d lost %s lease: %+v", i, k, row.Leases)
			}
		}
	}
}

// The sweeper's Find snapshot can predate an extension; the destroy decision
// must use the leases as they are when it is made.
func TestSweepUsesCurrentLeasesNotSnapshot(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	name := "grasp-sb-toctou"
	info := runtimeInfo(name)
	db.Create(&models.Run{ID: info.RunID, Status: "completed", StartedAt: time.Now().Add(-3 * time.Hour), DurationSec: 60})
	s.RegisterRunSandbox(info)
	s.RetireRunSandbox(name)
	var row models.Sandbox
	db.Where("name = ?", name).First(&row)
	past := time.Now().Add(-time.Minute)
	db.Model(&models.Sandbox{ID: row.ID}).Select("destroy_at", "leases").Updates(&models.Sandbox{
		DestroyAt: &past,
		Leases:    []models.SandboxLease{{Kind: models.LeaseDebug, Until: &past, CreatedAt: past}},
	})

	var due []models.Sandbox
	db.Where("destroy_at IS NOT NULL AND destroy_at <= ?", time.Now()).Find(&due)
	if len(due) != 1 {
		t.Fatalf("due rows = %d", len(due))
	}
	if _, err := s.ExtendLease(row.ID, 60, false, "admin"); err != nil {
		t.Fatalf("extend: %v", err)
	}
	s.sweepRows(ctx, due)
	if err := db.Where("id = ?", row.ID).First(&row).Error; err != nil {
		t.Fatal("swept a sandbox extended after the sweeper's snapshot")
	}
	if d := time.Until(*row.DestroyAt); d < 59*time.Minute {
		t.Fatalf("sweeper overwrote the extended deadline: %v", d)
	}

	// Once the sweeper has claimed an expired row, a late extension is
	// rejected rather than accepted and then lost to the destroy.
	db.Model(&models.Sandbox{ID: row.ID}).Select("destroy_at", "leases").Updates(&models.Sandbox{
		DestroyAt: &past,
		Leases:    []models.SandboxLease{{Kind: models.LeaseDebug, Until: &past, CreatedAt: past}},
	})
	if !s.claimExpired(row.ID, time.Now()) {
		t.Fatal("expired row not claimed")
	}
	if _, err := s.ExtendLease(row.ID, 60, false, "admin"); !errors.Is(err, ErrSandboxNotRetained) {
		t.Fatalf("extend after claim = %v, want ErrSandboxNotRetained", err)
	}
}
