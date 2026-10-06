package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

const (
	GlobalTokenStatsSourceAll      = "all"
	GlobalTokenStatsSourceWorkflow = "workflow"
	GlobalTokenStatsSourcePM       = "pm"
	GlobalTokenStatsSourceStudio   = "studio"

	// TokenStatsWindowCustom is echoed when an explicit from/to range is used.
	TokenStatsWindowCustom = "custom"

	globalTokenStatsTopProjects  = 10
	globalTokenStatsTopModels    = 10
	globalTokenStatsTopWorkflows = 10
	globalTokenStatsTopRuns      = 20

	// tokenStatsMaxHourSpan caps hour-granularity trends (bucket count guard).
	tokenStatsMaxHourSpan = 14 * 24 * time.Hour

	globalUnassignedProjectName = "未归属项目"
)

// ErrInvalidTokenStatsRange is returned for malformed from/to query values.
var ErrInvalidTokenStatsRange = errors.New("invalid token-stats range")

// GlobalTokenStatsQuery filters cross-project token aggregation.
type GlobalTokenStatsQuery struct {
	Window           string // 24h|7d|30d|90d|all (ignored when From is set)
	From             string // optional local date YYYY-MM-DD (inclusive)
	To               string // optional local date YYYY-MM-DD (inclusive)
	Granularity      string // optional hour|day|week override
	Timezone         string
	UTCOffsetMinutes *int
	Source           string // all|workflow|pm|studio
	Status           string // ok|failed|cancelled
	Phase            string // production|interactive|chat
	ProjectID        string
	ModelKey         string
	WorkflowID       string
	NodeType         string
	RunID            string
	Now              time.Time
}

// GlobalTokenStatsKPI is overview counters for the analytics page.
type GlobalTokenStatsKPI struct {
	Total            int64    `json:"total"`
	PrevTotal        *int64   `json:"prevTotal,omitempty"`
	DeltaPct         *float64 `json:"deltaPct,omitempty"`
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	CacheReadTokens  int64    `json:"cacheReadTokens"`
	CacheWriteTokens int64    `json:"cacheWriteTokens"`
	WorkflowTotal    int64    `json:"workflowTotal"`
	PmTotal          int64    `json:"pmTotal"`
	StudioTotal      int64    `json:"studioTotal"`
	FailedTotal      int64    `json:"failedTotal"`
	ProjectCount     int      `json:"projectCount"`
	RunCount         int      `json:"runCount"`
	ThreadCount      int      `json:"threadCount"`
	ModelCount       int      `json:"modelCount"`
	EventCount       int      `json:"eventCount"`
	// CacheHitRate is cacheRead / (input + cacheRead), 0..1.
	CacheHitRate float64 `json:"cacheHitRate"`
	// AvgPerRun is workflow tokens / distinct runs (0 when no runs).
	AvgPerRun     float64  `json:"avgPerRun"`
	Cost          float64  `json:"cost"`
	PrevCost      *float64 `json:"prevCost,omitempty"`
	CostDeltaPct  *float64 `json:"costDeltaPct,omitempty"`
	UnpricedTotal int64    `json:"unpricedTotal"`
}

// GlobalTokenStatsProjectRow is one project breakdown row.
type GlobalTokenStatsProjectRow struct {
	ProjectID        string   `json:"projectId"`
	Name             string   `json:"name"`
	Total            int64    `json:"total"`
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	CacheReadTokens  int64    `json:"cacheReadTokens"`
	CacheWriteTokens int64    `json:"cacheWriteTokens"`
	Cost             float64  `json:"cost"`
	RunCount         int      `json:"runCount"`
	Deleted          bool     `json:"deleted,omitempty"`
	DeltaPct         *float64 `json:"deltaPct,omitempty"`
}

// GlobalTokenStatsRunRow is a Top-N run consumption row.
type GlobalTokenStatsRunRow struct {
	RunID            string    `json:"runId"`
	Title            string    `json:"title"`
	ProjectID        string    `json:"projectId"`
	ProjectName      string    `json:"projectName"`
	WorkflowID       string    `json:"workflowId,omitempty"`
	WorkflowName     string    `json:"workflowName"`
	ModelKey         string    `json:"modelKey"`
	ModelName        string    `json:"modelName"`
	Total            int64     `json:"total"`
	InputTokens      int64     `json:"inputTokens"`
	OutputTokens     int64     `json:"outputTokens"`
	CacheReadTokens  int64     `json:"cacheReadTokens"`
	CacheWriteTokens int64     `json:"cacheWriteTokens"`
	Cost             float64   `json:"cost"`
	NodeCount        int       `json:"nodeCount"`
	Status           string    `json:"status"`
	FirstAt          time.Time `json:"firstAt"`
}

// GlobalTokenStatsNamedBucket is a generic name→total slice (node types, status…).
type GlobalTokenStatsNamedBucket struct {
	Key              string  `json:"key,omitempty"`
	Name             string  `json:"name"`
	Total            int64   `json:"total"`
	InputTokens      int64   `json:"inputTokens,omitempty"`
	OutputTokens     int64   `json:"outputTokens,omitempty"`
	CacheReadTokens  int64   `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64   `json:"cacheWriteTokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
	Count            int     `json:"count,omitempty"`
	Other            bool    `json:"other,omitempty"`
}

// GlobalTokenStatsHeatmap is model×project matrix (TopN + other).
type GlobalTokenStatsHeatmap struct {
	Rows []string  `json:"rows"`
	Cols []string  `json:"cols"`
	Grid [][]int64 `json:"grid"`
}

// GlobalTokenStatsTreeNode is one node of the project→workflow→nodeType tree.
type GlobalTokenStatsTreeNode struct {
	Key      string                     `json:"key"`
	Name     string                     `json:"name"`
	Kind     string                     `json:"kind"` // project|workflow|pm|studio|nodeType
	Value    int64                      `json:"value"`
	Cost     float64                    `json:"cost,omitempty"`
	Children []GlobalTokenStatsTreeNode `json:"children,omitempty"`
}

// GlobalTokenStatsSeries is a multi-line trend group (Top projects/models).
type GlobalTokenStatsSeries struct {
	Key   string             `json:"key"`
	Name  string             `json:"name"`
	Trend []TokenStatsBucket `json:"trend"`
}

// GlobalTokenStatsFilterOption is a dropdown option for project/model filters.
type GlobalTokenStatsFilterOption struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// GlobalTokenStatsFilterOptions lists available filter values in the window.
type GlobalTokenStatsFilterOptions struct {
	Projects  []GlobalTokenStatsFilterOption `json:"projects"`
	Models    []GlobalTokenStatsFilterOption `json:"models"`
	Workflows []GlobalTokenStatsFilterOption `json:"workflows"`
	NodeTypes []GlobalTokenStatsFilterOption `json:"nodeTypes"`
}

