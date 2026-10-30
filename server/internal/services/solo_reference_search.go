package services

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

const soloReferencePageSize = 20

// ReferenceSearchQuery is one picker lane request. Lane is "solo" or
// "github"; Kinds narrows GitHub to github_issue and/or github_pr.
type ReferenceSearchQuery struct {
	Lane  string
	Query string
	Kinds []string
	State string
	Page  int
}

// SoloReferenceItem is a session candidate. It never carries message text.
type SoloReferenceItem struct {
	Kind         string     `json:"kind"`
	Target       string     `json:"target"`
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	LastActiveAt *time.Time `json:"lastActiveAt,omitempty"`
}

// ReferenceSearchError is a lane failure shown inside that lane only.
type ReferenceSearchError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	RetryAfterMs int64  `json:"retryAfterMs,omitempty"`
}

// ReferenceSearchResult holds exactly one lane.
type ReferenceSearchResult struct {
	Lane       string                `json:"lane"`
	Items      any                   `json:"items"`
	Total      *int                  `json:"total,omitempty"`
	NextPage   int                   `json:"nextPage,omitempty"`
	Incomplete bool                  `json:"incomplete,omitempty"`
	Source     string                `json:"source,omitempty"`
	Hint       string                `json:"hint,omitempty"`
	RateLimit  *githubRateLimit      `json:"rateLimit,omitempty"`
	Error      *ReferenceSearchError `json:"error,omitempty"`
}

// SearchReferences lists @ candidates for session id. Sessions are the
// user's own, matched by title only; GitHub stays on the session's repos
// except for exact targets.
func (s *SoloService) SearchReferences(ctx context.Context, id, user string, q ReferenceSearchQuery) (ReferenceSearchResult, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return ReferenceSearchResult{}, err
	}
	switch q.Lane {
	case "solo":
		return s.searchSoloReferences(row, q)
	case "github":
		return s.searchGitHubReferences(ctx, row, q), nil
	}
	return ReferenceSearchResult{}, soloErr(http.StatusBadRequest, "reference_lane_invalid", "lane 必须是 solo 或 github")
}

func (s *SoloService) searchSoloReferences(row *models.SoloSession, q ReferenceSearchQuery) (ReferenceSearchResult, error) {
	page := max(q.Page, 1)
	db := s.db.Model(&models.SoloSession{}).Select("id", "title", "status", "last_active_at").
		Where("user_id = ? AND id <> ?", row.UserID, row.ID)
	if term := strings.TrimSpace(q.Query); term != "" {
		db = db.Where("title LIKE ? ESCAPE ?", "%"+escapeLikePattern(term)+"%", `\`)
	}
	var rows []models.SoloSession
	if err := db.Order("COALESCE(last_active_at, updated_at) desc, id desc").
		Offset((page - 1) * soloReferencePageSize).Limit(soloReferencePageSize + 1).Find(&rows).Error; err != nil {
		return ReferenceSearchResult{}, err
	}
	res := ReferenceSearchResult{Lane: "solo", Source: "search"}
	if len(rows) > soloReferencePageSize {
		rows = rows[:soloReferencePageSize]
		res.NextPage = page + 1
	}
	items := make([]SoloReferenceItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, SoloReferenceItem{Kind: models.ReferenceKindSolo, Target: r.ID, Title: r.Title, Status: r.Status, LastActiveAt: r.LastActiveAt})
	}
	res.Items = items
	return res, nil
}

func (s *SoloService) searchGitHubReferences(ctx context.Context, row *models.SoloSession, q ReferenceSearchQuery) ReferenceSearchResult {
	repos := row.Repos
	if len(repos) == 0 {
		repos = legacySessionRepos(row)
	}
	tokens := map[githubRepo]string{}
	tokenOf := func(r githubRepo) string {
		if t, ok := tokens[r]; ok {
			return t
		}
		t := s.githubToken(row, r.Owner, r.Repo)
		tokens[r] = t
		return t
	}
	found, err := s.githubClient().Search(ctx, githubSearchRequest{
		Query: q.Query, Kinds: q.Kinds, State: q.State, Page: q.Page, Repos: sessionGitHubRepos(repos), TokenOf: tokenOf,
	})
	if err != nil {
		res := ReferenceSearchResult{Lane: "github", Items: []githubItem{}, Error: &ReferenceSearchError{Code: githubErrCode(err), Message: err.Error()}}
		if res.Error.Code == "" {
			res.Error.Code = githubErrUpstream
		}
		if ge, ok := err.(*githubError); ok {
			res.Error.RetryAfterMs = ge.RetryAfter.Milliseconds()
		}
		return res
	}
	return ReferenceSearchResult{Lane: "github", Items: found.Items, Total: found.Total, NextPage: found.NextPage, Incomplete: found.Incomplete,
		Source: found.Source, Hint: found.Hint, RateLimit: found.RateLimit}
}
