package runtime

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"
)

// gitBaseURL returns the scheme://host origin of an http(s) repo URL, used to
// derive GITLAB_URL (the git credential host) from a GitLab repo_url. Returns ""
// for non-http URLs (e.g. ssh git@host:…) or unparseable input. GitHub hosts are
// skipped by spec() so a GitHub first-repo cannot become GITLAB_URL.
func gitBaseURL(repo string) string {
	u, err := url.Parse(strings.TrimSpace(repo))
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// gitRepoHost extracts the hostname from https, ssh://, or SCP-style repo URLs.
func gitRepoHost(repo string) string {
	repo = strings.TrimSpace(repo)
	if u, err := url.Parse(repo); err == nil && u.Host != "" {
		host := u.Host
		if i := strings.Index(host, ":"); i >= 0 {
			host = host[:i]
		}
		return host
	}
	if at := strings.Index(repo, "@"); at >= 0 {
		rest := repo[at+1:]
		if colon := strings.Index(rest, ":"); colon >= 0 {
			return rest[:colon]
		}
		if slash := strings.Index(rest, "/"); slash >= 0 {
			return rest[:slash]
		}
	}
	return ""
}

// isGitLabRepo reports whether repo_url points at GitLab (gitlab.com or GITLAB_URL host).
func isGitLabRepo(repo, gitlabURL string) bool {
	host := gitRepoHost(repo)
	if host == "" {
		return false
	}
	if host == "gitlab.com" {
		return true
	}
	glHost := gitRepoHost(gitlabURL)
	if glHost == "" {
		glHost = gitRepoHost("https://" + strings.TrimPrefix(strings.TrimPrefix(gitlabURL, "https://"), "http://"))
	}
	return glHost != "" && host == glHost
}

type repoEntry struct {
	Name   string `json:"name"`
	URL    string `json:"url,omitempty"`
	Branch string `json:"branch,omitempty"`
}

func parseReposVar(v any) []repoEntry {
	if v == nil || models.IsBlankVar(v) {
		return nil
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		var repos []repoEntry
		if json.Unmarshal([]byte(s), &repos) == nil {
			out := make([]repoEntry, 0, len(repos))
			for _, r := range repos {
				r.Name = strings.TrimSpace(r.Name)
				r.URL = strings.TrimSpace(r.URL)
				r.Branch = strings.TrimSpace(r.Branch)

				if r.Name == "" {
					r.Name = repoNameFromURL(r.URL)
				}
				if !safeRepoName(r.Name) {
					continue
				}
				out = append(out, r)
			}
			return out
		}

		wire := sandbox.DecodeRepos(s)
		if len(wire) == 0 {
			return nil
		}
		out := make([]repoEntry, 0, len(wire))
		for _, r := range wire {
			if !safeRepoName(r.Name) {
				continue
			}
			out = append(out, repoEntry{Name: r.Name, URL: r.URL, Branch: r.Branch})
		}
		return out
	case []any:
		out := make([]repoEntry, 0, len(t))
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			url := strings.TrimSpace(str2(m["url"]))
			name := strings.TrimSpace(str2(m["name"]))

			if name == "" {
				name = repoNameFromURL(url)
			}
			if !safeRepoName(name) {
				continue
			}
			out = append(out, repoEntry{
				Name:   name,
				URL:    url,
				Branch: strings.TrimSpace(str2(m["branch"])),
			})
		}
		return out
	default:
		return nil
	}
}

func repoWorkspacePath(name string) string {
	name = strings.TrimSpace(name)

	if name == "" {
		return "/root/workspace"
	}
	return "/root/workspace/" + name
}

// safeRepoName reports whether name is safe to use as a flat workspace subdir
// (<workspace>/<name>/). It rejects empty names, "."/"..", and any name with a
// path separator so a malicious or mistyped `repos[].name` can never escape the
// workspace root when clones/paths are derived from it. Unsafe entries are
// dropped at parse time (parseReposVar) so they never reach GIT_REPOS, prompts,
// or git/glab commands; startup.sh applies the same guard defensively.
func safeRepoName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, "/\\")
}

// repoNameFromURL derives a workspace subdir name from a clone URL: the last
// path segment without a trailing ".git". Empty when it can't be derived.
func repoNameFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimSuffix(raw, "/")
	seg := raw
	if i := strings.LastIndexAny(seg, "/:"); i >= 0 {
		seg = seg[i+1:]
	}
	seg = strings.TrimSuffix(seg, ".git")
	return strings.TrimSpace(seg)
}

// parseBranchesVar parses the run-scoped `branches` variable (name→branch map)
// that an upstream implement node publishes so downstream fresh clones check
// out each repo's working branch. Accepts a JSON string or a decoded map.
func parseBranchesVar(v any) map[string]string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		m := map[string]string{}
		if json.Unmarshal([]byte(s), &m) != nil {
			return nil
		}
		return m
	case map[string]any:
		m := map[string]string{}
		for k, val := range t {
			if s := strings.TrimSpace(str2(val)); s != "" {
				m[k] = s
			}
		}
		return m
	case map[string]string:
		return t
	default:
		return nil
	}
}

// resolveRepos builds the (flat) clone list from vars.repos. Every repository —
// including a lone one — is cloned to <workspace>/<name>/; the workspace root is
// never a git repo. Returns nil when no usable repos are configured (a pure
// artifact flow / empty workspace). Each repo's checkout branch resolves from
// vars.branches[name] (downstream continuity) over the entry's own branch.
func resolveRepos(req NodeReq) []sandbox.RepoSpec {
	entries := parseReposVar(req.Vars["repos"])
	if len(entries) == 0 {
		return nil
	}
	branches := parseBranchesVar(req.Vars["branches"])
	seen := map[string]bool{}
	out := make([]sandbox.RepoSpec, 0, len(entries))
	for _, e := range entries {
		name := strings.TrimSpace(e.Name)
		repoURL := strings.TrimSpace(e.URL)
		if name == "" || repoURL == "" || seen[name] {
			continue
		}
		branch := e.Branch
		if b := strings.TrimSpace(branches[name]); b != "" {
			branch = b
		}
		seen[name] = true
		out = append(out, sandbox.RepoSpec{Name: name, URL: repoURL, Branch: branch})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstRepoURL returns the clone URL of the run's first configured repo (for
// GITLAB_URL host derivation / sandbox display). Empty when no repos.
func firstRepoURL(req NodeReq) string {
	repos := resolveRepos(req)
	if len(repos) == 0 {
		return ""
	}
	return repos[0].URL
}

// nodeRepo resolves the single repository a node's git/glab commands operate
// in: submit_mr pins config["repo"] (target repo name); otherwise a lone
// configured repo is used. Returns the in-sandbox working directory (the repo's
// flat subdir, or the workspace root when nothing resolves) and the repo's
// clone URL (empty when it can't be determined).
func (c *acpProvider) nodeRepo(req NodeReq) (dir, url string) {
	repos := resolveRepos(req)
	name := strings.TrimSpace(str2(req.Config["repo"]))
	if name == "" && len(repos) == 1 {
		name = repos[0].Name
	}
	for _, r := range repos {
		if r.Name == name {
			url = r.URL
			break
		}
	}
	return repoWorkspacePath(name), url
}

// nodeRepoURL is nodeRepo's URL, falling back to the first repo so GitLab host
// derivation still works when the node didn't pin a specific repo.
func (c *acpProvider) nodeRepoURL(req NodeReq) string {
	if _, u := c.nodeRepo(req); u != "" {
		return u
	}
	return firstRepoURL(req)
}

func str2(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return textutil.TruncateBytes(s, 120, "")
	}
	return s
}
