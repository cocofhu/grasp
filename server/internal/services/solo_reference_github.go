package services

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

const (
	githubAPIBase        = "https://api.github.com"
	githubCacheTTL       = 60 * time.Second
	githubCacheSize      = 500
	githubRequestTimeout = 3 * time.Second
	githubPageSize       = 20
	githubMaxDepth       = 100
	// githubSearchReserve keeps search calls back so exact lookups still work
	// before GitHub starts rejecting every request.
	githubSearchReserve = 5
)

const githubAgentGuide = "GitHub 内容请用上面的命令读取最新版本。评论很多时用\n" +
	"  gh api repos/<o>/<r>/issues/<n>/comments --paginate --jq '.[] | {user: .user.login, created_at, body}'\n" +
	"图片和录屏：私有仓库的附件先取\n" +
	"  gh api -H 'Accept: application/vnd.github.full+json' repos/<o>/<r>/issues/<n> --jq .body_html\n" +
	"里的 private-user-images 链接（5 分钟有效），再 curl 下载后用读文件工具打开。\n" +
	"沙箱里 gh 未登录且仓库公开时，可用 curl https://api.github.com/repos/<o>/<r>/issues/<n> 匿名读取。\n"

// Error codes mirror the reference-provider plugin protocol.
const (
	githubErrNotFound     = "not_found"
	githubErrUnauthorized = "unauthorized"
	githubErrForbidden    = "forbidden"
	githubErrRateLimited  = "rate_limited"
	githubErrTimeout      = "timeout"
	githubErrUpstream     = "upstream"
	githubErrInvalid      = "invalid"
)

type githubError struct {
	Code       string
	Msg        string
	RetryAfter time.Duration
}

func (e *githubError) Error() string { return e.Msg }

func githubErrCode(err error) string {
	var ge *githubError
	if errors.As(err, &ge) {
		return ge.Code
	}
	return ""
}

// isGitHubDenied reports an access-check failure the credential explains;
// timeouts and rate limits leave the reference unchecked instead.
func isGitHubDenied(err error) bool {
	switch githubErrCode(err) {
	case githubErrNotFound, githubErrUnauthorized, githubErrForbidden:
		return true
	}
	return false
}

var (
	githubName     = `[A-Za-z0-9_.-]+`
	githubURLRe    = regexp.MustCompile(`^https?://github\.com/(` + githubName + `)/(` + githubName + `)/(issues|pull)/(\d+)`)
	githubShortRe  = regexp.MustCompile(`^(?:(` + githubName + `)/(` + githubName + `))?#(\d+)$`)
	githubRemoteRe = regexp.MustCompile(`(?i)^(?:https?://github\.com/|git@github\.com:|ssh://git@github\.com/)(` + githubName + `)/(` + githubName + `?)(?:\.git)?/?$`)
)

type githubRepo struct {
	Owner, Repo string
}

type githubTarget struct {
	Owner, Repo string
	Number      int
}

func (t githubTarget) String() string { return fmt.Sprintf("%s/%s#%d", t.Owner, t.Repo, t.Number) }

func (t githubTarget) URL(kind string) string {
	segment := "issues"
	if kind == models.ReferenceKindGitHubPR {
		segment = "pull"
	}
	return fmt.Sprintf("https://github.com/%s/%s/%s/%d", t.Owner, t.Repo, segment, t.Number)
}

// parseGitHubTarget accepts a URL, owner/repo#n, or #n when the session has
// exactly one GitHub repository to resolve it against.
func parseGitHubTarget(text string, defaultRepo *githubRepo) (githubTarget, bool) {
	text = strings.TrimSpace(text)
	if m := githubURLRe.FindStringSubmatch(text); m != nil {
		n, err := strconv.Atoi(m[4])
		return githubTarget{Owner: m[1], Repo: m[2], Number: n}, err == nil && n > 0
	}
	m := githubShortRe.FindStringSubmatch(text)
	if m == nil {
		return githubTarget{}, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return githubTarget{}, false
	}
	if m[1] != "" {
		return githubTarget{Owner: m[1], Repo: m[2], Number: n}, true
	}
	if defaultRepo == nil {
		return githubTarget{}, false
	}
	return githubTarget{Owner: defaultRepo.Owner, Repo: defaultRepo.Repo, Number: n}, true
}

