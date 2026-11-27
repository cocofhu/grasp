package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/models"
)

const discussionFields = `number title closed updatedAt comments { totalCount }`

const discussionLookupQuery = `query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    discussion(number: $number) { ` + discussionFields + ` }
  }
}`

const discussionSearchQuery = `query($query: String!, $first: Int!) {
  search(query: $query, type: DISCUSSION, first: $first) {
    discussionCount
    nodes { ... on Discussion { ` + discussionFields + ` repository { nameWithOwner } } }
  }
}`

const discussionCredentialHint = "GitHub Discussion 搜索需要仓库凭据，也可以粘贴 Discussion 链接引用"

type githubDiscussion struct {
	Number     int
	Title      string
	Closed     bool
	UpdatedAt  string
	Comments   struct{ TotalCount int }
	Repository struct{ NameWithOwner string }
}

func (d githubDiscussion) item(target githubTarget) githubItem {
	state := "open"
	if d.Closed {
		state = "closed"
	}
	return githubItem{Kind: models.ReferenceKindGitHubDiscussion, Target: target.String(), Title: d.Title, State: state,
		URL: target.URL(models.ReferenceKindGitHubDiscussion), Comments: d.Comments.TotalCount, UpdatedAt: d.UpdatedAt,
		Repo: target.Owner + "/" + target.Repo, Number: target.Number}
}

func (c *githubRefClient) discussionGraphQL(ctx context.Context, token, query string, variables map[string]any, data any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	response := struct {
		Data   any
		Errors []struct{ Type string }
	}{Data: data}
	if err := c.request(ctx, http.MethodPost, "/graphql", token, payload, &response); err != nil {
		return err
	}
	if len(response.Errors) == 0 {
		return nil
	}
	switch response.Errors[0].Type {
	case "NOT_FOUND":
		return &githubError{Code: githubErrNotFound, Msg: "GitHub Discussion 不存在或仓库不可见"}
	case "FORBIDDEN":
		return &githubError{Code: githubErrForbidden, Msg: "GitHub 凭据没有读取 Discussion 的权限"}
	case "RATE_LIMITED":
		if r, ok := c.rateLimit(token, "graphql"); ok {
			return githubRateLimited(r.ResetAt.Sub(c.now()))
		}
	}
	return &githubError{Code: githubErrUpstream, Msg: "GitHub Discussion 请求失败"}
}

func (c *githubRefClient) exactDiscussion(ctx context.Context, target githubTarget, token string) (githubItem, error) {
	var data struct {
		Repository struct{ Discussion *githubDiscussion }
	}
	if err := c.discussionGraphQL(ctx, token, discussionLookupQuery,
		map[string]any{"owner": target.Owner, "repo": target.Repo, "number": target.Number}, &data); err != nil {
		return githubItem{}, err
	}
	if data.Repository.Discussion == nil {
		return githubItem{}, &githubError{Code: githubErrNotFound, Msg: "GitHub Discussion 不存在或仓库不可见"}
	}
	return data.Repository.Discussion.item(target), nil
}

