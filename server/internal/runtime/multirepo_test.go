package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

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
		if got := sandbox.RepoNameFromURL(in); got != want {
			t.Errorf("RepoNameFromURL(%q) = %q, want %q", in, got, want)
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
	legacy := parseBranchesVar("map[grasp:feature/home-light-theme-character web:feat/x]")
	if len(legacy) != 2 || legacy["grasp"] != "feature/home-light-theme-character" || legacy["web"] != "feat/x" {
		t.Errorf("legacy fmt.Sprint map = %+v", legacy)
	}
	if parseBranchesVar("map[]") != nil || parseBranchesVar("not json") != nil {
		t.Errorf("unparseable should be nil")
	}
}

// A run that persisted the legacy map form must still clone the
// implementation branch downstream, not the default branch.
func TestResolveReposLegacyBranchesVar(t *testing.T) {
	got := resolveRepos(NodeReq{
		Repos: []models.SessionRepo{{RepoURL: "https://github.com/cocofhu/grasp", RepoDirName: "grasp"}},
		Vars:  map[string]any{"branches": "map[grasp:feature/home-light-theme-character]"},
	})
	if len(got) != 1 || got[0].Branch != "feature/home-light-theme-character" {
		t.Fatalf("repos = %+v", got)
	}
}

func TestResolveRepos(t *testing.T) {
	// No repositories -> nil (pure artifact flow / empty workspace).
	if r := resolveRepos(NodeReq{Vars: map[string]any{"repo_url": "https://h/only.git"}}); r != nil {
		t.Errorf("no repos should yield nil, got %+v", r)
	}

	// A single repo is a first-class entry (cloned to <workspace>/<name>/ like
	// any other). branches map overrides per-repo branch.
	single := resolveRepos(NodeReq{
		Repos: []models.SessionRepo{{RepoURL: "https://h/web.git"}},
		Vars:  map[string]any{"branches": `{"web":"feat/web"}`},
	})
	if len(single) != 1 || single[0] != (sandbox.RepoSpec{Name: "web", URL: "https://h/web.git", Branch: "feat/web"}) {
		t.Fatalf("single = %+v", single)
	}

	// Multiple repos: per-entry branch, with branches map override taking
	// precedence over the entry's own branch.
	req := NodeReq{
		Repos: []models.SessionRepo{{RepoURL: "https://h/web.git", RepoDirName: "web"}, {RepoURL: "https://h/api.git", RepoDirName: "api", Branch: "main"}},
		Vars:  map[string]any{"branches": `{"api":"feat/api","web":"feat/web"}`},
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
	got2 := resolveRepos(NodeReq{Repos: []models.SessionRepo{
		{RepoURL: "https://h/web.git", RepoDirName: "web"}, {RepoURL: "https://other/web.git", RepoDirName: "web"}, {RepoDirName: "nourl"},
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
	req := NodeReq{Repos: []models.SessionRepo{{RepoURL: "https://h/web.git"}, {RepoURL: "https://h/api.git"}}}
	if got := firstRepoURL(req); got != "https://h/web.git" {
		t.Errorf("firstRepoURL = %q", got)
	}
	if got := firstRepoURL(NodeReq{}); got != "" {
		t.Errorf("no repos should yield empty, got %q", got)
	}
}

func TestMultiRepoLayoutText(t *testing.T) {
	// No repos -> empty (single-repo / pure artifact flow).
	if got := multiRepoLayoutText(NodeReq{}); got != "" {
		t.Errorf("no repos should yield empty layout, got %q", got)
	}
	got := multiRepoLayoutText(NodeReq{Repos: []models.SessionRepo{{RepoURL: "https://h/web.git"}, {RepoURL: "https://h/api.git"}}})
	if !strings.Contains(got, "/root/workspace/web/") || !strings.Contains(got, "/root/workspace/api/") {
		t.Errorf("layout text missing repo dirs: %q", got)
	}
}