// sessionGitHubRepos keeps github.com repositories only; other hosts belong
// to other providers.
func sessionGitHubRepos(repos []models.SessionRepo) []githubRepo {
	var out []githubRepo
	for _, r := range repos {
		m := githubRemoteRe.FindStringSubmatch(strings.TrimSpace(r.RepoURL))
		if m == nil {
			continue
		}
		repo := githubRepo{Owner: m[1], Repo: strings.TrimSuffix(m[2], ".git")}
		dup := false
		for _, x := range out {
			dup = dup || (strings.EqualFold(x.Owner, repo.Owner) && strings.EqualFold(x.Repo, repo.Repo))
		}
		if !dup {
			out = append(out, repo)
		}
	}
	return out
}

// githubItem is one issue or pull request as the picker shows it.
type githubItem struct {
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Title     string `json:"title"`
	State     string `json:"state"`
	URL       string `json:"url"`
	Comments  int    `json:"comments"`
	UpdatedAt string `json:"updatedAt"`
	Repo      string `json:"repo"`
	Number    int    `json:"number"`
}

type githubIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	Comments    int    `json:"comments"`
	UpdatedAt   string `json:"updated_at"`
	RepoURL     string `json:"repository_url"`
	PullRequest *struct {
		MergedAt *string `json:"merged_at"`
	} `json:"pull_request"`
}

func (it githubIssue) item(t githubTarget) githubItem {
	kind, state := models.ReferenceKindGitHubIssue, it.State
	if it.PullRequest != nil {
		kind = models.ReferenceKindGitHubPR
		if it.PullRequest.MergedAt != nil && *it.PullRequest.MergedAt != "" {
			state = "merged"
		}
	}
	return githubItem{Kind: kind, Target: t.String(), Title: it.Title, State: state, URL: it.HTMLURL, Comments: it.Comments,
		UpdatedAt: it.UpdatedAt, Repo: t.Owner + "/" + t.Repo, Number: t.Number}
}

