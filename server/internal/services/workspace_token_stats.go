package services

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// Token-stats window presets and errors.
const (
	TokenStatsWindow24h = "24h"
	TokenStatsWindow7d  = "7d"
	TokenStatsWindow30d = "30d"
	TokenStatsWindow90d = "90d"
	TokenStatsWindowAll = "all"

	TokenStatsBucketHour = "hour"
	TokenStatsBucketDay  = "day"
	TokenStatsBucketWeek = "week"

	// tokenStatsTimeout is the server-side soft deadline for large ledger scans.
	tokenStatsTimeout = 30 * time.Second
)

var (
	// ErrInvalidTokenStatsWindow is returned for unknown window query values.
	ErrInvalidTokenStatsWindow = errors.New("invalid token-stats window")
	// ErrInvalidTokenStatsTimezone is returned when IANA timezone cannot be loaded
	// and no usable utcOffsetMinutes fallback is available.
	ErrInvalidTokenStatsTimezone = errors.New("invalid token-stats timezone")
	// ErrTokenStatsTimeout is returned when aggregation exceeds the soft deadline
	// (clients should treat as retryable).
	ErrTokenStatsTimeout = errors.New("token-stats aggregation timed out")
)

// TokenStatsBucket is one hour, day, or week bucket with totals, source split, and
// four components (sum of all reported sources in the bucket).
type TokenStatsBucket struct {
	Bucket           string `json:"bucket"`
	Total            int64  `json:"total"`
	WorkflowTotal    int64  `json:"workflowTotal"`
	PmTotal          int64  `json:"pmTotal"`
	InputTokens      int64  `json:"inputTokens"`
	OutputTokens     int64  `json:"outputTokens"`
	CacheReadTokens  int64  `json:"cacheReadTokens"`
	CacheWriteTokens int64  `json:"cacheWriteTokens"`
	// StudioTotal / FailedTotal / Cost are populated by global stats only.
	StudioTotal int64   `json:"studioTotal,omitempty"`
	FailedTotal int64   `json:"failedTotal,omitempty"`
	Cost        float64 `json:"cost,omitempty"`
}

// TokenStatsComposition is the four-component sum for the selected window.
type TokenStatsComposition struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	Total            int64 `json:"total"`
}

// Token-stats rank kinds.
const (
	TokenStatsKindWorkflow = "workflow"
	TokenStatsKindPM       = "pm"
	TokenStatsKindOther    = "other"
)

// TokenStatsWorkflow is one consumption-rank row (workflow Top-N, PM, or other).
type TokenStatsWorkflow struct {
	WorkflowID       string `json:"workflowId,omitempty"`
	Name             string `json:"name"`
	Total            int64  `json:"total"`
	InputTokens      int64  `json:"inputTokens,omitempty"`
	OutputTokens     int64  `json:"outputTokens,omitempty"`
	CacheReadTokens  int64  `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64  `json:"cacheWriteTokens,omitempty"`
	Other            bool   `json:"other,omitempty"`
	Kind             string `json:"kind,omitempty"` // workflow | pm | other
	// Cost is the estimated spend (global stats only).
	Cost float64 `json:"cost,omitempty"`
}

// TokenStatsModel is one model-composition / model-ranking row.
type TokenStatsModel struct {
	ModelKey         string `json:"modelKey"`
	Name             string `json:"name"`
	Total            int64  `json:"total"`
	InputTokens      int64  `json:"inputTokens,omitempty"`
	OutputTokens     int64  `json:"outputTokens,omitempty"`
	CacheReadTokens  int64  `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64  `json:"cacheWriteTokens,omitempty"`
	// Unknown marks the「未知/未分桶」bucket. It is a normal model bucket:
	// it appears in ranking only when its total places it in Top10; otherwise
	// its usage is folded into other. other.Unknown must stay false.
	Unknown bool `json:"unknown,omitempty"`
	// Other marks Top10 remainder (all buckets outside Top10, including unknown
	// if it did not qualify). other is not unknown.
	Other bool `json:"other,omitempty"`
	// Filled is true when the bucket includes ACP_BRIDGE_MODEL backfill.
	Filled bool `json:"filled,omitempty"`
	// Source: upstream | via ACP_BRIDGE_MODEL | unknown (omitted for other).
	Source string `json:"source,omitempty"`
	// Cost is the estimated spend (global stats only).
	Cost float64 `json:"cost,omitempty"`
}

// tokenStatsWindowSpec is the parsed window preset.
// Calendar windows (7d/30d/90d) use days; rolling 24h uses duration; all uses neither.
type tokenStatsWindowSpec struct {
	days        int
	duration    time.Duration
	bucketWidth string
}

