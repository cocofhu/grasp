package services

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

const platformStatusTimeout = 15 * time.Second

// PlatformStatusQuery controls timezone for the local-calendar today window.
type PlatformStatusQuery struct {
	Timezone         string
	UTCOffsetMinutes *int
	Now              time.Time
}

// PlatformStatusMetrics is the sidebar status payload. Null token fields mean
// no usage has been reported; an explicitly reported zero stays zero.
type PlatformStatusMetrics struct {
	CumulativeTokens *int64                `json:"cumulativeTokens"`
	TodayTokens      *int64                `json:"todayTokens"`
	RunningCount     int64                 `json:"runningCount"`
	QueuedCount      int64                 `json:"queuedCount"`
	AsOf             time.Time             `json:"asOf"`
	Timezone         string                `json:"timezone"`
	Accounting       TokenAccountingStatus `json:"accounting"`
}

// PlatformStatus reads both token totals in one SQL snapshot. The cumulative
// path already scans ledger totals, so a conditional today sum avoids the old
// second scan, materialized usage points, and stale process-local day cache.
func (s *DashboardService) PlatformStatus(ctx context.Context, q PlatformStatusQuery) (PlatformStatusMetrics, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, platformStatusTimeout)
	defer cancel()
	loc, label, err := resolveTokenStatsLocation(q.Timezone, q.UTCOffsetMinutes)
	if err != nil {
		return PlatformStatusMetrics{}, err
	}
	now := q.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := PlatformStatusMetrics{AsOf: now, Timezone: label}
	var solos int64
	for _, item := range []struct {
		model any
		where string
		args  []any
		dest  *int64
	}{
		{&models.Run{}, "status = ?", []any{"running"}, &out.RunningCount},
		{&models.Run{}, "status = ?", []any{"queued"}, &out.QueuedCount},
		// A running Solo only means its environment is alive; it is executing
		// only while its chat queue holds an active turn.
		{&models.SoloSession{}, "status = ? OR (status = ? AND EXISTS (SELECT 1 FROM chat_queues q WHERE q.surface = ? AND q.session_key = solo_sessions.id AND q.active_turn_id <> ''))",
			[]any{models.SoloStatusStarting, models.SoloStatusRunning, soloPendingSurface}, &solos},
	} {
		if err := s.db.WithContext(ctx).Model(item.model).Where(item.where, item.args...).Count(item.dest).Error; err != nil {
			return PlatformStatusMetrics{}, err
		}
	}
	out.RunningCount += solos
	if s.workspace != nil {
		var totals struct {
			Reports    int64
			Cumulative int64
			Today      int64
		}
		start := truncateLocalDay(now.In(loc)).UTC()
		err := s.db.WithContext(ctx).Model(&models.TokenUsageEvent{}).Select(
			"COUNT(*) AS reports, COALESCE(SUM("+ledgerTotalSQL+"), 0) AS cumulative, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? AND created_at <= ? THEN "+ledgerTotalSQL+" ELSE 0 END), 0) AS today", start, now.UTC()).Scan(&totals).Error
		if err != nil {
			return PlatformStatusMetrics{}, err
		}
		if totals.Reports > 0 {
			out.CumulativeTokens = &totals.Cumulative
			out.TodayTokens = &totals.Today
		}
	}
	out.Accounting, err = loadTokenAccountingStatus(ctx, s.db, GlobalTokenStatsQuery{}, windowSlice{end: now.In(loc)})
	return out, err
}