// GlobalTokenStatsRange echoes the resolved current window (local time).
type GlobalTokenStatsRange struct {
	Start *time.Time `json:"start,omitempty"`
	End   time.Time  `json:"end"`
}

// GlobalTokenStatsResult is GET /api/stats/token payload.
type GlobalTokenStatsResult struct {
	Window         string                        `json:"window"`
	BucketWidth    string                        `json:"bucketWidth"`
	Timezone       string                        `json:"timezone"`
	Range          GlobalTokenStatsRange         `json:"range"`
	Currency       string                        `json:"currency"`
	Empty          bool                          `json:"empty"`
	KPI            GlobalTokenStatsKPI           `json:"kpi"`
	Trend          []TokenStatsBucket            `json:"trend"`
	PrevTrend      []TokenStatsBucket            `json:"prevTrend"`
	Composition    TokenStatsComposition         `json:"composition"`
	Projects       []GlobalTokenStatsProjectRow  `json:"projects"`
	ModelRanking   []TokenStatsModel             `json:"modelRanking"`
	NodeTypes      []GlobalTokenStatsNamedBucket `json:"nodeTypes"`
	Sources        []GlobalTokenStatsNamedBucket `json:"sources"`
	Statuses       []GlobalTokenStatsNamedBucket `json:"statuses"`
	Phases         []GlobalTokenStatsNamedBucket `json:"phases"`
	Workflows      []TokenStatsWorkflow          `json:"workflows"`
	Heatmap        GlobalTokenStatsHeatmap       `json:"heatmap"`
	WeekHour       [][]int64                     `json:"weekHour"`
	Tree           []GlobalTokenStatsTreeNode    `json:"tree"`
	TopRuns        []GlobalTokenStatsRunRow      `json:"topRuns"`
	ProjectTrends  []GlobalTokenStatsSeries      `json:"projectTrends"`
	ModelTrends    []GlobalTokenStatsSeries      `json:"modelTrends"`
	UnpricedModels []string                      `json:"unpricedModels"`
	FilterOptions  GlobalTokenStatsFilterOptions `json:"filterOptions"`
}

type globalProjOpt struct {
	id, name string
}

type globalTokenUsageRow struct {
	id           uint
	ts           time.Time
	usage        models.TokenUsage
	byModel      models.TokenUsageByModel
	projectID    string
	projectName  string
	runID        string
	runTitle     string
	workflowID   string
	workflowName string
	nodeID       string
	nodeType     string
	threadID     string
	source       string
	status       string
	phase        string
	// cost is filled after model rebucketing; priced=false when unpriced.
	cost   float64
	priced bool
}

type windowSlice struct {
	start    time.Time
	end      time.Time
	hasStart bool
}

// GlobalTokenStats aggregates ledger usage across all projects with optional filters.
func (s *ProjectService) GlobalTokenStats(ctx context.Context, q GlobalTokenStatsQuery) (GlobalTokenStatsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, tokenStatsTimeout)
	defer cancel()

	loc, tzLabel, err := resolveTokenStatsLocation(q.Timezone, q.UTCOffsetMinutes)
	if err != nil {
		return GlobalTokenStatsResult{}, err
	}
	now := q.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowLocal := now.In(loc)

	plan, err := resolveGlobalWindow(q, nowLocal)
	if err != nil {
		return GlobalTokenStatsResult{}, err
	}
	window, bucketWidth, curWin, prevWin := plan.window, plan.bucketWidth, plan.cur, plan.prev

	sourceFilter := strings.TrimSpace(q.Source)
	if sourceFilter == "" {
		sourceFilter = GlobalTokenStatsSourceAll
	}
	modelFilter := strings.TrimSpace(q.ModelKey)

	pricing := LoadTokenPricing(s.db)

	var since *time.Time
	if prevWin.hasStart {
		t := prevWin.start
		since = &t
	} else if curWin.hasStart {
		t := curWin.start
		since = &t
	}
	rows, err := s.loadLedgerTokenUsageRows(ctx, ledgerRowFilter{since: since})
	if err != nil {
		return GlobalTokenStatsResult{}, err
	}

	unknownAliases := map[string]string{}
	liveProjects := map[string]struct{}{}
	for _, p := range s.List() {
		unknownAliases[p.ID] = ResolveUnknownModelDisplayName(p.UnknownModelDisplayName)
		liveProjects[p.ID] = struct{}{}
	}

	projSeen := map[string]globalProjOpt{}
	modelSeen := map[string]string{}
	wfSeen := map[string]string{}
	nodeTypeSeen := map[string]string{}
	unpriced := map[string]struct{}{}

	var filtered []globalTokenUsageRow
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return GlobalTokenStatsResult{}, ErrTokenStatsTimeout
		}
		u, ok := filterGlobalRowUsage(row, q, sourceFilter, modelFilter, unknownAliases)
		if !ok || u.Total() <= 0 {
			continue
		}
		row.usage = u
		if row.byModel != nil && modelFilter != "" {
			narrowGlobalRowByModel(&row, modelFilter, unknownAliases)
		}
		row.cost, row.priced = 0, true
		for mk, b := range row.byModel {
			effective := rebucketGlobalModelKey(mk, row.projectID, unknownAliases)
			if mk != "" {
				modelSeen[effective] = effective
			}
			c, ok := pricing.Cost(effective, b.AsTokenUsage())
			if !ok {
				row.priced = false
				unpriced[effective] = struct{}{}
				continue
			}
			row.cost += c
		}
		filtered = append(filtered, row)
		projSeen[row.projectID] = globalProjOpt{id: row.projectID, name: row.projectName}
		if row.workflowID != "" {
			wfSeen[row.workflowID] = row.workflowName
		}
		if row.nodeType != "" {
			nodeTypeSeen[row.nodeType] = row.nodeType
		}
	}
	filterOptions := buildFilterOptions(projSeen, modelSeen, wfSeen, nodeTypeSeen)

	rangeOut := GlobalTokenStatsRange{End: curWin.end}
	if curWin.hasStart {
		st := curWin.start
		rangeOut.Start = &st
	}
	base := GlobalTokenStatsResult{
		Window:         window,
		BucketWidth:    bucketWidth,
		Timezone:       tzLabel,
		Range:          rangeOut,
		Currency:       pricing.Currency,
		Empty:          true,
		Trend:          []TokenStatsBucket{},
		PrevTrend:      []TokenStatsBucket{},
		Projects:       []GlobalTokenStatsProjectRow{},
		ModelRanking:   []TokenStatsModel{},
		NodeTypes:      []GlobalTokenStatsNamedBucket{},
		Sources:        []GlobalTokenStatsNamedBucket{},
		Statuses:       []GlobalTokenStatsNamedBucket{},
		Phases:         []GlobalTokenStatsNamedBucket{},
		Workflows:      []TokenStatsWorkflow{},
		Heatmap:        GlobalTokenStatsHeatmap{Rows: []string{}, Cols: []string{}, Grid: [][]int64{}},
		WeekHour:       emptyWeekHour(),
		Tree:           []GlobalTokenStatsTreeNode{},
		TopRuns:        []GlobalTokenStatsRunRow{},
		ProjectTrends:  []GlobalTokenStatsSeries{},
		ModelTrends:    []GlobalTokenStatsSeries{},
		UnpricedModels: []string{},
		FilterOptions:  filterOptions,
	}
	if len(filtered) == 0 {
		return base, nil
	}

	curRows := filterRowsByWindow(filtered, curWin, loc)
	prevRows := filterRowsByWindow(filtered, prevWin, loc)

	trendEnd := nowLocal
	if window == TokenStatsWindowCustom {
		trendEnd = curWin.end
	}
	if len(curRows) == 0 {
		base.Trend = fillTrendBuckets(trendEnd, curWin, bucketWidth, map[string]*tokenBucketAgg{})
		return base, nil
	}

	curAgg := aggregateGlobalRows(curRows, loc, bucketWidth, nowLocal, curWin, unknownAliases)
	prevAgg := aggregateGlobalRows(prevRows, loc, bucketWidth, nowLocal, prevWin, unknownAliases)

	kpi := buildGlobalKPI(curAgg, prevAgg)
	trend := bucketsFromAgg(curAgg.buckets, trendEnd, curWin, bucketWidth)
	prevNow := plan.prevEnd
	prevTrend := bucketsFromAgg(prevAgg.buckets, prevNow, prevWin, bucketWidth)

	projRows, projTrends := buildProjectStats(curAgg, prevAgg, globalTokenStatsTopProjects)
	for i := range projRows {
		if projRows[i].ProjectID == "" {
			projRows[i].Name = globalUnassignedProjectName
			continue
		}
		if _, ok := liveProjects[projRows[i].ProjectID]; !ok {
			projRows[i].Deleted = true
		}
	}
	for i := range projTrends {
		if projTrends[i].Key == "" {
			projTrends[i].Name = globalUnassignedProjectName
		}
	}
	modelRank, modelTrends := buildGlobalModelStats(curAgg, prevAgg, unknownAliases, globalTokenStatsTopModels)
	unpricedList := make([]string, 0, len(unpriced))
	for k := range unpriced {
		if _, ok := curAgg.models[k]; ok {
			unpricedList = append(unpricedList, k)
		}
	}
	sort.Strings(unpricedList)

	out := base
	out.Empty = false
	out.KPI = kpi
	out.Trend = trend
	out.PrevTrend = prevTrend
	out.Composition = curAgg.composition
	out.Projects = projRows
	out.ModelRanking = modelRank
	out.NodeTypes = namedBucketsFromAgg(curAgg.nodeTypes, nil)
	out.Sources = namedBucketsFromAgg(curAgg.sources, nil)
	out.Statuses = namedBucketsFromAgg(curAgg.statuses, nil)
	out.Phases = namedBucketsFromAgg(curAgg.phases, nil)
	out.Workflows = buildGlobalWorkflowRank(curAgg)
	out.Heatmap = buildHeatmap(curAgg, globalTokenStatsTopModels, globalTokenStatsTopProjects)
	out.WeekHour = curAgg.weekHour
	out.Tree = buildTokenTree(curAgg)
	out.TopRuns = buildTopRuns(curAgg, unknownAliases, globalTokenStatsTopRuns)
	out.ProjectTrends = projTrends
	out.ModelTrends = modelTrends
	out.UnpricedModels = unpricedList
	return out, nil
}