func parseTokenStatsWindow(window string) (tokenStatsWindowSpec, error) {
	switch window {
	case TokenStatsWindow24h:
		return tokenStatsWindowSpec{duration: 24 * time.Hour, bucketWidth: TokenStatsBucketHour}, nil
	case TokenStatsWindow7d:
		return tokenStatsWindowSpec{days: 7, bucketWidth: TokenStatsBucketDay}, nil
	case TokenStatsWindow30d:
		return tokenStatsWindowSpec{days: 30, bucketWidth: TokenStatsBucketDay}, nil
	case TokenStatsWindow90d:
		return tokenStatsWindowSpec{days: 90, bucketWidth: TokenStatsBucketDay}, nil
	case TokenStatsWindowAll:
		return tokenStatsWindowSpec{bucketWidth: TokenStatsBucketWeek}, nil
	default:
		return tokenStatsWindowSpec{}, ErrInvalidTokenStatsWindow
	}
}

func resolveTokenStatsLocation(iana string, offsetMinutes *int) (*time.Location, string, error) {
	iana = strings.TrimSpace(iana)
	if iana != "" {
		loc, err := time.LoadLocation(iana)
		if err == nil {
			return loc, iana, nil
		}
		// Fall through to offset if provided.
		if offsetMinutes == nil {
			return nil, "", fmt.Errorf("%w: %s", ErrInvalidTokenStatsTimezone, iana)
		}
	}
	if offsetMinutes != nil {
		mins := *offsetMinutes
		if mins < -14*60 || mins > 14*60 {
			return nil, "", ErrInvalidTokenStatsTimezone
		}
		name := fmt.Sprintf("UTC%+d", mins)
		return time.FixedZone(name, mins*60), name, nil
	}
	if iana == "" {
		return time.UTC, "UTC", nil
	}
	return nil, "", ErrInvalidTokenStatsTimezone
}

func truncateLocalDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func truncateLocalHour(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
}

func bucketKey(local time.Time, width string) string {
	if width == TokenStatsBucketHour {
		return local.Format("2006-01-02T15")
	}
	if width == TokenStatsBucketWeek {
		y, w := local.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	}
	return local.Format("2006-01-02")
}

func fillHourBucketKeys(nowLocal, windowStart time.Time, hasStart bool, present map[string]struct{}) []string {
	start := truncateLocalHour(nowLocal)
	if hasStart {
		start = truncateLocalHour(windowStart)
	} else {
		for k := range present {
			t, ok := parseHourKey(k, nowLocal.Location())
			if !ok {
				continue
			}
			if t.Before(start) {
				start = t
			}
		}
	}
	end := truncateLocalHour(nowLocal)
	var keys []string
	for t := start; !t.After(end); t = t.Add(time.Hour) {
		keys = append(keys, bucketKey(t, TokenStatsBucketHour))
	}
	return keys
}

func parseHourKey(key string, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02T15", key, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func fillBucketKeys(nowLocal, windowStart time.Time, hasStart bool, width string, present map[string]struct{}) []string {
	if width == TokenStatsBucketHour {
		return fillHourBucketKeys(nowLocal, windowStart, hasStart, present)
	}
	if width == TokenStatsBucketWeek {
		var start time.Time
		if hasStart {
			start = windowStart
		} else {
			// Earliest present week, or now if somehow empty (caller guards empty).
			start = nowLocal
			for k := range present {
				t, ok := parseWeekKey(k, nowLocal.Location())
				if !ok {
					continue
				}
				if t.Before(start) {
					start = t
				}
			}
		}
		start = startOfISOWeek(start)
		end := startOfISOWeek(nowLocal)
		var keys []string
		for t := start; !t.After(end); t = t.AddDate(0, 0, 7) {
			keys = append(keys, bucketKey(t, TokenStatsBucketWeek))
		}
		return keys
	}

	start := windowStart
	if !hasStart {
		start = truncateLocalDay(nowLocal)
		for k := range present {
			t, err := time.ParseInLocation("2006-01-02", k, nowLocal.Location())
			if err != nil {
				continue
			}
			if t.Before(start) {
				start = t
			}
		}
	}
	end := truncateLocalDay(nowLocal)
	var keys []string
	for t := start; !t.After(end); t = t.AddDate(0, 0, 1) {
		keys = append(keys, t.Format("2006-01-02"))
	}
	return keys
}