type githubRateLimit struct {
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"resetAt"`
}

type githubCacheEntry struct {
	key  string
	body []byte
	etag string
	at   time.Time
}

// githubRefClient is the only place that talks to the GitHub API for
// references. Tokens arrive per call and are kept only as hashed cache keys.
type githubRefClient struct {
	http    *http.Client
	apiBase string
	now     func() time.Time

	mu    sync.Mutex
	order *list.List
	cache map[string]*list.Element
	rate  map[string]githubRateLimit
}

func newGitHubRefClient(apiBase string, client *http.Client) *githubRefClient {
	if client == nil {
		client = &http.Client{}
	}
	return &githubRefClient{http: client, apiBase: strings.TrimRight(apiBase, "/"), now: time.Now,
		order: list.New(), cache: map[string]*list.Element{}, rate: map[string]githubRateLimit{}}
}

func githubTokenKey(token string) string {
	if token == "" {
		return "anon"
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

func (c *githubRefClient) cached(key string) (githubCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.cache[key]
	if !ok {
		return githubCacheEntry{}, false
	}
	c.order.MoveToFront(el)
	return *el.Value.(*githubCacheEntry), true
}

func (c *githubRefClient) store(entry githubCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.cache[entry.key]; ok {
		el.Value = &entry
		c.order.MoveToFront(el)
		return
	}
	c.cache[entry.key] = c.order.PushFront(&entry)
	for c.order.Len() > githubCacheSize {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.cache, last.Value.(*githubCacheEntry).key)
	}
}

func (c *githubRefClient) rateLimit(token, resource string) (githubRateLimit, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.rate[githubTokenKey(token)+":"+resource]
	return r, ok
}

func (c *githubRefClient) track(token string, h http.Header) {
	remaining := h.Get("X-Ratelimit-Remaining")
	if remaining == "" {
		return
	}
	n, err := strconv.Atoi(remaining)
	if err != nil {
		return
	}
	resource := h.Get("X-Ratelimit-Resource")
	if resource == "" {
		resource = "core"
	}
	reset, _ := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64)
	c.mu.Lock()
	c.rate[githubTokenKey(token)+":"+resource] = githubRateLimit{Remaining: n, ResetAt: time.Unix(reset, 0)}
	c.mu.Unlock()
}

func githubRateLimited(wait time.Duration) *githubError {
	seconds := int((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return &githubError{Code: githubErrRateLimited, RetryAfter: time.Duration(seconds) * time.Second,
		Msg: fmt.Sprintf("GitHub 搜索已限流，%ds 后恢复；仍可输入 #编号 或粘贴链接", seconds)}
}

// get fetches path with a fresh cache window, then revalidates with ETag so
// a 304 costs no rate limit.
func (c *githubRefClient) get(ctx context.Context, path, token string, out any) error {
	key := githubTokenKey(token) + " " + path
	hit, ok := c.cached(key)
	if ok && c.now().Sub(hit.at) < githubCacheTTL {
		return json.Unmarshal(hit.body, out)
	}
	ctx, cancel := context.WithTimeout(ctx, githubRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+path, nil)
	if err != nil {
		return &githubError{Code: githubErrUpstream, Msg: "GitHub 请求无效"}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "grasp-references")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ok && hit.etag != "" {
		req.Header.Set("If-None-Match", hit.etag)
	}
	res, err := c.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &githubError{Code: githubErrTimeout, Msg: fmt.Sprintf("GitHub 请求超时（%s）", githubRequestTimeout)}
		}
		return &githubError{Code: githubErrUpstream, Msg: "GitHub 请求失败"}
	}
	defer func() { _ = res.Body.Close() }()
	c.track(token, res.Header)
	switch {
	case res.StatusCode == http.StatusNotModified && ok:
		hit.at = c.now()
		c.store(hit)
		return json.Unmarshal(hit.body, out)
	case res.StatusCode == http.StatusNotFound:
		return &githubError{Code: githubErrNotFound, Msg: "GitHub 404：仓库不可见或编号不存在"}
	case res.StatusCode == http.StatusUnauthorized:
		return &githubError{Code: githubErrUnauthorized, Msg: "GitHub 凭据无效或已过期"}
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		if retry, _ := strconv.Atoi(res.Header.Get("Retry-After")); retry > 0 {
			return githubRateLimited(time.Duration(retry) * time.Second)
		}
		if res.Header.Get("X-Ratelimit-Remaining") == "0" {
			reset, _ := strconv.ParseInt(res.Header.Get("X-Ratelimit-Reset"), 10, 64)
			return githubRateLimited(time.Unix(reset, 0).Sub(c.now()))
		}
		return &githubError{Code: githubErrForbidden, Msg: "GitHub 拒绝访问：凭据没有该仓库的读权限"}
	case res.StatusCode < 200 || res.StatusCode > 299:
		return &githubError{Code: githubErrUpstream, Msg: fmt.Sprintf("GitHub 返回 %d", res.StatusCode)}
	}
	var body json.RawMessage
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&body); err != nil {
		return &githubError{Code: githubErrUpstream, Msg: "GitHub 响应无法解析"}
	}
	c.store(githubCacheEntry{key: key, body: body, etag: res.Header.Get("ETag"), at: c.now()})
	return json.Unmarshal(body, out)
}

// Exact looks up one issue or pull request. It is not limited to the
// session's repositories: what the credential can read decides.
func (c *githubRefClient) Exact(ctx context.Context, t githubTarget, token string) (githubItem, error) {
	var it githubIssue
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(t.Owner), url.PathEscape(t.Repo), t.Number), token, &it); err != nil {
		return githubItem{}, err
	}
	return it.item(t), nil
}

// githubSearchRequest is one picker query against the session's repositories.
type githubSearchRequest struct {
	Query   string
	Kinds   []string
	State   string
	Page    int
	Repos   []githubRepo
	TokenOf func(githubRepo) string
}

// githubCounts is how many issues and pull requests match the repositories,
// state and query, whatever kind the picker shows. A missing field means the
// count could not be fetched.
type githubCounts struct {
	Issue *int `json:"issue,omitempty"`
	PR    *int `json:"pr,omitempty"`
}

type githubSearchResult struct {
	Items      []githubItem     `json:"items"`
	Total      *int             `json:"total"`
	NextPage   int              `json:"nextPage,omitempty"`
	Incomplete bool             `json:"incomplete,omitempty"`
	Source     string           `json:"source"`
	Hint       string           `json:"hint,omitempty"`
	RateLimit  *githubRateLimit `json:"rateLimit,omitempty"`
	Counts     *githubCounts    `json:"counts,omitempty"`
}

func wantKinds(kinds []string) map[string]bool {
	want := map[string]bool{}
	for _, k := range kinds {
		if k == models.ReferenceKindGitHubIssue || k == models.ReferenceKindGitHubPR {
			want[k] = true
		}
	}
	if len(want) == 0 {
		want[models.ReferenceKindGitHubIssue], want[models.ReferenceKindGitHubPR] = true, true
	}
	return want
}

// Search: an exact target is one GET; an empty query lists recently updated
// open items without search quota; otherwise one page of the search API.
func (c *githubRefClient) Search(ctx context.Context, req githubSearchRequest) (githubSearchResult, error) {
	want := wantKinds(req.Kinds)
	tokenOf := req.TokenOf
	if tokenOf == nil {
		tokenOf = func(githubRepo) string { return "" }
	}
	var defaultRepo *githubRepo
	if len(req.Repos) == 1 {
		defaultRepo = &req.Repos[0]
	}
	query := strings.TrimSpace(req.Query)
	if t, ok := parseGitHubTarget(query, defaultRepo); ok {
		item, err := c.Exact(ctx, t, tokenOf(githubRepo{Owner: t.Owner, Repo: t.Repo}))
		if err != nil {
			return githubSearchResult{}, err
		}
		res := githubSearchResult{Items: []githubItem{}, Source: "exact"}
		if want[item.Kind] {
			res.Items = append(res.Items, item)
		}
		total := len(res.Items)
		res.Total = &total
		return res, nil
	}
	if len(req.Repos) == 0 {
		return githubSearchResult{Items: []githubItem{}, Source: "none", Hint: "没有选择 GitHub 仓库；可以输入 owner/repo#编号 或粘贴 issue/PR 链接"}, nil
	}
	page := max(req.Page, 1)
	if (page-1)*githubPageSize >= githubMaxDepth {
		return githubSearchResult{}, &githubError{Code: githubErrInvalid, Msg: fmt.Sprintf("最多翻到第 %d 条，请换更具体的关键词", githubMaxDepth)}
	}
	token := tokenOf(req.Repos[0])
	if query == "" {
		res, err := c.recent(ctx, req.Repos, req.State, page, want, tokenOf)
		if err != nil {
			return githubSearchResult{}, err
		}
		res.Counts = c.counts(ctx, req.Repos, req.State, nil, "", token, nil)
		return res, nil
	}
	terms, qualifiers, err := splitGitHubQuery(query)
	if err != nil {
		return githubSearchResult{}, err
	}
	if len([]rune(terms)) < 2 && len(qualifiers) == 0 {
		return githubSearchResult{Items: []githubItem{}, Source: "none", Hint: "至少输入 2 个字符搜索 GitHub；或输入 #编号、粘贴链接"}, nil
	}
	kind := ""
	if len(want) == 1 {
		kind = models.ReferenceKindGitHubIssue
		if want[models.ReferenceKindGitHubPR] {
			kind = models.ReferenceKindGitHubPR
		}
	}
	if c.searchExhausted(token) {
		r, _ := c.rateLimit(token, "search")
		return githubSearchResult{}, githubRateLimited(r.ResetAt.Sub(c.now()))
	}
	var body struct {
		TotalCount        int           `json:"total_count"`
		IncompleteResults bool          `json:"incomplete_results"`
		Items             []githubIssue `json:"items"`
	}
	q := strings.Join(githubSearchParts(req.Repos, req.State, kind, qualifiers, terms), " ")
	path := fmt.Sprintf("/search/issues?q=%s&per_page=%d&page=%d", url.QueryEscape(q), githubPageSize, page)
	if err := c.get(ctx, path, token, &body); err != nil {
		return githubSearchResult{}, err
	}
	res := githubSearchResult{Items: []githubItem{}, Total: &body.TotalCount, Incomplete: body.IncompleteResults, Source: "search"}
	for _, it := range body.Items {
		t, ok := targetFromRepositoryURL(it.RepoURL, it.Number)
		if !ok {
			continue
		}
		if item := it.item(t); want[item.Kind] {
			res.Items = append(res.Items, item)
		}
	}
	if page*githubPageSize < min(body.TotalCount, githubMaxDepth) {
		res.NextPage = page + 1
	}
	var known map[string]int
	if kind != "" {
		known = map[string]int{kind: body.TotalCount}
	}
	res.Counts = c.counts(ctx, req.Repos, req.State, qualifiers, terms, token, known)
	if r, ok := c.rateLimit(token, "search"); ok {
		res.RateLimit = &r
	}
	return res, nil
}

// githubSearchParts builds the search query; kind "" matches both issues and
// pull requests. Listing and counting share it so their numbers agree.
func githubSearchParts(repos []githubRepo, state, kind string, qualifiers []string, terms string) []string {
	parts := make([]string, 0, len(repos)+len(qualifiers)+3)
	for _, r := range repos {
		parts = append(parts, "repo:"+r.Owner+"/"+r.Repo)
	}
	if state != "all" {
		parts = append(parts, "is:open")
	}
	switch kind {
	case models.ReferenceKindGitHubIssue:
		parts = append(parts, "is:issue")
	case models.ReferenceKindGitHubPR:
		parts = append(parts, "is:pr")
	}
	parts = append(parts, qualifiers...)
	if terms != "" {
		parts = append(parts, terms)
	}
	return parts
}

func (c *githubRefClient) searchExhausted(token string) bool {
	r, ok := c.rateLimit(token, "search")
	return ok && r.Remaining < githubSearchReserve && r.ResetAt.After(c.now())
}

// counts asks the search API for each kind's total_count, one result per
// page. Kinds in known are already counted. Counts are best effort: a low
// search quota or a failed request leaves that kind out instead of failing
// the lane.
func (c *githubRefClient) counts(ctx context.Context, repos []githubRepo, state string, qualifiers []string, terms, token string, known map[string]int) *githubCounts {
	kinds := []string{models.ReferenceKindGitHubIssue, models.ReferenceKindGitHubPR}
	totals := make([]*int, len(kinds))
	var wg sync.WaitGroup
	for i, kind := range kinds {
		if n, ok := known[kind]; ok {
			totals[i] = &n
			continue
		}
		if c.searchExhausted(token) {
			continue
		}
		wg.Add(1)
		go func(i int, kind string) {
			defer wg.Done()
			var body struct {
				TotalCount int `json:"total_count"`
			}
			q := strings.Join(githubSearchParts(repos, state, kind, qualifiers, terms), " ")
			if err := c.get(ctx, fmt.Sprintf("/search/issues?q=%s&per_page=1", url.QueryEscape(q)), token, &body); err == nil {
				totals[i] = &body.TotalCount
			}
		}(i, kind)
	}
	wg.Wait()
	if totals[0] == nil && totals[1] == nil {
		return nil
	}
	return &githubCounts{Issue: totals[0], PR: totals[1]}
}

// recent lists recently updated items without search quota; the issues
// endpoint returns issues and pull requests together.
func (c *githubRefClient) recent(ctx context.Context, repos []githubRepo, state string, page int, want map[string]bool, tokenOf func(githubRepo) string) (githubSearchResult, error) {
	if state != "all" {
		state = "open"
	}
	res := githubSearchResult{Items: []githubItem{}, Source: "recent"}
	for _, r := range repos[:min(len(repos), 3)] {
		var body []githubIssue
		path := fmt.Sprintf("/repos/%s/%s/issues?state=%s&sort=updated&direction=desc&per_page=%d&page=%d",
			url.PathEscape(r.Owner), url.PathEscape(r.Repo), state, githubPageSize, page)
		if err := c.get(ctx, path, tokenOf(r), &body); err != nil {
			return githubSearchResult{}, err
		}
		if len(body) >= githubPageSize && page*githubPageSize < githubMaxDepth {
			res.NextPage = page + 1
		}
		for _, it := range body {
			if item := it.item(githubTarget{Owner: r.Owner, Repo: r.Repo, Number: it.Number}); want[item.Kind] {
				res.Items = append(res.Items, item)
			}
		}
	}
	sort.SliceStable(res.Items, func(i, j int) bool { return res.Items[i].UpdatedAt > res.Items[j].UpdatedAt })
	return res, nil
}

func targetFromRepositoryURL(repositoryURL string, number int) (githubTarget, bool) {
	parts := strings.Split(strings.TrimRight(repositoryURL, "/"), "/")
	if len(parts) < 3 || parts[len(parts)-3] != "repos" {
		return githubTarget{}, false
	}
	return githubTarget{Owner: parts[len(parts)-2], Repo: parts[len(parts)-1], Number: number}, true
}

var (
	githubQueryTokenRe = regexp.MustCompile(`(\S+?:)?"([^"]*)"|\S+`)
	githubQualifierRe  = regexp.MustCompile(`(?i)^(-?)([a-z]+):(.+)$`)
	githubAnyValue     = regexp.MustCompile(`.+`)
	githubQualifiers   = map[string]*regexp.Regexp{
		"is":    regexp.MustCompile(`(?i)^(open|closed|issue|pr|merged|unmerged|draft|locked|unlocked)$`),
		"state": regexp.MustCompile(`(?i)^(open|closed)$`),
		"type":  regexp.MustCompile(`(?i)^(issue|pr)$`),
		"no":    regexp.MustCompile(`(?i)^(label|assignee|milestone|project)$`),
		"in":    regexp.MustCompile(`(?i)^(title|body|comments)$`),
		"label": githubAnyValue, "author": githubAnyValue, "assignee": githubAnyValue, "mentions": githubAnyValue,
		"involves": githubAnyValue, "commenter": githubAnyValue, "milestone": githubAnyValue,
		"created": githubAnyValue, "updated": githubAnyValue, "closed": githubAnyValue,
	}
)

