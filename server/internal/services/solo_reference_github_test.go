package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// fakeGitHub serves the few API paths reference lookups use.
type fakeGitHub struct {
	mu       sync.Mutex
	hits     map[string]int
	auth     []string
	queries  []string
	searchRL string
	handler  func(w http.ResponseWriter, r *http.Request) bool
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *httptest.Server) {
	f := &fakeGitHub{hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits[r.URL.Path]++
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		handler := f.handler
		f.mu.Unlock()
		if handler != nil && handler(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/repos/acme/web/issues/7":
			if r.Header.Get("If-None-Match") == `"v7"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v7"`)
			w.Header().Set("X-Ratelimit-Remaining", "4999")
			_, _ = fmt.Fprint(w, `{"number":7,"title":"Fix login","state":"closed","html_url":"https://github.com/acme/web/pull/7","comments":3,"updated_at":"2026-10-01T00:00:00Z","pull_request":{"merged_at":"2026-10-02T00:00:00Z"}}`)
		case r.URL.Path == "/repos/acme/web/issues/3":
			_, _ = fmt.Fprint(w, `{"number":3,"title":"Bug","state":"open","html_url":"https://github.com/acme/web/issues/3","comments":1}`)
		case r.URL.Path == "/repos/acme/secret/issues/1":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/repos/acme/web/issues":
			_, _ = fmt.Fprint(w, `[{"number":3,"title":"Bug","state":"open","updated_at":"2026-10-03"},{"number":8,"title":"PR","state":"open","updated_at":"2026-10-04","pull_request":{}}]`)
		case r.URL.Path == "/repos/acme/api/issues":
			_, _ = fmt.Fprint(w, `[{"number":1,"title":"API","state":"open","updated_at":"2026-10-05"}]`)
		case r.URL.Path == "/search/issues":
			f.mu.Lock()
			f.queries = append(f.queries, r.URL.Query().Get("q"))
			rl := f.searchRL
			f.mu.Unlock()
			w.Header().Set("X-Ratelimit-Resource", "search")
			if rl == "" {
				rl = "29"
			}
			w.Header().Set("X-Ratelimit-Remaining", rl)
			w.Header().Set("X-Ratelimit-Reset", fmt.Sprint(time.Now().Add(time.Minute).Unix()))
			_, _ = fmt.Fprint(w, `{"total_count":45,"incomplete_results":false,"items":[`+
				`{"number":3,"title":"Bug","state":"open","repository_url":"https://api.github.com/repos/acme/web"},`+
				`{"number":9,"title":"PR","state":"open","repository_url":"https://api.github.com/repos/acme/web","pull_request":{}},`+
				`{"number":1,"title":"odd","state":"open","repository_url":"nope"}]}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeGitHub) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func TestParseGitHubTargets(t *testing.T) {
	def := &githubRepo{Owner: "acme", Repo: "web"}
	for _, tc := range []struct {
		in   string
		def  *githubRepo
		want string
		ok   bool
	}{
		{"https://github.com/acme/web/pull/12/files", nil, "acme/web#12", true},
		{"http://github.com/a/b/issues/3", nil, "a/b#3", true},
		{"acme/api#5", nil, "acme/api#5", true},
		{"#5", def, "acme/web#5", true},
		{"#5", nil, "", false},
		{"#0", def, "", false},
		{"acme/web#99999999999999999999", nil, "", false},
		{"hello", def, "", false},
	} {
		got, ok := parseGitHubTarget(tc.in, tc.def)
		if ok != tc.ok || (ok && got.String() != tc.want) {
			t.Errorf("%q: %v %v", tc.in, got, ok)
		}
	}
	tgt := githubTarget{Owner: "a", Repo: "b", Number: 2}
	if tgt.URL(models.ReferenceKindGitHubPR) != "https://github.com/a/b/pull/2" || tgt.URL(models.ReferenceKindGitHubIssue) != "https://github.com/a/b/issues/2" {
		t.Fatal("target URLs")
	}
	repos := sessionGitHubRepos([]models.SessionRepo{
		{RepoURL: "https://github.com/acme/web.git"}, {RepoURL: "git@github.com:ACME/web"}, {RepoURL: "ssh://git@github.com/acme/api"},
		{RepoURL: "https://gitlab.com/acme/x"},
	})
	if len(repos) != 2 || repos[0] != (githubRepo{"acme", "web"}) || repos[1] != (githubRepo{"acme", "api"}) {
		t.Fatalf("session repos: %+v", repos)
	}
	if _, ok := targetFromRepositoryURL("https://api.github.com/repos/a/b", 1); !ok {
		t.Fatal("repository url")
	}
}

func TestSplitGitHubQuery(t *testing.T) {
	terms, quals, err := splitGitHubQuery(`login bug label:"needs review" -author:bot is:open`)
	if err != nil || terms != "login bug" || strings.Join(quals, ",") != `label:"needs review",-author:bot,is:open` {
		t.Fatalf("%q %q %v", terms, quals, err)
	}
	for _, q := range []string{"repo:x/y bug", "org:acme", "user:me", "is:nonsense", "sort:updated"} {
		if _, _, err := splitGitHubQuery(q); githubErrCode(err) != githubErrInvalid {
			t.Errorf("%q accepted: %v", q, err)
		}
	}
}

func TestPickGitHubToken(t *testing.T) {
	creds := []sandbox.GitCredential{
		{Kind: "ssh", Scope: "github.com", Secret: "ssh"},
		{Kind: sandbox.RepoCredentialGitHubHTTPS, Scope: "https://gitlab.com", Secret: "gitlab"},
		{Kind: sandbox.RepoCredentialGitHubHTTPS, Scope: "", Secret: "host"},
		{Kind: sandbox.RepoCredentialGitHubHTTPS, Scope: "github.com", Secret: "host2"},
		{Kind: sandbox.RepoCredentialGitHubHTTPS, Scope: "https://github.com/acme/web.git", Secret: "repo"},
		{Kind: sandbox.RepoCredentialGitHubHTTPS, Scope: "://bad", Secret: "bad"},
	}
	if got := pickGitHubToken(creds, "ACME", "Web"); got != "repo" {
		t.Fatalf("repo scope: %q", got)
	}
	if got := pickGitHubToken(creds, "acme", "api"); got != "host" {
		t.Fatalf("host scope: %q", got)
	}
	if got := pickGitHubToken(nil, "a", "b"); got != "" {
		t.Fatalf("anonymous: %q", got)
	}
}

func TestGitHubExactCachesAndMapsErrors(t *testing.T) {
	f, srv := newFakeGitHub(t)
	c := newGitHubRefClient(srv.URL, srv.Client())
	now := time.Now()
	c.now = func() time.Time { return now }
	ctx := context.Background()

	item, err := c.Exact(ctx, githubTarget{"acme", "web", 7}, "tok")
	if err != nil || item.Kind != models.ReferenceKindGitHubPR || item.State != "merged" || item.Title != "Fix login" || item.Repo != "acme/web" {
		t.Fatalf("exact: %+v %v", item, err)
	}
	if _, err := c.Exact(ctx, githubTarget{"acme", "web", 7}, "tok"); err != nil || f.count("/repos/acme/web/issues/7") != 1 {
		t.Fatalf("fresh cache must not refetch: %d %v", f.count("/repos/acme/web/issues/7"), err)
	}
	now = now.Add(2 * githubCacheTTL)
	if item, err := c.Exact(ctx, githubTarget{"acme", "web", 7}, "tok"); err != nil || item.Title != "Fix login" || f.count("/repos/acme/web/issues/7") != 2 {
		t.Fatalf("revalidated with 304: %+v %v", item, err)
	}
	if _, err := c.Exact(ctx, githubTarget{"acme", "web", 7}, "other"); err != nil || f.count("/repos/acme/web/issues/7") != 3 {
		t.Fatal("cache is per credential")
	}
	if f.auth[0] != "Bearer tok" {
		t.Fatalf("auth header: %q", f.auth[0])
	}
	if _, err := c.Exact(ctx, githubTarget{"acme", "secret", 1}, ""); githubErrCode(err) != githubErrNotFound || !isGitHubDenied(err) {
		t.Fatalf("404: %v", err)
	}

	for _, tc := range []struct {
		name   string
		write  func(w http.ResponseWriter)
		code   string
		denied bool
	}{
		{"401", func(w http.ResponseWriter) { w.WriteHeader(401) }, githubErrUnauthorized, true},
		{"403", func(w http.ResponseWriter) { w.WriteHeader(403) }, githubErrForbidden, true},
		{"retry-after", func(w http.ResponseWriter) { w.Header().Set("Retry-After", "7"); w.WriteHeader(429) }, githubErrRateLimited, false},
		{"exhausted", func(w http.ResponseWriter) {
			w.Header().Set("X-Ratelimit-Remaining", "0")
			w.Header().Set("X-Ratelimit-Reset", fmt.Sprint(now.Add(30*time.Second).Unix()))
			w.WriteHeader(403)
		}, githubErrRateLimited, false},
		{"500", func(w http.ResponseWriter) { w.WriteHeader(502) }, githubErrUpstream, false},
		{"bad json", func(w http.ResponseWriter) { _, _ = fmt.Fprint(w, "{") }, githubErrUpstream, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.mu.Lock()
			f.handler = func(w http.ResponseWriter, r *http.Request) bool { tc.write(w); return true }
			f.mu.Unlock()
			_, err := c.Exact(ctx, githubTarget{"x", "y", 1}, "")
			if githubErrCode(err) != tc.code || isGitHubDenied(err) != tc.denied {
				t.Fatalf("%v", err)
			}
		})
	}
	f.mu.Lock()
	f.handler = func(w http.ResponseWriter, r *http.Request) bool { time.Sleep(200 * time.Millisecond); return false }
	f.mu.Unlock()
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := c.Exact(short, githubTarget{"x", "y", 2}, ""); githubErrCode(err) != githubErrTimeout {
		t.Fatalf("timeout: %v", err)
	}
	dead := newGitHubRefClient("http://127.0.0.1:1", nil)
	if _, err := dead.Exact(ctx, githubTarget{"x", "y", 2}, ""); githubErrCode(err) != githubErrUpstream {
		t.Fatalf("unreachable: %v", err)
	}
	if _, err := newGitHubRefClient("://", nil).Exact(ctx, githubTarget{"x", "y", 2}, ""); githubErrCode(err) != githubErrUpstream {
		t.Fatalf("bad base: %v", err)
	}
	if githubErrCode(fmt.Errorf("plain")) != "" {
		t.Fatal("plain errors have no code")
	}
}

func TestGitHubCacheEvictsOldest(t *testing.T) {
	c := newGitHubRefClient("http://x", nil)
	for i := 0; i <= githubCacheSize; i++ {
		c.store(githubCacheEntry{key: fmt.Sprint(i), body: []byte("{}")})
	}
	c.store(githubCacheEntry{key: "1", body: []byte("[]")})
	if _, ok := c.cached("0"); ok {
		t.Fatal("oldest entry kept")
	}
	if hit, ok := c.cached("1"); !ok || string(hit.body) != "[]" || c.order.Len() != githubCacheSize {
		t.Fatal("update in place")
	}
}

func TestGitHubSearchModes(t *testing.T) {
	f, srv := newFakeGitHub(t)
	c := newGitHubRefClient(srv.URL, srv.Client())
	ctx := context.Background()
	web := []githubRepo{{"acme", "web"}}

	res, err := c.Search(ctx, githubSearchRequest{Query: "#3", Repos: web})
	if err != nil || res.Source != "exact" || len(res.Items) != 1 || *res.Total != 1 {
		t.Fatalf("exact #n: %+v %v", res, err)
	}
	res, _ = c.Search(ctx, githubSearchRequest{Query: "acme/web#3", Kinds: []string{models.ReferenceKindGitHubPR}})
	if len(res.Items) != 0 {
		t.Fatal("exact target filtered by kind")
	}
	if _, err := c.Search(ctx, githubSearchRequest{Query: "acme/secret#1"}); githubErrCode(err) != githubErrNotFound {
		t.Fatalf("exact error: %v", err)
	}
	if res, _ := c.Search(ctx, githubSearchRequest{Query: "bug"}); res.Source != "none" || res.Hint == "" {
		t.Fatalf("no repos: %+v", res)
	}
	res, err = c.Search(ctx, githubSearchRequest{Repos: []githubRepo{{"acme", "web"}, {"acme", "api"}}, Kinds: []string{models.ReferenceKindGitHubIssue}})
	if err != nil || res.Source != "recent" || len(res.Items) != 2 || res.Items[0].Title != "API" {
		t.Fatalf("recent: %+v %v", res, err)
	}
	if res, _ := c.Search(ctx, githubSearchRequest{Query: "x", Repos: web}); res.Hint == "" || len(res.Items) != 0 {
		t.Fatalf("short query: %+v", res)
	}
	if _, err := c.Search(ctx, githubSearchRequest{Query: "repo:a/b x", Repos: web}); githubErrCode(err) != githubErrInvalid {
		t.Fatalf("scope qualifier: %v", err)
	}
	if _, err := c.Search(ctx, githubSearchRequest{Query: "bug", Repos: web, Page: 6}); githubErrCode(err) != githubErrInvalid {
		t.Fatalf("depth: %v", err)
	}

	res, err = c.Search(ctx, githubSearchRequest{Query: "bug", Repos: web, Kinds: []string{models.ReferenceKindGitHubPR}, TokenOf: func(githubRepo) string { return "t" }})
	if err != nil || res.Source != "search" || len(res.Items) != 1 || res.Items[0].Number != 9 || res.NextPage != 2 || res.RateLimit == nil || res.RateLimit.Remaining != 29 {
		t.Fatalf("search: %+v %v", res, err)
	}
	if q := f.queries[len(f.queries)-1]; q != "repo:acme/web is:open is:pr bug" {
		t.Fatalf("query: %q", q)
	}
	res, _ = c.Search(ctx, githubSearchRequest{Query: "is:closed", Repos: web, State: "all", Kinds: []string{models.ReferenceKindGitHubIssue}, Page: 3})
	if q := f.queries[len(f.queries)-1]; q != "repo:acme/web is:issue is:closed" || res.NextPage != 0 {
		t.Fatalf("qualifier-only query: %q next=%d", q, res.NextPage)
	}
	f.mu.Lock()
	f.searchRL = "1"
	f.mu.Unlock()
	_, _ = c.Search(ctx, githubSearchRequest{Query: "again", Repos: web})
	if _, err := c.Search(ctx, githubSearchRequest{Query: "reserve", Repos: web}); githubErrCode(err) != githubErrRateLimited {
		t.Fatalf("reserve guard: %v", err)
	}
	f.mu.Lock()
	f.handler = func(w http.ResponseWriter, r *http.Request) bool { w.WriteHeader(500); return true }
	f.mu.Unlock()
	c2 := newGitHubRefClient(srv.URL, srv.Client())
	if _, err := c2.Search(ctx, githubSearchRequest{Repos: web}); err == nil {
		t.Fatal("recent error")
	}
	if _, err := c2.Search(ctx, githubSearchRequest{Query: "bug", Repos: web}); err == nil {
		t.Fatal("search error")
	}
}

func TestGitHubReferencePromptText(t *testing.T) {
	pr := models.MessageReference{Kind: models.ReferenceKindGitHubPR, Target: "acme/web#7", Title: "Fix", URL: "u"}
	if got := renderGitHubReference(pr); !strings.Contains(got, "gh pr view 7 -R acme/web") || !strings.Contains(got, "「Fix」") {
		t.Fatalf("pr: %s", got)
	}
	issue := models.MessageReference{Kind: models.ReferenceKindGitHubIssue, Target: "acme/web#3", Access: models.ReferenceAccessDenied, Error: "GitHub 404"}
	if got := renderGitHubReference(issue); !strings.Contains(got, "访问失败") || !strings.Contains(got, "GitHub 404") {
		t.Fatalf("denied: %s", got)
	}
	if got := githubReferenceHint(issue); !strings.Contains(got, "gh issue view 3 -R acme/web") {
		t.Fatalf("hint: %s", got)
	}
	if githubReferenceCommands(models.MessageReference{Target: "junk"}) != "" {
		t.Fatal("junk target")
	}
}