type globalWindowPlan struct {
	window      string
	bucketWidth string
	cur, prev   windowSlice
	prevEnd     time.Time
}

// resolveGlobalWindow turns preset / custom range + granularity into slices.
func resolveGlobalWindow(q GlobalTokenStatsQuery, nowLocal time.Time) (globalWindowPlan, error) {
	from := strings.TrimSpace(q.From)
	to := strings.TrimSpace(q.To)
	gran := strings.TrimSpace(q.Granularity)
	if gran != "" && gran != TokenStatsBucketHour && gran != TokenStatsBucketDay && gran != TokenStatsBucketWeek {
		return globalWindowPlan{}, ErrInvalidTokenStatsWindow
	}
	loc := nowLocal.Location()

	if from != "" || to != "" {
		if from == "" {
			return globalWindowPlan{}, ErrInvalidTokenStatsRange
		}
		start, err := time.ParseInLocation("2006-01-02", from, loc)
		if err != nil {
			return globalWindowPlan{}, ErrInvalidTokenStatsRange
		}
		end := nowLocal
		if to != "" {
			tday, err := time.ParseInLocation("2006-01-02", to, loc)
			if err != nil {
				return globalWindowPlan{}, ErrInvalidTokenStatsRange
			}
			if e := tday.AddDate(0, 0, 1).Add(-time.Nanosecond); e.Before(end) {
				end = e
			}
		}
		if !start.Before(end) {
			return globalWindowPlan{}, ErrInvalidTokenStatsRange
		}
		span := end.Sub(start)
		bw := TokenStatsBucketDay
		switch {
		case span <= 2*24*time.Hour:
			bw = TokenStatsBucketHour
		case span > 120*24*time.Hour:
			bw = TokenStatsBucketWeek
		}
		bw = applyGranularity(bw, gran, span, true)
		cur := windowSlice{start: start, end: end, hasStart: true}
		prevEnd := start.Add(-time.Nanosecond)
		prev := windowSlice{start: start.Add(-span - time.Nanosecond), end: prevEnd, hasStart: true}
		return globalWindowPlan{window: TokenStatsWindowCustom, bucketWidth: bw, cur: cur, prev: prev, prevEnd: prevEnd}, nil
	}

	window := strings.TrimSpace(q.Window)
	if window == "" {
		window = TokenStatsWindowAll
	}
	spec, err := parseTokenStatsWindow(window)
	if err != nil {
		return globalWindowPlan{}, err
	}
	cur := buildWindowSlice(nowLocal, spec)
	prev := buildPrevWindowSlice(cur, spec)
	span := time.Duration(0)
	if cur.hasStart {
		span = cur.end.Sub(cur.start)
	}
	bw := applyGranularity(spec.bucketWidth, gran, span, cur.hasStart)
	prevEnd := nowLocal
	if spec.duration > 0 {
		prevEnd = nowLocal.Add(-spec.duration)
	} else if spec.days > 0 {
		prevEnd = nowLocal.AddDate(0, 0, -spec.days)
	}
	return globalWindowPlan{window: window, bucketWidth: bw, cur: cur, prev: prev, prevEnd: prevEnd}, nil
}