// splitGitHubQuery passes whitelisted qualifiers through. Scope qualifiers
// are rejected: the search stays on the session's repositories.
func splitGitHubQuery(query string) (string, []string, error) {
	var terms, qualifiers []string
	for _, m := range githubQueryTokenRe.FindAllStringSubmatch(query, -1) {
		tok := m[0]
		if m[1] != "" {
			tok = m[1] + `"` + m[2] + `"`
		}
		q := githubQualifierRe.FindStringSubmatch(tok)
		if q == nil {
			terms = append(terms, tok)
			continue
		}
		key, value := strings.ToLower(q[2]), strings.Trim(q[3], `"`)
		if key == "repo" || key == "org" || key == "user" {
			return "", nil, &githubError{Code: githubErrInvalid, Msg: "不支持 " + key + ": 限定词，搜索范围固定为当前会话的仓库"}
		}
		re, ok := githubQualifiers[key]
		if !ok || !re.MatchString(value) {
			return "", nil, &githubError{Code: githubErrInvalid, Msg: "不支持的限定词：" + tok}
		}
		if strings.ContainsAny(value, " \t") {
			value = `"` + value + `"`
		}
		qualifiers = append(qualifiers, q[1]+key+":"+value)
	}
	return strings.Join(terms, " "), qualifiers, nil
}

