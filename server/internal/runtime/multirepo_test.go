package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestParseReposVar(t *testing.T) {
	// JSON string form with url + branch.
	got := parseReposVar(`[{"name":"web","url":"https://h/w.git","branch":"dev"},{"name":"api","url":"https://h/a.git"}]`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
	if got[0].Name != "web" || got[0].URL != "https://h/w.git" || got[0].Branch != "dev" {
		t.Errorf("entry0 = %+v", got[0])
	}
	if got[1].Name != "api" || got[1].URL != "https://h/a.git" || got[1].Branch != "" {
		t.Errorf("entry1 = %+v", got[1])
	}

	// Decoded []any form (as it arrives from run vars). A blank name defaults to
	// the repo derived from the clone URL (so url-only entries are kept).
	anyForm := []any{
		map[string]any{"name": "web", "url": "https://h/w.git", "branch": "feat"},
		map[string]any{"url": "https://h/x.git"}, // no name -> derived "x"
	}
	got2 := parseReposVar(anyForm)
	if len(got2) != 2 || got2[0].Name != "web" || got2[0].Branch != "feat" || got2[1].Name != "x" {
		t.Errorf("anyForm = %+v", got2)
	}

	// Wire format (GIT_REPOS / onboarding literal) is accepted alongside JSON.
	wire := parseReposVar("demo|https://github.com/heroku/nodejs-getting-started.git|main")
	if len(wire) != 1 || wire[0].Name != "demo" || wire[0].Branch != "main" {
		t.Errorf("wire form = %+v", wire)
	}

	// Blank / invalid -> nil (single-repo mode).
	if parseReposVar("") != nil || parseReposVar(nil) != nil || parseReposVar("not json") != nil {
		t.Errorf("blank/invalid should be nil")
	}
	if r := parseReposVar([]any{}); len(r) != 0 {
		t.Errorf("empty slice should yield no repos, got %+v", r)
	}

	// Blank name defaults to the repo derived from the clone URL (both forms).
	if r := parseReposVar(`[{"url":"https://h/web-svc.git"}]`); len(r) != 1 || r[0].Name != "web-svc" || r[0].URL != "https://h/web-svc.git" {
		t.Errorf("string blank-name derive = %+v", r)
	}
	if r := parseReposVar([]any{map[string]any{"url": "git@github.com:org/api.git"}}); len(r) != 1 || r[0].Name != "api" {
		t.Errorf("anyForm blank-name derive = %+v", r)
	}
	// A blank name with no URL is still dropped.
	if r := parseReposVar([]any{map[string]any{"branch": "x"}}); len(r) != 0 {
		t.Errorf("no name/url should be dropped, got %+v", r)
	}

	// Unsafe names (path separators / traversal) are dropped so a clone can
	// never escape the workspace root. Both string and []any forms.
	unsafe := parseReposVar(`[{"name":"../etc","url":"https://h/a.git"},{"name":"a/b","url":"https://h/b.git"},{"name":"ok","url":"https://h/c.git"}]`)
	if len(unsafe) != 1 || unsafe[0].Name != "ok" {
		t.Errorf("unsafe names should be dropped, got %+v", unsafe)
	}
	unsafe2 := parseReposVar([]any{
		map[string]any{"name": "..", "url": "https://h/a.git"},
		map[string]any{"name": "good", "url": "https://h/b.git"},
	})
	if len(unsafe2) != 1 || unsafe2[0].Name != "good" {
		t.Errorf("unsafe []any names should be dropped, got %+v", unsafe2)
	}
}

func TestSafeRepoName(t *testing.T) {
	for _, ok := range []string{"web", "api-svc", "a.b", "repo_1", " trimmed "} {
		if !safeRepoName(ok) {
			t.Errorf("%q should be safe", ok)
		}
	}
	for _, bad := range []string{"", "   ", ".", "..", "a/b", "../etc", `a\b`, "/abs"} {
		if safeRepoName(bad) {
			t.Errorf("%q should be unsafe", bad)
		}
	}
}

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://git.host.cc/org/web.git": "web",
		"https://git.host.cc/org/web":     "web",
		"git@github.com:org/api.git":      "api",
		"ssh://git@host/org/infra.git/":   "infra",
		"":                                "",
	}
	for in, want := range cases {
		if got := repoNameFromURL(in); got != want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseBranchesVar(t *testing.T) {
	m := parseBranchesVar(`{"web":"feat/x","api":"feat/y"}`)
	if len(m) != 2 || m["web"] != "feat/x" || m["api"] != "feat/y" {
		t.Fatalf("json map = %+v", m)
	}
	m2 := parseBranchesVar(map[string]any{"web": "b1", "api": ""})
	if len(m2) != 1 || m2["web"] != "b1" {
		t.Errorf("any map = %+v", m2)
	}
	if parseBranchesVar("") != nil || parseBranchesVar(nil) != nil {
		t.Errorf("blank should be nil")
	}
}

func TestResolveRepos(t *testing.T) {
	// No vars.repos -> nil (pure artifact flow / empty workspace). repo_url is
	// no longer consulted.
	if r := resolveRepos(NodeReq{Vars: map[string]any{"repo_url": "https://h/only.git"}}); r != nil {
		t.Errorf("no repos should yield nil, got %+v", r)
	}

	// A single repo is a first-class entry (cloned to <workspace>/<name>/ like
	// any other). branches map overrides per-repo branch.
	single := resolveRepos(NodeReq{Vars: map[string]any{
		"repos":    `[{"name":"web","url":"https://h/web.git"}]`,
		"branches": `{"web":"feat/web"}`,
	}})
	if len(single) != 1 || single[0] != (sandbox.RepoSpec{Name: "web", URL: "https://h/web.git", Branch: "feat/web"}) {
		t.Fatalf("single = %+v", single)
	}

	// Multiple repos: per-entry branch, with branches map override taking
	// precedence over the entry's own branch.
	req := NodeReq{
		Vars: map[string]any{
			"repos":    `[{"name":"web","url":"https://h/web.git"},{"name":"api","url":"https://h/api.git","branch":"main"}]`,
			"branches": `{"api":"feat/api","web":"feat/web"}`,
		},
	}
	got := resolveRepos(req)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
	want := map[string]sandbox.RepoSpec{
		"web": {Name: "web", URL: "https://h/web.git", Branch: "feat/web"},
		"api": {Name: "api", URL: "https://h/api.git", Branch: "feat/api"},
	}
	for _, r := range got {
		w, ok := want[r.Name]
		if !ok || r != w {
			t.Errorf("repo %q = %+v, want %+v", r.Name, r, w)
		}
	}

	// Dedupe: a later entry whose name collides with an earlier one is skipped
	// (first wins). Entries without a URL are dropped.
	got2 := resolveRepos(NodeReq{Vars: map[string]any{
		"repos": `[{"name":"web","url":"https://h/web.git"},{"name":"web","url":"https://other/web.git"},{"name":"nourl"}]`,
	}})
	if len(got2) != 1 || got2[0].URL != "https://h/web.git" {
		t.Errorf("dedupe = %+v", got2)
	}
}

func TestCaptureMultiRepoChanges(t *testing.T) {
	ch := &sandbox.Changes{
		VCS: "multi",
		Repos: []sandbox.RepoChanges{
			{Name: "web", Path: "/root/workspace/web", Changes: sandbox.Changes{
				Branch: "feat/web", HeadSHA: "aaa", Pushed: true,
				ChangedFiles: []sandbox.ChangedFile{{Path: "a.ts"}},
			}},
			{Name: "api", Path: "/root/workspace/api", Changes: sandbox.Changes{
				Branch: "feat/api", HeadSHA: "bbb", Pushed: false,
				ChangedFiles: []sandbox.ChangedFile{{Path: "b.go"}},
			}},
		},
	}
	out := map[string]any{}
	git := captureMultiRepoChanges(ch, out)
	if git == nil {
		t.Fatal("git is nil")
	}
	if len(git.Repos) != 2 {
		t.Fatalf("git.Repos len = %d", len(git.Repos))
	}
	// branches map emitted as JSON string.
	branches, _ := out["branches"].(string)
	if branches == "" {
		t.Fatal("branches not emitted")
	}
	bm := parseBranchesVar(branches)
	if bm["web"] != "feat/web" || bm["api"] != "feat/api" {
		t.Errorf("branches = %+v", bm)
	}
	// Aggregate pushed is false because api (with changes) was not pushed.
	if got, _ := out["pushed"].(bool); got {
		t.Errorf("aggregate pushed should be false")
	}
	if _, ok := out["repos_changes"]; !ok {
		t.Error("repos_changes not emitted")
	}

	// All pushed -> aggregate true.
	ch.Repos[1].Pushed = true
	out2 := map[string]any{}
	captureMultiRepoChanges(ch, out2)
	if got, _ := out2["pushed"].(bool); !got {
		t.Errorf("aggregate pushed should be true when all pushed")
	}
}

func TestCaptureMultiRepoChangesEmpty(t *testing.T) {
	// No repos -> nil GitInfo, no outputs mutated.
	out := map[string]any{}
	if git := captureMultiRepoChanges(&sandbox.Changes{VCS: "multi"}, out); git != nil {
		t.Errorf("empty repos should yield nil, got %+v", git)
	}
	if len(out) != 0 {
		t.Errorf("empty repos should not mutate out, got %+v", out)
	}
}

func TestRepoWorkspacePath(t *testing.T) {
	// repoWorkspacePath is a pure join; name safety is enforced earlier at parse
	// time (see TestSafeRepoName), so unsafe names never reach here in practice.
	cases := map[string]string{
		"":      "/root/workspace",
		"  ":    "/root/workspace",
		"web":   "/root/workspace/web",
		" api ": "/root/workspace/api",
	}
	for in, want := range cases {
		if got := repoWorkspacePath(in); got != want {
			t.Errorf("repoWorkspacePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstRepoURL(t *testing.T) {
	req := NodeReq{Vars: map[string]any{
		"repos": `[{"name":"web","url":"https://h/web.git"},{"name":"api","url":"https://h/api.git"}]`,
	}}
	if got := firstRepoURL(req); got != "https://h/web.git" {
		t.Errorf("firstRepoURL = %q", got)
	}
	if got := firstRepoURL(NodeReq{}); got != "" {
		t.Errorf("no repos should yield empty, got %q", got)
	}
}

func TestNodeRepo(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)

	reposVar := `[{"name":"web","url":"https://h/web.git"},{"name":"api","url":"https://h/api.git"}]`

	// submit_mr pins config["repo"] -> that repo's dir + url.
	dir, url := p.nodeRepo(NodeReq{
		NodeType: "agent", Caps: testPlainCaps,
		Config: map[string]any{"repo": "api"},
		Vars:   map[string]any{"repos": reposVar},
	})
	if dir != "/root/workspace/api" || url != "https://h/api.git" {
		t.Errorf("pinned repo: dir=%q url=%q", dir, url)
	}

	// No pin + a lone repo -> that repo.
	dir2, url2 := p.nodeRepo(NodeReq{
		Vars: map[string]any{"repos": `[{"name":"only","url":"https://h/only.git"}]`},
	})
	if dir2 != "/root/workspace/only" || url2 != "https://h/only.git" {
		t.Errorf("lone repo: dir=%q url=%q", dir2, url2)
	}

	// No pin + multiple repos -> workspace root, empty url (ambiguous).
	dir3, url3 := p.nodeRepo(NodeReq{Vars: map[string]any{"repos": reposVar}})
	if dir3 != "/root/workspace" || url3 != "" {
		t.Errorf("ambiguous: dir=%q url=%q", dir3, url3)
	}

	// nodeRepoURL falls back to the first repo when the node didn't pin one.
	if got := p.nodeRepoURL(NodeReq{Vars: map[string]any{"repos": reposVar}}); got != "https://h/web.git" {
		t.Errorf("nodeRepoURL fallback = %q", got)
	}
}

func TestMRBranchesMultiRepo(t *testing.T) {
	// Explicit source_branch always wins.
	src, _ := mrBranches(NodeReq{Config: map[string]any{"source_branch": "explicit"}})
	if src != "explicit" {
		t.Errorf("explicit source = %q", src)
	}

	// Derive the source from branches[repo] using config["repo"].
	src2, _ := mrBranches(NodeReq{
		Config: map[string]any{"repo": "api"},
		Vars: map[string]any{
			"repos":    `[{"name":"web","url":"https://h/web.git"},{"name":"api","url":"https://h/api.git"}]`,
			"branches": `{"api":"feat/api","web":"feat/web"}`,
		},
	})
	if src2 != "feat/api" {
		t.Errorf("derived source from branches[api] = %q", src2)
	}

	// A lone repo needs no explicit config["repo"].
	src3, _ := mrBranches(NodeReq{
		Vars: map[string]any{
			"repos":    `[{"name":"only","url":"https://h/only.git"}]`,
			"branches": `{"only":"feat/only"}`,
		},
	})
	if src3 != "feat/only" {
		t.Errorf("lone repo derived source = %q", src3)
	}

	// No branches map → source stays empty (vars.branch is no longer consulted).
	src4, tgt4 := mrBranches(NodeReq{
		Config: map[string]any{"target_branch": "release"},
		Vars:   map[string]any{"branch": "legacy"},
	})
	if src4 != "" || tgt4 != "release" {
		t.Errorf("no branches map: src=%q tgt=%q", src4, tgt4)
	}
}

func TestMultiRepoLayoutText(t *testing.T) {
	// No repos -> empty (single-repo / pure artifact flow).
	if got := multiRepoLayoutText(NodeReq{}); got != "" {
		t.Errorf("no repos should yield empty layout, got %q", got)
	}
	got := multiRepoLayoutText(NodeReq{Vars: map[string]any{
		"repos": `[{"name":"web","url":"https://h/web.git"},{"name":"api","url":"https://h/api.git"}]`,
	}})
	if !strings.Contains(got, "/root/workspace/web/") || !strings.Contains(got, "/root/workspace/api/") {
		t.Errorf("layout text missing repo dirs: %q", got)
	}
}