// applyGranularity honours an explicit granularity unless it would explode the
// bucket count (hour over an unbounded or > 14 day span falls back).
func applyGranularity(def, gran string, span time.Duration, bounded bool) string {
	if gran == "" {
		return def
	}
	if gran == TokenStatsBucketHour && (!bounded || span > tokenStatsMaxHourSpan) {
		return def
	}
	return gran
}

func buildWindowSlice(nowLocal time.Time, spec tokenStatsWindowSpec) windowSlice {
	if spec.duration > 0 {
		return windowSlice{start: nowLocal.Add(-spec.duration), end: nowLocal, hasStart: true}
	}
	if spec.days <= 0 {
		return windowSlice{end: nowLocal, hasStart: false}
	}
	// Inclusive local-day window: today and the previous (days-1) local days.
	startDay := truncateLocalDay(nowLocal).AddDate(0, 0, -(spec.days - 1))
	return windowSlice{start: startDay, end: nowLocal, hasStart: true}
}

func buildPrevWindowSlice(cur windowSlice, spec tokenStatsWindowSpec) windowSlice {
	if !cur.hasStart {
		return windowSlice{hasStart: false}
	}
	if spec.duration > 0 {
		prevEnd := cur.start.Add(-time.Nanosecond)
		prevStart := cur.start.Add(-spec.duration)
		return windowSlice{start: prevStart, end: prevEnd, hasStart: true}
	}
	if spec.days <= 0 {
		return windowSlice{hasStart: false}
	}
	prevEnd := cur.start.Add(-time.Nanosecond)
	prevStart := truncateLocalDay(prevEnd).AddDate(0, 0, -(spec.days - 1))
	return windowSlice{start: prevStart, end: prevEnd, hasStart: true}
}

func globalModelFilterUsage(row globalTokenUsageRow, modelKey string, unknownAliases map[string]string) (models.TokenUsage, bool) {
	if row.byModel == nil {
		return models.TokenUsage{}, false
	}
	var usage models.TokenUsage
	matched := false
	if b, ok := row.byModel[modelKey]; ok {
		usage.InputTokens += b.InputTokens
		usage.OutputTokens += b.OutputTokens
		usage.CacheReadTokens += b.CacheReadTokens
		usage.CacheWriteTokens += b.CacheWriteTokens
		matched = true
	}
	if b, ok := row.byModel[models.TokenUsageModelUnknown]; ok && modelKey != models.TokenUsageModelUnknown {
		if rebucketGlobalModelKey(models.TokenUsageModelUnknown, row.projectID, unknownAliases) == modelKey {
			usage.InputTokens += b.InputTokens
			usage.OutputTokens += b.OutputTokens
			usage.CacheReadTokens += b.CacheReadTokens
			usage.CacheWriteTokens += b.CacheWriteTokens
			matched = true
		}
	}
	if !matched {
		return models.TokenUsage{}, false
	}
	return usage, true
}

func narrowGlobalRowByModel(row *globalTokenUsageRow, modelKey string, unknownAliases map[string]string) {
	u, ok := globalModelFilterUsage(*row, modelKey, unknownAliases)
	if !ok {
		return
	}
	var merged models.ModelTokenUsage
	if b, ok := row.byModel[modelKey]; ok {
		merged = b
	} else if b, ok := row.byModel[models.TokenUsageModelUnknown]; ok {
		merged = b
	}
	merged.InputTokens = u.InputTokens
	merged.OutputTokens = u.OutputTokens
	merged.CacheReadTokens = u.CacheReadTokens
	merged.CacheWriteTokens = u.CacheWriteTokens
	row.byModel = models.TokenUsageByModel{modelKey: merged}
}

func filterGlobalRowUsage(row globalTokenUsageRow, q GlobalTokenStatsQuery, source, modelKey string, unknownAliases map[string]string) (models.TokenUsage, bool) {
	if pid := strings.TrimSpace(q.ProjectID); pid != "" && row.projectID != pid {
		return models.TokenUsage{}, false
	}
	if source != GlobalTokenStatsSourceAll && row.source != source {
		return models.TokenUsage{}, false
	}
	if st := strings.TrimSpace(q.Status); st != "" && row.status != st {
		return models.TokenUsage{}, false
	}
	if ph := strings.TrimSpace(q.Phase); ph != "" && orDefault(row.phase, models.TokenLedgerPhaseProduction) != ph {
		return models.TokenUsage{}, false
	}
	if wf := strings.TrimSpace(q.WorkflowID); wf != "" && row.workflowID != wf {
		return models.TokenUsage{}, false
	}
	if nt := strings.TrimSpace(q.NodeType); nt != "" && row.nodeType != nt {
		return models.TokenUsage{}, false
	}
	if rid := strings.TrimSpace(q.RunID); rid != "" && row.runID != rid {
		return models.TokenUsage{}, false
	}
	if modelKey != "" {
		return globalModelFilterUsage(row, modelKey, unknownAliases)
	}
	return row.usage, true
}

func filterRowsByWindow(rows []globalTokenUsageRow, win windowSlice, loc *time.Location) []globalTokenUsageRow {
	if !win.hasStart {
		return rows
	}
	out := make([]globalTokenUsageRow, 0, len(rows))
	for _, row := range rows {
		local := row.ts.In(loc)
		if local.Before(win.start) || local.After(win.end) {
			continue
		}
		out = append(out, row)
	}
	return out
}

type tokenBucketAgg struct {
	input, output, cacheRead, cacheWrite int64
	workflow, pm, studio, failed         int64
	cost                                 float64
}

type globalAgg struct {
	buckets      map[string]*tokenBucketAgg
	composition  TokenStatsComposition
	workflowTot  int64
	pmTot        int64
	studioTot    int64
	failedTot    int64
	cost         float64
	unpricedTot  int64
	events       int
	projects     map[string]*globalProjectAgg
	models       map[string]*tokenModelAgg
	modelCost    map[string]float64
	nodeTypes    map[string]*namedAgg
	sources      map[string]*namedAgg
	statuses     map[string]*namedAgg
	phases       map[string]*namedAgg
	workflows    map[string]*globalWorkflowAgg
	wfNames      map[string]string
	runs         map[string]*globalRunAgg
	projBuckets  map[string]map[string]*tokenBucketAgg
	modelBuckets map[string]map[string]*tokenBucketAgg
	heat         map[string]map[string]int64 // modelKey → projectID → total
	runSet       map[string]struct{}
	threadSet    map[string]struct{}
	weekHour     [][]int64
	tree         map[string]*treeAgg // projectID → subtree
}

type globalProjectAgg struct {
	name                  string
	total                 int64
	input, output         int64
	cacheRead, cacheWrite int64
	cost                  float64
	runs                  map[string]struct{}
}

type globalWorkflowAgg struct {
	total                 int64
	input, output         int64
	cacheRead, cacheWrite int64
	cost                  float64
}

