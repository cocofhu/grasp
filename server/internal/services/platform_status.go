package services

import (
	"context"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

const (
	platformStatusTTL     = 12 * time.Second
	platformStatusTimeout = 15 * time.Second
)

// PlatformStatusQuery controls timezone for the local-calendar "today" window.
type PlatformStatusQuery struct {
	Timezone         string
	UTCOffsetMinutes *int
	Now              time.Time // tests may inject; zero → time.Now().UTC()
}

// PlatformStatusMetrics is the AppTopbar StatusMetrics payload.
// Token fields use pointers: JSON null = unavailable / never reported; 0 is a real zero.
type PlatformStatusMetrics struct {
	CumulativeTokens *int64    `json:"cumulativeTokens"`
	TodayTokens      *int64    `json:"todayTokens"`
	RunningCount     int64     `json:"runningCount"`
	QueuedCount      int64     `json:"queuedCount"`
	AsOf             time.Time `json:"asOf"`
	Timezone         string    `json:"timezone"`
}

type platformStatusCacheEntry struct {
	metrics PlatformStatusMetrics
	expires time.Time
}

type platformStatusCall struct {
	wg  sync.WaitGroup
	val PlatformStatusMetrics
}

type platformUsagePoint struct {
	ts    time.Time
	total int64
}

// PlatformStatus returns running/queued counts, cumulative tokens, and today's
// token sum (client timezone midnight–asOf). Today aggregation is process-cached
// (TTL≈12s) with singleflight so topbar polling does not re-scan every request.
func (s *DashboardService) PlatformStatus(ctx context.Context, q PlatformStatusQuery) (PlatformStatusMetrics, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	loc, tzLabel, err := resolveTokenStatsLocation(q.Timezone, q.UTCOffsetMinutes)
	if err != nil {
		return PlatformStatusMetrics{}, err
	}

	now := q.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// Cheap counts always fresh; today-token path may hit cache.
	running, queued := s.countRunStatuses()
	var cumulative *int64
	if s.projects != nil {
		cumulative = s.projects.PlatformTokenBreakdown().Total
	}

	today, err := s.cachedTodayTokens(ctx, tzLabel, loc, now)
	if err != nil {
		return PlatformStatusMetrics{}, err
	}

	out := PlatformStatusMetrics{
		CumulativeTokens: cumulative,
		RunningCount:     running,
		QueuedCount:      queued,
		AsOf:             now,
		Timezone:         tzLabel,
	}
	if cumulative == nil {
		// Never reported → today stays null (UI "—"), not a fake zero.
		out.TodayTokens = nil
	} else {
		out.TodayTokens = today
	}
	return out, nil
}

type todayTokenBundle struct {
	todayTokens *int64
}

func (s *DashboardService) countRunStatuses() (running, queued int64) {
	count := func(status string) int64 {
		var n int64
		s.db.Model(&models.Run{}).Where("status = ?", status).Count(&n)
		return n
	}
	return count("running"), count("queued")
}

func (s *DashboardService) cachedTodayTokens(ctx context.Context, cacheKey string, loc *time.Location, now time.Time) (*int64, error) {
	s.statusMu.Lock()
	if s.statusCache == nil {
		s.statusCache = map[string]platformStatusCacheEntry{}
	}
	if ent, ok := s.statusCache[cacheKey]; ok && now.Before(ent.expires) {
		m := ent.metrics
		s.statusMu.Unlock()
		return m.TodayTokens, nil
	}
	if s.statusInflight == nil {
		s.statusInflight = map[string]*platformStatusCall{}
	}
	if call, ok := s.statusInflight[cacheKey]; ok {
		s.statusMu.Unlock()
		call.wg.Wait()
		return call.val.TodayTokens, nil
	}
	call := &platformStatusCall{}
	call.wg.Add(1)
	s.statusInflight[cacheKey] = call
	s.statusMu.Unlock()

	bundle, err := s.computeTodayTokens(ctx, loc, now)
	metrics := PlatformStatusMetrics{
		TodayTokens: bundle.todayTokens,
		AsOf:        now,
		Timezone:    cacheKey,
	}

	s.statusMu.Lock()
	if err == nil {
		s.statusCache[cacheKey] = platformStatusCacheEntry{
			metrics: metrics,
			expires: now.Add(platformStatusTTL),
		}
	}
	delete(s.statusInflight, cacheKey)
	call.val = metrics
	call.wg.Done()
	s.statusMu.Unlock()

	if err != nil {
		return nil, err
	}
	return bundle.todayTokens, nil
}

func (s *DashboardService) computeTodayTokens(ctx context.Context, loc *time.Location, now time.Time) (todayTokenBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, platformStatusTimeout)
	defer cancel()

	nowLocal := now.In(loc)
	dayStart := truncateLocalDay(nowLocal)

	// Scan from before local midnight (UTC instant) so timezone edges are complete.
	points, err := s.loadPlatformUsageSince(ctx, dayStart.UTC().Add(-14*time.Hour))
	if err != nil {
		return todayTokenBundle{}, err
	}

	var sum int64
	for _, p := range points {
		local := p.ts.In(loc)
		if local.Before(dayStart) || local.After(nowLocal) {
			continue
		}
		sum += p.total
	}
	return todayTokenBundle{todayTokens: &sum}, nil
}

func (s *DashboardService) loadPlatformUsageSince(ctx context.Context, since time.Time) ([]platformUsagePoint, error) {
	if s.loadPlatformUsageHook != nil {
		s.loadPlatformUsageHook()
	}
	var out []platformUsagePoint
	err := loadLedgerEvents(ctx, s.db, ledgerRowFilter{since: &since}, func(ev models.TokenUsageEvent) {
		out = append(out, platformUsagePoint{ts: ev.CreatedAt, total: ev.Total()})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ClearPlatformStatusCacheForTest resets the today-token cache (tests only).
func (s *DashboardService) ClearPlatformStatusCacheForTest() {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.statusCache = nil
	s.statusInflight = nil
}
