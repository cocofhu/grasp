package services

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	tokenEventsDefaultPageSize = 50
	tokenEventsMaxPageSize     = 200
)

// TokenUsageEventsQuery pages ledger rows under the same filters as GlobalTokenStats.
type TokenUsageEventsQuery struct {
	GlobalTokenStatsQuery
	Page     int
	PageSize int
	Sort     string // time (default) | total | cost
}

// TokenUsageEventRow is one ledger row as shown in the drill-down list.
type TokenUsageEventRow struct {
	ID               uint      `json:"id"`
	At               time.Time `json:"at"`
	Source           string    `json:"source"`
	Phase            string    `json:"phase,omitempty"`
	Status           string    `json:"status"`
	ProjectID        string    `json:"projectId,omitempty"`
	ProjectName      string    `json:"projectName,omitempty"`
	WorkflowID       string    `json:"workflowId,omitempty"`
	WorkflowName     string    `json:"workflowName,omitempty"`
	RunID            string    `json:"runId,omitempty"`
	RunTitle         string    `json:"runTitle,omitempty"`
	NodeID           string    `json:"nodeId,omitempty"`
	NodeType         string    `json:"nodeType,omitempty"`
	ThreadID         string    `json:"threadId,omitempty"`
	ModelKey         string    `json:"modelKey"`
	Total            int64     `json:"total"`
	InputTokens      int64     `json:"inputTokens"`
	OutputTokens     int64     `json:"outputTokens"`
	CacheReadTokens  int64     `json:"cacheReadTokens"`
	CacheWriteTokens int64     `json:"cacheWriteTokens"`
	Cost             float64   `json:"cost"`
	Priced           bool      `json:"priced"`
}

// TokenUsageEventsResult is GET /api/stats/token/events payload.
type TokenUsageEventsResult struct {
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
	Currency string               `json:"currency"`
	Items    []TokenUsageEventRow `json:"items"`
}

// TokenUsageEvents lists ledger rows in the current window under the stats filters.
func (s *ProjectService) TokenUsageEvents(ctx context.Context, q TokenUsageEventsQuery) (TokenUsageEventsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, tokenStatsTimeout)
	defer cancel()

	loc, _, err := resolveTokenStatsLocation(q.Timezone, q.UTCOffsetMinutes)
	if err != nil {
		return TokenUsageEventsResult{}, err
	}
	now := q.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	plan, err := resolveGlobalWindow(q.GlobalTokenStatsQuery, now.In(loc))
	if err != nil {
		return TokenUsageEventsResult{}, err
	}
	var since *time.Time
	if plan.cur.hasStart {
		t := plan.cur.start
		since = &t
	}
	rows, err := s.loadLedgerTokenUsageRows(ctx, ledgerRowFilter{since: since})
	if err != nil {
		return TokenUsageEventsResult{}, err
	}
	rows = filterRowsByWindow(rows, plan.cur, loc)

	unknownAliases := map[string]string{}
	for _, p := range s.List() {
		unknownAliases[p.ID] = ResolveUnknownModelDisplayName(p.UnknownModelDisplayName)
	}
	source := strings.TrimSpace(q.Source)
	if source == "" {
		source = GlobalTokenStatsSourceAll
	}
	modelFilter := strings.TrimSpace(q.ModelKey)
	pricing := LoadTokenPricing(s.db)

	items := make([]TokenUsageEventRow, 0, len(rows))
	for _, row := range rows {
		u, ok := filterGlobalRowUsage(row, q.GlobalTokenStatsQuery, source, modelFilter, unknownAliases)
		if !ok || u.Total() <= 0 {
			continue
		}
		var mk string
		for k := range row.byModel {
			mk = rebucketGlobalModelKey(k, row.projectID, unknownAliases)
		}
		cost, priced := pricing.Cost(mk, u)
		items = append(items, TokenUsageEventRow{
			ID: row.id, At: row.ts.In(loc), Source: row.source, Phase: row.phase, Status: row.status,
			ProjectID: row.projectID, ProjectName: orDefault(row.projectName, globalUnassignedProjectName),
			WorkflowID: row.workflowID, WorkflowName: row.workflowName,
			RunID: row.runID, RunTitle: row.runTitle, NodeID: row.nodeID, NodeType: row.nodeType,
			ThreadID: row.threadID, ModelKey: mk, Total: u.Total(),
			InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
			Cost: cost, Priced: priced,
		})
	}

	switch q.Sort {
	case "total":
		sort.SliceStable(items, func(i, j int) bool { return items[i].Total > items[j].Total })
	case "cost":
		sort.SliceStable(items, func(i, j int) bool { return items[i].Cost > items[j].Cost })
	default:
		sort.SliceStable(items, func(i, j int) bool {
			if !items[i].At.Equal(items[j].At) {
				return items[i].At.After(items[j].At)
			}
			return items[i].ID > items[j].ID
		})
	}

	size := q.PageSize
	if size <= 0 {
		size = tokenEventsDefaultPageSize
	}
	if size > tokenEventsMaxPageSize {
		size = tokenEventsMaxPageSize
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	start := (page - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return TokenUsageEventsResult{
		Total: len(items), Page: page, PageSize: size,
		Currency: pricing.Currency, Items: items[start:end],
	}, nil
}