type globalRunAgg struct {
	runID, title, projectID, projectName, workflowID, workflowName string
	total                                                          int64
	input, output, cacheRead, cacheWrite                           int64
	cost                                                           float64
	modelTotals                                                    map[string]int64
	nodes                                                          map[string]struct{}
	failed, cancelled                                              bool
	firstAt                                                        time.Time
}

type namedAgg struct {
	total                                int64
	input, output, cacheRead, cacheWrite int64
	cost                                 float64
	count                                int
}

func (n *namedAgg) add(u models.TokenUsage, cost float64) {
	n.total += u.Total()
	n.input += u.InputTokens
	n.output += u.OutputTokens
	n.cacheRead += u.CacheReadTokens
	n.cacheWrite += u.CacheWriteTokens
	n.cost += cost
	n.count++
}

type treeAgg struct {
	name     string
	total    int64
	cost     float64
	children map[string]*treeAgg
	kind     string
}

func (t *treeAgg) child(key, name, kind string) *treeAgg {
	if t.children == nil {
		t.children = map[string]*treeAgg{}
	}
	c := t.children[key]
	if c == nil {
		c = &treeAgg{name: name, kind: kind}
		t.children[key] = c
	}
	if c.name == "" {
		c.name = name
	}
	return c
}

// rebucketGlobalModelKey merges per-project unknown usage into the configured default model.
func rebucketGlobalModelKey(mk, projectID string, unknownAliases map[string]string) string {
	if mk != models.TokenUsageModelUnknown {
		return mk
	}
	alias := strings.TrimSpace(unknownAliases[projectID])
	if IsConfiguredUnknownAlias(alias) {
		return alias
	}
	return mk
}

func emptyWeekHour() [][]int64 {
	out := make([][]int64, 7)
	for i := range out {
		out[i] = make([]int64, 24)
	}
	return out
}