func (c *githubRefClient) searchDiscussions(ctx context.Context, req githubSearchRequest) (githubSearchResult, error) {
	res := githubSearchResult{Items: []githubItem{}, Source: "none"}
	var defaultRepo *githubRepo
	if len(req.Repos) == 1 {
		defaultRepo = &req.Repos[0]
	}
	segment := githubURLSegment(req.Query)
	if segment == "issues" || segment == "pull" {
		return res, nil
	}
	if target, ok := parseGitHubTarget(req.Query, defaultRepo); ok {
		token := req.TokenOf(githubRepo{Owner: target.Owner, Repo: target.Repo})
		if token == "" {
			res.Hint = discussionCredentialHint
			return res, nil
		}
		item, err := c.exactDiscussion(ctx, target, token)
		if githubErrCode(err) == githubErrNotFound && segment == "" {
			return res, nil // A bare #number may be an Issue or PR.
		}
		if err != nil {
			return res, err
		}
		total := 1
		return githubSearchResult{Items: []githubItem{item}, Total: &total, Source: "exact"}, nil
	}
	if len(req.Repos) == 0 {
		res.Hint = "没有选择 GitHub 仓库，可以粘贴 Discussion 链接引用"
		return res, nil
	}
	page := max(req.Page, 1)
	if page > githubMaxDepth/githubPageSize {
		return res, &githubError{Code: githubErrInvalid, Msg: fmt.Sprintf("最多翻到第 %d 条，请换更具体的关键词", githubMaxDepth)}
	}
	groups := map[string][]githubRepo{}
	for _, repo := range req.Repos {
		if token := req.TokenOf(repo); token != "" {
			groups[token] = append(groups[token], repo)
		}
	}
	if len(groups) == 0 {
		res.Hint = discussionCredentialHint
		return res, nil
	}

	// Fetch the ordered prefix for each credential. Merging those prefixes
	// supplies one page across repos without sharing a credential across scopes.
	type batch struct {
		items []githubItem
		total int
		err   error
	}
	var loading sync.WaitGroup
	batches := make(chan batch, len(groups))
	for token, repositories := range groups {
		loading.Go(func() {
			items, total, err := c.discussionSearchBatch(ctx, token, repositories, req, page*githubPageSize)
			batches <- batch{items, total, err}
		})
	}
	loading.Wait()
	close(batches)
	var lastErr error
	var total, successful int
	for found := range batches {
		if found.err != nil {
			lastErr = found.err
			continue
		}
		successful++
		total += found.total
		res.Items = append(res.Items, found.items...)
	}
	if successful == 0 {
		return res, lastErr
	}
	sort.Slice(res.Items, func(i, j int) bool {
		if res.Items[i].UpdatedAt != res.Items[j].UpdatedAt {
			return res.Items[i].UpdatedAt > res.Items[j].UpdatedAt
		}
		return res.Items[i].Target < res.Items[j].Target
	})
	start := min((page-1)*githubPageSize, len(res.Items))
	res.Items = res.Items[start:min(page*githubPageSize, len(res.Items))]
	res.Source, res.Total = "search", &total
	res.Incomplete = lastErr != nil
	if page*githubPageSize < min(total, githubMaxDepth) {
		res.NextPage = page + 1
	}
	return res, nil
}

func (c *githubRefClient) discussionSearchBatch(ctx context.Context, token string, repos []githubRepo, req githubSearchRequest, limit int) ([]githubItem, int, error) {
	parts := []string{"sort:updated-desc"}
	allowed := make(map[string]githubRepo, len(repos))
	for _, repo := range repos {
		name := repo.Owner + "/" + repo.Repo
		allowed[strings.ToLower(name)] = repo
		parts = append(parts, "repo:"+name)
	}
	if req.State != "all" {
		parts = append(parts, "is:open")
	}
	// Quote the keywords as a phrase so typed text cannot add search qualifiers.
	if term := strings.TrimSpace(strings.NewReplacer(`"`, " ", `\`, " ").Replace(req.Query)); term != "" {
		parts = append(parts, `"`+term+`"`)
	}
	var data struct {
		Search struct {
			DiscussionCount int
			Nodes           []githubDiscussion
		}
	}
	if err := c.discussionGraphQL(ctx, token, discussionSearchQuery, map[string]any{"query": strings.Join(parts, " "), "first": limit}, &data); err != nil {
		return nil, 0, err
	}
	items := make([]githubItem, 0, len(data.Search.Nodes))
	for _, d := range data.Search.Nodes {
		repo, ok := allowed[strings.ToLower(d.Repository.NameWithOwner)]
		if !ok || d.Number < 1 || (req.State != "all" && d.Closed) {
			continue
		}
		items = append(items, d.item(githubTarget{Owner: repo.Owner, Repo: repo.Repo, Number: d.Number}))
	}
	return items, data.Search.DiscussionCount, nil
}

func githubDiscussionCommand(t githubTarget, useRepoCredential bool) string {
	api := fmt.Sprintf(`api --hostname github.com graphql --paginate -f owner=%s -f repo=%s -F number=%d -f query='query($owner: String!, $repo: String!, $number: Int!, $endCursor: String) { repository(owner: $owner, name: $repo) { discussion(number: $number) { title body comments(first: 50, after: $endCursor) { nodes { author { login } body } pageInfo { hasNextPage endCursor } } } } }'`, t.Owner, t.Repo, t.Number)
	if useRepoCredential {
		return fmt.Sprintf(`(helper="$HOME/.grasp/git-credentials/helper.py"; if [ -r "$helper" ]; then set -- python3 "$helper" exec-cli gh https://github.com/%s/%s --; else set -- gh; fi; "$@" %s)`, t.Owner, t.Repo, api)
	}
	return "gh " + api
}