func startOfISOWeek(t time.Time) time.Time {
	t = truncateLocalDay(t)
	// Go Weekday: Sunday=0 … Saturday=6; ISO week starts Monday.
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t.AddDate(0, 0, -(wd - 1))
}

func parseWeekKey(key string, loc *time.Location) (time.Time, bool) {
	// "YYYY-Www"
	if len(key) != 8 || key[4] != '-' || key[5] != 'W' {
		return time.Time{}, false
	}
	y, err1 := strconv.Atoi(key[:4])
	w, err2 := strconv.Atoi(key[6:])
	if err1 != nil || err2 != nil || w < 1 || w > 53 {
		return time.Time{}, false
	}
	// Find Monday of ISO week w in year y.
	jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, loc)
	start := startOfISOWeek(jan4).AddDate(0, 0, (w-1)*7)
	return start, true
}

type tokenModelAgg struct {
	total  int64
	filled bool
	source string
}

// buildModelStats builds model composition (all buckets desc by total) and
// Top10 + other ranking. 「未知/未分桶」is a normal bucket: it competes by
// total, appears in ranking only when it ranks in Top10, and is otherwise
// folded into other. other.Unknown is always false.
// unknownAlias (workspace UnknownModelDisplayName) only replaces Name for the
// unknown row; ModelKey stays TokenUsageModelUnknown and Unknown stays true.
func buildModelStats(totals map[string]*tokenModelAgg, unknownAlias string) (composition []TokenStatsModel, ranking []TokenStatsModel) {
	unknownName := ResolveUnknownModelDisplayName(unknownAlias)
	type item struct {
		key    string
		total  int64
		filled bool
		source string
		unk    bool
	}
	list := make([]item, 0, len(totals))
	for k, a := range totals {
		if a == nil {
			continue
		}
		list = append(list, item{
			key:    k,
			total:  a.total,
			filled: a.filled,
			source: a.source,
			unk:    k == models.TokenUsageModelUnknown,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].total != list[j].total {
			return list[i].total > list[j].total
		}
		return list[i].key < list[j].key
	})

	composition = make([]TokenStatsModel, 0, len(list))
	for _, it := range list {
		name := it.key
		if it.unk {
			name = unknownName
		}
		composition = append(composition, TokenStatsModel{
			ModelKey: it.key,
			Name:     name,
			Total:    it.total,
			Unknown:  it.unk,
			Filled:   it.filled,
			Source:   it.source,
		})
	}

	const topN = 10
	ranking = make([]TokenStatsModel, 0, topN+1)
	var other int64
	for i, it := range list {
		if i < topN {
			name := it.key
			if it.unk {
				name = unknownName
			}
			ranking = append(ranking, TokenStatsModel{
				ModelKey: it.key,
				Name:     name,
				Total:    it.total,
				Unknown:  it.unk,
				Filled:   it.filled,
				Source:   it.source,
			})
			continue
		}
		other += it.total
	}
	if other > 0 {
		ranking = append(ranking, TokenStatsModel{
			Name:  "other",
			Total: other,
			Other: true,
		})
	}
	return composition, ranking
}

func buildConsumptionRank(totals map[string]int64, names map[string]string, pmTotal int64, hasPM bool) []TokenStatsWorkflow {
	type item struct {
		id    string
		name  string
		total int64
	}
	list := make([]item, 0, len(totals))
	for id, total := range totals {
		name := names[id]
		if name == "" {
			name = id
		}
		list = append(list, item{id: id, name: name, total: total})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].total != list[j].total {
			return list[i].total > list[j].total
		}
		return list[i].name < list[j].name
	})

	const topN = 10
	out := make([]TokenStatsWorkflow, 0, topN+2)
	var other int64
	for i, it := range list {
		if i < topN {
			id := it.id
			if id == "_unknown" {
				id = ""
			}
			out = append(out, TokenStatsWorkflow{
				WorkflowID: id,
				Name:       it.name,
				Total:      it.total,
				Kind:       TokenStatsKindWorkflow,
			})
			continue
		}
		other += it.total
	}
	if hasPM {
		out = append(out, TokenStatsWorkflow{
			Name:  "PM",
			Total: pmTotal,
			Kind:  TokenStatsKindPM,
		})
	}
	if other > 0 {
		out = append(out, TokenStatsWorkflow{
			Name:  "other",
			Total: other,
			Other: true,
			Kind:  TokenStatsKindOther,
		})
	}
	// Fixed slot order: Top workflows (already desc) → PM (if any) → other (if any).
	// Do not re-sort by Total — that would insert PM between workflow rows (12PM34).
	return out
}