// githubToken picks the credential the sandbox's git-credentials helper would
// use for owner/repo: an exact repository scope beats a host scope; the first
// equal match wins. Empty means anonymous.
func (s *SoloService) githubToken(row *models.SoloSession, owner, repo string) string {
	if s.sbx == nil || s.sbx.gitCredential == nil {
		return ""
	}
	agent, err := s.sessionAgent(row)
	if err != nil || agent.GitBindings == nil {
		return ""
	}
	credentials, err := runtime.ResolveGitBindings(agent.GitBindings, s.sbx.gitCredential)
	if err != nil {
		return ""
	}
	return pickGitHubToken(credentials, owner, repo)
}

func pickGitHubToken(credentials []sandbox.GitCredential, owner, repo string) string {
	hostToken := ""
	for _, c := range credentials {
		if c.Kind != sandbox.RepoCredentialGitHubHTTPS {
			continue
		}
		host, path := githubScope(c.Scope)
		if host != "github.com" {
			continue
		}
		if path == "" {
			if hostToken == "" {
				hostToken = c.Secret
			}
			continue
		}
		if strings.EqualFold(path, owner+"/"+repo) {
			return c.Secret
		}
	}
	return hostToken
}

func githubScope(scope string) (host, path string) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "github.com", ""
	}
	if !strings.Contains(scope, "://") {
		return strings.ToLower(scope), ""
	}
	u, err := url.Parse(scope)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(u.Host), strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
}