// isoWeekday maps Go weekday onto Monday=0 … Sunday=6.
func isoWeekday(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

func aggregateGlobalRows(rows []globalTokenUsageRow, loc *time.Location, bucketWidth string, nowLocal time.Time, win windowSlice, unknownAliases map[string]string) *globalAgg {
	agg := &globalAgg{
		buckets:      map[string]*tokenBucketAgg{},
		projects:     map[string]*globalProjectAgg{},
		models:       map[string]*tokenModelAgg{},
		modelCost:    map[string]float64{},
		nodeTypes:    map[string]*namedAgg{},
		sources:      map[string]*namedAgg{},
		statuses:     map[string]*namedAgg{},
		phases:       map[string]*namedAgg{},
		workflows:    map[string]*globalWorkflowAgg{},
		wfNames:      map[string]string{},
		runs:         map[string]*globalRunAgg{},
		projBuckets:  map[string]map[string]*tokenBucketAgg{},
		modelBuckets: map[string]map[string]*tokenBucketAgg{},
		heat:         map[string]map[string]int64{},
		runSet:       map[string]struct{}{},
		threadSet:    map[string]struct{}{},
		weekHour:     emptyWeekHour(),
		tree:         map[string]*treeAgg{},
	}
	for _, row := range rows {
		local := row.ts.In(loc)
		if win.hasStart && (local.Before(win.start) || local.After(win.end)) {
			continue
		}
		total := row.usage.Total()
		agg.events++
		key := bucketKey(local, bucketWidth)
		b := agg.buckets[key]
		if b == nil {
			b = &tokenBucketAgg{}
			agg.buckets[key] = b
		}
		addUsageToBucket(b, row.usage, row.source)
		b.cost += row.cost
		if isFailedStatus(row.status) {
			b.failed += total
			agg.failedTot += total
		}
		agg.weekHour[isoWeekday(local)][local.Hour()] += total

		agg.composition.InputTokens += row.usage.InputTokens
		agg.composition.OutputTokens += row.usage.OutputTokens
		agg.composition.CacheReadTokens += row.usage.CacheReadTokens
		agg.composition.CacheWriteTokens += row.usage.CacheWriteTokens
		agg.composition.Total += total
		agg.cost += row.cost
		if !row.priced {
			agg.unpricedTot += total
		}

		switch row.source {
		case TokenStatsKindPM:
			agg.pmTot += total
		case GlobalTokenStatsSourceStudio:
			agg.studioTot += total
		default:
			agg.workflowTot += total
		}
		if row.threadID != "" {
			agg.threadSet[row.threadID] = struct{}{}
		}

		namedAddTo(agg.sources, orDefault(row.source, TokenStatsKindWorkflow), row)
		namedAddTo(agg.statuses, orDefault(row.status, models.TokenLedgerStatusOK), row)
		namedAddTo(agg.phases, orDefault(row.phase, models.TokenLedgerPhaseProduction), row)
		if row.nodeType != "" {
			namedAddTo(agg.nodeTypes, row.nodeType, row)
		}

		pa := agg.projects[row.projectID]
		if pa == nil {
			pa = &globalProjectAgg{name: row.projectName, runs: map[string]struct{}{}}
			agg.projects[row.projectID] = pa
		}
		if pa.name == "" {
			pa.name = row.projectName
		}
		pa.total += total
		pa.input += row.usage.InputTokens
		pa.output += row.usage.OutputTokens
		pa.cacheRead += row.usage.CacheReadTokens
		pa.cacheWrite += row.usage.CacheWriteTokens
		pa.cost += row.cost
		if row.runID != "" {
			pa.runs[row.runID] = struct{}{}
		}

		pb := agg.projBuckets[row.projectID]
		if pb == nil {
			pb = map[string]*tokenBucketAgg{}
			agg.projBuckets[row.projectID] = pb
		}
		pbk := pb[key]
		if pbk == nil {
			pbk = &tokenBucketAgg{}
			pb[key] = pbk
		}
		addUsageToBucket(pbk, row.usage, row.source)
		pbk.cost += row.cost

		addTreeRow(agg.tree, row)

		if row.source == TokenStatsKindWorkflow && row.workflowID != "" {
			wa := agg.workflows[row.workflowID]
			if wa == nil {
				wa = &globalWorkflowAgg{}
				agg.workflows[row.workflowID] = wa
			}
			wa.total += total
			wa.input += row.usage.InputTokens
			wa.output += row.usage.OutputTokens
			wa.cacheRead += row.usage.CacheReadTokens
			wa.cacheWrite += row.usage.CacheWriteTokens
			wa.cost += row.cost
			if row.workflowName != "" {
				agg.wfNames[row.workflowID] = row.workflowName
			}
		}

		var ra *globalRunAgg
		if row.runID != "" {
			agg.runSet[row.runID] = struct{}{}
			ra = agg.runs[row.runID]
			if ra == nil {
				ra = &globalRunAgg{
					runID: row.runID, title: row.runTitle,
					projectID: row.projectID, projectName: row.projectName,
					workflowID: row.workflowID, workflowName: row.workflowName,
					modelTotals: map[string]int64{}, nodes: map[string]struct{}{},
					firstAt: row.ts,
				}
				agg.runs[row.runID] = ra
			}
			if ra.title == "" {
				ra.title = row.runTitle
			}
			ra.total += total
			ra.input += row.usage.InputTokens
			ra.output += row.usage.OutputTokens
			ra.cacheRead += row.usage.CacheReadTokens
			ra.cacheWrite += row.usage.CacheWriteTokens
			ra.cost += row.cost
			if row.nodeID != "" {
				ra.nodes[row.nodeID] = struct{}{}
			}
			if row.status == models.TokenLedgerStatusFailed {
				ra.failed = true
			}
			if row.status == models.TokenLedgerStatusCancelled {
				ra.cancelled = true
			}
			if row.ts.Before(ra.firstAt) {
				ra.firstAt = row.ts
			}
		}

		for mk, bu := range row.byModel {
			tot := bu.Total()
			if tot <= 0 {
				continue
			}
			targetKey := rebucketGlobalModelKey(mk, row.projectID, unknownAliases)
			ma := agg.models[targetKey]
			if ma == nil {
				ma = &tokenModelAgg{}
				agg.models[targetKey] = ma
			}
			ma.total += tot
			if bu.Filled {
				ma.filled = true
			}
			if bu.Source != "" {
				ma.source = bu.Source
			}
			if row.usage.Total() > 0 {
				agg.modelCost[targetKey] += row.cost * float64(tot) / float64(row.usage.Total())
			}

			mb := agg.modelBuckets[targetKey]
			if mb == nil {
				mb = map[string]*tokenBucketAgg{}
				agg.modelBuckets[targetKey] = mb
			}
			mbk := mb[key]
			if mbk == nil {
				mbk = &tokenBucketAgg{}
				mb[key] = mbk
			}
			mbk.input += bu.InputTokens
			mbk.output += bu.OutputTokens
			mbk.cacheRead += bu.CacheReadTokens
			mbk.cacheWrite += bu.CacheWriteTokens
			mbk.workflow += tot

			hm := agg.heat[targetKey]
			if hm == nil {
				hm = map[string]int64{}
				agg.heat[targetKey] = hm
			}
			hm[row.projectID] += tot

			if ra != nil {
				ra.modelTotals[targetKey] += tot
			}
		}
	}
	return agg
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func isFailedStatus(s string) bool {
	return s == models.TokenLedgerStatusFailed || s == models.TokenLedgerStatusCancelled
}

func namedAddTo(m map[string]*namedAgg, key string, row globalTokenUsageRow) {
	n := m[key]
	if n == nil {
		n = &namedAgg{}
		m[key] = n
	}
	n.add(row.usage, row.cost)
}

func addTreeRow(tree map[string]*treeAgg, row globalTokenUsageRow) {
	p := tree[row.projectID]
	if p == nil {
		p = &treeAgg{name: row.projectName, kind: "project"}
		tree[row.projectID] = p
	}
	total := row.usage.Total()
	p.total += total
	p.cost += row.cost
	var mid *treeAgg
	switch row.source {
	case TokenStatsKindPM:
		mid = p.child("_pm", "PM", "pm")
	case GlobalTokenStatsSourceStudio:
		mid = p.child("_studio", "Studio", "studio")
	default:
		wfKey := orDefault(row.workflowID, "_unknown")
		mid = p.child(wfKey, orDefault(row.workflowName, wfKey), "workflow")
	}
	mid.total += total
	mid.cost += row.cost
	if row.source == TokenStatsKindWorkflow || row.source == "" {
		nt := orDefault(row.nodeType, "unknown")
		leaf := mid.child(nt, nt, "nodeType")
		leaf.total += total
		leaf.cost += row.cost
	}
}

func buildTokenTree(cur *globalAgg) []GlobalTokenStatsTreeNode {
	var walk func(key string, t *treeAgg) GlobalTokenStatsTreeNode
	walk = func(key string, t *treeAgg) GlobalTokenStatsTreeNode {
		n := GlobalTokenStatsTreeNode{Key: key, Name: t.name, Kind: t.kind, Value: t.total, Cost: t.cost}
		for ck, c := range t.children {
			n.Children = append(n.Children, walk(ck, c))
		}
		sort.Slice(n.Children, func(i, j int) bool {
			if n.Children[i].Value != n.Children[j].Value {
				return n.Children[i].Value > n.Children[j].Value
			}
			return n.Children[i].Key < n.Children[j].Key
		})
		return n
	}
	out := make([]GlobalTokenStatsTreeNode, 0, len(cur.tree))
	for pid, t := range cur.tree {
		n := walk(pid, t)
		if pid == "" {
			n.Name = globalUnassignedProjectName
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func namedBucketsFromAgg(m map[string]*namedAgg, names map[string]string) []GlobalTokenStatsNamedBucket {
	out := make([]GlobalTokenStatsNamedBucket, 0, len(m))
	for k, a := range m {
		name := k
		if names != nil && names[k] != "" {
			name = names[k]
		}
		out = append(out, GlobalTokenStatsNamedBucket{
			Key: k, Name: name, Total: a.total,
			InputTokens: a.input, OutputTokens: a.output,
			CacheReadTokens: a.cacheRead, CacheWriteTokens: a.cacheWrite,
			Cost: a.cost, Count: a.count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func addUsageToBucket(b *tokenBucketAgg, u models.TokenUsage, source string) {
	b.input += u.InputTokens
	b.output += u.OutputTokens
	b.cacheRead += u.CacheReadTokens
	b.cacheWrite += u.CacheWriteTokens
	tot := u.Total()
	switch source {
	case TokenStatsKindPM:
		b.pm += tot
	case GlobalTokenStatsSourceStudio:
		b.studio += tot
	default:
		b.workflow += tot
	}
}

func bucketsFromAgg(buckets map[string]*tokenBucketAgg, nowLocal time.Time, win windowSlice, bucketWidth string) []TokenStatsBucket {
	present := map[string]struct{}{}
	for k := range buckets {
		present[k] = struct{}{}
	}
	var start time.Time
	hasStart := win.hasStart
	if hasStart {
		start = win.start
	}
	keys := fillBucketKeys(nowLocal, start, hasStart, bucketWidth, present)
	out := make([]TokenStatsBucket, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		if b == nil {
			out = append(out, TokenStatsBucket{Bucket: k})
			continue
		}
		out = append(out, TokenStatsBucket{
			Bucket:           k,
			Total:            b.input + b.output + b.cacheRead + b.cacheWrite,
			WorkflowTotal:    b.workflow,
			PmTotal:          b.pm,
			StudioTotal:      b.studio,
			FailedTotal:      b.failed,
			InputTokens:      b.input,
			OutputTokens:     b.output,
			CacheReadTokens:  b.cacheRead,
			CacheWriteTokens: b.cacheWrite,
			Cost:             b.cost,
		})
	}
	return out
}

func fillTrendBuckets(nowLocal time.Time, win windowSlice, bucketWidth string, buckets map[string]*tokenBucketAgg) []TokenStatsBucket {
	return bucketsFromAgg(buckets, nowLocal, win, bucketWidth)
}

func buildGlobalKPI(cur, prev *globalAgg) GlobalTokenStatsKPI {
	kpi := GlobalTokenStatsKPI{
		Total:            cur.composition.Total,
		InputTokens:      cur.composition.InputTokens,
		OutputTokens:     cur.composition.OutputTokens,
		CacheReadTokens:  cur.composition.CacheReadTokens,
		CacheWriteTokens: cur.composition.CacheWriteTokens,
		WorkflowTotal:    cur.workflowTot,
		PmTotal:          cur.pmTot,
		StudioTotal:      cur.studioTot,
		FailedTotal:      cur.failedTot,
		ProjectCount:     len(cur.projects),
		RunCount:         len(cur.runSet),
		ThreadCount:      len(cur.threadSet),
		ModelCount:       len(cur.models),
		EventCount:       cur.events,
		Cost:             cur.cost,
		UnpricedTotal:    cur.unpricedTot,
	}
	if denom := cur.composition.InputTokens + cur.composition.CacheReadTokens; denom > 0 {
		kpi.CacheHitRate = float64(cur.composition.CacheReadTokens) / float64(denom)
	}
	if n := len(cur.runSet); n > 0 {
		kpi.AvgPerRun = float64(cur.workflowTot) / float64(n)
	}
	if prev != nil && prev.composition.Total > 0 {
		pt := prev.composition.Total
		kpi.PrevTotal = &pt
		delta := (float64(cur.composition.Total-prev.composition.Total) / float64(prev.composition.Total)) * 100
		kpi.DeltaPct = &delta
	}
	if prev != nil && prev.cost > 0 {
		pc := prev.cost
		kpi.PrevCost = &pc
		delta := (cur.cost - prev.cost) / prev.cost * 100
		kpi.CostDeltaPct = &delta
	}
	return kpi
}

func buildProjectStats(cur, prev *globalAgg, topN int) ([]GlobalTokenStatsProjectRow, []GlobalTokenStatsSeries) {
	type item struct {
		id string
		p  *globalProjectAgg
	}
	list := make([]item, 0, len(cur.projects))
	for id, p := range cur.projects {
		list = append(list, item{id: id, p: p})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].p.total != list[j].p.total {
			return list[i].p.total > list[j].p.total
		}
		return list[i].id < list[j].id
	})

	rows := make([]GlobalTokenStatsProjectRow, 0, len(list))
	series := make([]GlobalTokenStatsSeries, 0, topN)
	for i, it := range list {
		p := it.p
		row := GlobalTokenStatsProjectRow{
			ProjectID: it.id, Name: p.name, Total: p.total,
			InputTokens: p.input, OutputTokens: p.output,
			CacheReadTokens: p.cacheRead, CacheWriteTokens: p.cacheWrite,
			Cost: p.cost, RunCount: len(p.runs),
		}
		if prev != nil {
			if pp, ok := prev.projects[it.id]; ok && pp.total > 0 {
				delta := (float64(p.total-pp.total) / float64(pp.total)) * 100
				row.DeltaPct = &delta
			}
		}
		rows = append(rows, row)
		if i < topN {
			series = append(series, GlobalTokenStatsSeries{
				Key: it.id, Name: p.name,
				Trend: projectTrendFromBuckets(cur.projBuckets[it.id]),
			})
		}
	}
	return rows, series
}

func projectTrendFromBuckets(buckets map[string]*tokenBucketAgg) []TokenStatsBucket {
	if len(buckets) == 0 {
		return []TokenStatsBucket{}
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]TokenStatsBucket, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		out = append(out, TokenStatsBucket{
			Bucket:        k,
			Total:         b.input + b.output + b.cacheRead + b.cacheWrite,
			WorkflowTotal: b.workflow,
			PmTotal:       b.pm,
			StudioTotal:   b.studio,
			Cost:          b.cost,
		})
	}
	return out
}

func buildGlobalModelStats(cur, prev *globalAgg, unknownAliases map[string]string, topN int) ([]TokenStatsModel, []GlobalTokenStatsSeries) {
	_, ranking := buildModelStats(cur.models, "")
	topKeys := make(map[string]struct{}, len(ranking))
	for i := range ranking {
		row := &ranking[i]
		if row.Other {
			continue
		}
		topKeys[row.ModelKey] = struct{}{}
		addModelBucketParts(row, cur.modelBuckets[row.ModelKey])
		row.Cost = cur.modelCost[row.ModelKey]
	}
	for i := range ranking {
		if !ranking[i].Other {
			continue
		}
		for key, buckets := range cur.modelBuckets {
			if _, ok := topKeys[key]; ok {
				continue
			}
			addModelBucketParts(&ranking[i], buckets)
			ranking[i].Cost += cur.modelCost[key]
		}
	}
	series := make([]GlobalTokenStatsSeries, 0, topN)
	for i, m := range ranking {
		if m.Other {
			continue
		}
		if i >= topN {
			break
		}
		name := m.Name
		if m.Unknown {
			for _, alias := range unknownAliases {
				if IsConfiguredUnknownAlias(alias) {
					name = alias
					break
				}
			}
		}
		series = append(series, GlobalTokenStatsSeries{
			Key: m.ModelKey, Name: name,
			Trend: projectTrendFromBuckets(cur.modelBuckets[m.ModelKey]),
		})
	}
	return ranking, series
}

func addModelBucketParts(row *TokenStatsModel, buckets map[string]*tokenBucketAgg) {
	for _, bucket := range buckets {
		if bucket == nil {
			continue
		}
		row.InputTokens += bucket.input
		row.OutputTokens += bucket.output
		row.CacheReadTokens += bucket.cacheRead
		row.CacheWriteTokens += bucket.cacheWrite
	}
}

func buildGlobalWorkflowRank(cur *globalAgg) []TokenStatsWorkflow {
	totals := make(map[string]int64, len(cur.workflows))
	for id, agg := range cur.workflows {
		if agg != nil {
			totals[id] = agg.total
		}
	}
	rows := buildConsumptionRank(totals, cur.wfNames, 0, false)
	topIDs := make(map[string]struct{}, len(rows))
	for i := range rows {
		row := &rows[i]
		if row.Other {
			continue
		}
		topIDs[row.WorkflowID] = struct{}{}
		if agg := cur.workflows[row.WorkflowID]; agg != nil {
			setWorkflowParts(row, agg)
		}
	}
	for i := range rows {
		if !rows[i].Other {
			continue
		}
		for id, agg := range cur.workflows {
			if _, ok := topIDs[id]; ok || agg == nil {
				continue
			}
			rows[i].InputTokens += agg.input
			rows[i].OutputTokens += agg.output
			rows[i].CacheReadTokens += agg.cacheRead
			rows[i].CacheWriteTokens += agg.cacheWrite
			rows[i].Cost += agg.cost
		}
	}
	return rows
}

func setWorkflowParts(row *TokenStatsWorkflow, agg *globalWorkflowAgg) {
	row.InputTokens = agg.input
	row.OutputTokens = agg.output
	row.CacheReadTokens = agg.cacheRead
	row.CacheWriteTokens = agg.cacheWrite
	row.Cost = agg.cost
}

func buildHeatmap(cur *globalAgg, topModels, topProjects int) GlobalTokenStatsHeatmap {
	type mItem struct {
		key, name string
		total     int64
	}
	type pItem struct {
		id, name string
		total    int64
	}

	modelList := make([]mItem, 0, len(cur.models))
	for k, a := range cur.models {
		if a == nil {
			continue
		}
		modelList = append(modelList, mItem{key: k, name: k, total: a.total})
	}
	sort.Slice(modelList, func(i, j int) bool {
		if modelList[i].total != modelList[j].total {
			return modelList[i].total > modelList[j].total
		}
		return modelList[i].key < modelList[j].key
	})

	projList := make([]pItem, 0, len(cur.projects))
	for id, p := range cur.projects {
		name := p.name
		if id == "" {
			name = globalUnassignedProjectName
		}
		projList = append(projList, pItem{id: id, name: name, total: p.total})
	}
	sort.Slice(projList, func(i, j int) bool {
		if projList[i].total != projList[j].total {
			return projList[i].total > projList[j].total
		}
		return projList[i].id < projList[j].id
	})

	modelKeys := make([]string, 0, topModels+1)
	modelNames := make([]string, 0, topModels+1)
	var modelOther int64
	for i, m := range modelList {
		if i < topModels {
			modelKeys = append(modelKeys, m.key)
			modelNames = append(modelNames, m.name)
		} else {
			modelOther += m.total
		}
	}
	if modelOther > 0 {
		modelKeys = append(modelKeys, "_other")
		modelNames = append(modelNames, "other")
	}

	projIDs := make([]string, 0, topProjects+1)
	projNames := make([]string, 0, topProjects+1)
	var projOtherID string
	for i, p := range projList {
		if i < topProjects {
			projIDs = append(projIDs, p.id)
			projNames = append(projNames, p.name)
		} else if projOtherID == "" {
			projOtherID = "_other"
		}
	}

	grid := make([][]int64, len(modelKeys))
	for mi, mk := range modelKeys {
		row := make([]int64, len(projIDs)+boolToInt(projOtherID != ""))
		for pi, pid := range projIDs {
			if mk == "_other" {
				var sum int64
				for j, m := range modelList {
					if j >= topModels {
						if hm, ok := cur.heat[m.key]; ok {
							sum += hm[pid]
						}
					}
				}
				row[pi] = sum
			} else if hm, ok := cur.heat[mk]; ok {
				row[pi] = hm[pid]
			}
		}
		if projOtherID != "" {
			idx := len(projIDs)
			var sum int64
			for j, p := range projList {
				if j >= topProjects {
					if mk == "_other" {
						for jj, m := range modelList {
							if jj >= topModels {
								if hm, ok := cur.heat[m.key]; ok {
									sum += hm[p.id]
								}
							}
						}
					} else if hm, ok := cur.heat[mk]; ok {
						sum += hm[p.id]
					}
				}
			}
			row[idx] = sum
		}
		grid[mi] = row
	}

	cols := projNames
	if projOtherID != "" {
		cols = append(cols, "other")
	}
	return GlobalTokenStatsHeatmap{Rows: modelNames, Cols: cols, Grid: grid}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func buildTopRuns(cur *globalAgg, unknownAliases map[string]string, topN int) []GlobalTokenStatsRunRow {
	list := make([]*globalRunAgg, 0, len(cur.runs))
	for _, r := range cur.runs {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].total != list[j].total {
			return list[i].total > list[j].total
		}
		return list[i].runID < list[j].runID
	})
	out := make([]GlobalTokenStatsRunRow, 0, topN)
	for i, it := range list {
		if i >= topN {
			break
		}
		var topKey string
		var topTot int64
		for k, v := range it.modelTotals {
			if v > topTot || (v == topTot && k < topKey) {
				topKey, topTot = k, v
			}
		}
		name := topKey
		if topKey == models.TokenUsageModelUnknown {
			name = unknownAliases[it.projectID]
			if name == "" {
				name = models.TokenUsageModelUnknownDisplay
			}
		}
		title := it.title
		if title == "" {
			title = it.workflowName
		}
		if title == "" {
			title = it.runID
		}
		status := models.TokenLedgerStatusOK
		if it.cancelled {
			status = models.TokenLedgerStatusCancelled
		}
		if it.failed {
			status = models.TokenLedgerStatusFailed
		}
		out = append(out, GlobalTokenStatsRunRow{
			RunID: it.runID, Title: title,
			ProjectID: it.projectID, ProjectName: it.projectName,
			WorkflowID: it.workflowID, WorkflowName: it.workflowName,
			ModelKey: topKey, ModelName: name,
			Total: it.total, InputTokens: it.input, OutputTokens: it.output,
			CacheReadTokens: it.cacheRead, CacheWriteTokens: it.cacheWrite,
			Cost: it.cost, NodeCount: len(it.nodes), Status: status, FirstAt: it.firstAt,
		})
	}
	return out
}

func buildFilterOptions(projects map[string]globalProjOpt, models map[string]string, workflows map[string]string, nodeTypes map[string]string) GlobalTokenStatsFilterOptions {
	pList := make([]GlobalTokenStatsFilterOption, 0, len(projects))
	for _, p := range projects {
		if p.id == "" {
			continue
		}
		pList = append(pList, GlobalTokenStatsFilterOption{Key: p.id, Name: orDefault(p.name, p.id)})
	}
	return GlobalTokenStatsFilterOptions{
		Projects:  sortOptions(pList),
		Models:    sortOptions(optionsFromMap(models)),
		Workflows: sortOptions(optionsFromMap(workflows)),
		NodeTypes: sortOptions(optionsFromMap(nodeTypes)),
	}
}

func optionsFromMap(m map[string]string) []GlobalTokenStatsFilterOption {
	out := make([]GlobalTokenStatsFilterOption, 0, len(m))
	for k, n := range m {
		out = append(out, GlobalTokenStatsFilterOption{Key: k, Name: orDefault(n, k)})
	}
	return out
}

func sortOptions(list []GlobalTokenStatsFilterOption) []GlobalTokenStatsFilterOption {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].Key < list[j].Key
	})
	return list
}