func (s *SoloService) githubClient() *githubRefClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.github == nil {
		s.github = newGitHubRefClient(githubAPIBase, nil)
	}
	return s.github
}

// SetGitHubAPIForTest points reference lookups at a fake GitHub API.
func (s *SoloService) SetGitHubAPIForTest(base string, client *http.Client) {
	s.mu.Lock()
	s.github = newGitHubRefClient(base, client)
	s.mu.Unlock()
}

func githubReferenceCommands(ref models.MessageReference) string {
	t, ok := parseGitHubTarget(ref.Target, nil)
	if !ok {
		return ""
	}
	repo := t.Owner + "/" + t.Repo
	if ref.Kind == models.ReferenceKindGitHubPR {
		return fmt.Sprintf("gh pr view %d -R %s；评论：加 --comments；改动：gh pr diff %d -R %s", t.Number, repo, t.Number, repo)
	}
	return fmt.Sprintf("gh issue view %d -R %s；评论：gh issue view %d -R %s --comments", t.Number, repo, t.Number, repo)
}

func renderGitHubReference(ref models.MessageReference) string {
	label := "issue"
	if ref.Kind == models.ReferenceKindGitHubPR {
		label = "PR"
	}
	if ref.Access == models.ReferenceAccessDenied {
		return fmt.Sprintf("- GitHub %s %s  ⚠ 平台用你的凭据访问失败（%s），可能没有这个仓库的权限\n", label, ref.Target, ref.Error)
	}
	return fmt.Sprintf("- GitHub %s %s「%s」 %s\n  %s\n", label, ref.Target, ref.Title, ref.URL, githubReferenceCommands(ref))
}

func githubReferenceHint(ref models.MessageReference) string {
	return "GitHub 引用请直接用 gh 读取：" + githubReferenceCommands(ref)
}
