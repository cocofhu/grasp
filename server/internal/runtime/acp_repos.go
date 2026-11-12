package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"
)

func repoWorkspacePath(name string) string {
	name = strings.TrimSpace(name)

	if name == "" {
		return "/root/workspace"
	}
	return "/root/workspace/" + name
}

// safeRepoName reports whether name is safe to use as a flat workspace subdir
// (<workspace>/<name>/). It rejects empty names, "."/"..", and any name with a
// path separator so a repository directory can never escape the workspace
// root when clones/paths are derived from it; startup.sh applies the same guard.
func safeRepoName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, "/\\")
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
			return parseGoMapBranches(s)
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

// parseGoMapBranches reads the "map[repo:branch other:branch]" form that older
// engines persisted (fmt.Sprint of a decoded object) so runs already carrying
// it still check out the implementation branch. Repo names cannot contain ':'
// or spaces and git branch names cannot contain spaces.
func parseGoMapBranches(s string) map[string]string {
	if !strings.HasPrefix(s, "map[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	m := map[string]string{}
	for _, pair := range strings.Fields(s[len("map[") : len(s)-1]) {
		name, branch, ok := strings.Cut(pair, ":")
		if ok && name != "" && branch != "" {
			m[name] = branch
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// resolveRepos builds the (flat) clone list from the run's repositories. Every
// repository — including a lone one — is cloned to <workspace>/<name>/; the
// workspace root is never a git repo. Returns nil for a run without
// repositories (a pure artifact flow / empty workspace). Each repo's checkout
// branch resolves from vars.branches[name] (downstream continuity) over the
// branch picked at launch.
func resolveRepos(req NodeReq) []sandbox.RepoSpec {
	repos, _ := CloneRepos(req.Repos, parseBranchesVar(req.Vars["branches"]), nil)
	return repos
}

func (c *acpProvider) repoGit(req NodeReq) ([]sandbox.RepoSpec, []RepoCredential) {
	return CloneRepos(req.Repos, parseBranchesVar(req.Vars["branches"]), c.opts.Project)
}

// CloneRepos turns frozen repositories into the clone list, in order, skipping
// unusable and repeated directories. branches overrides a checkout branch by
// directory. project, when set, adds each repository's current commit author
// and credential; a repository deleted since the snapshot keeps neither.
func CloneRepos(repos []models.SessionRepo, branches map[string]string, project func(id string) (*models.Project, bool)) ([]sandbox.RepoSpec, []RepoCredential) {
	seen := map[string]bool{}
	var specs []sandbox.RepoSpec
	var creds []RepoCredential
	for _, r := range repos {
		name := strings.TrimSpace(r.RepoDirName)
		if name == "" {
			name = sandbox.RepoNameFromURL(r.RepoURL)
		}
		repoURL := strings.TrimSpace(r.RepoURL)
		if !safeRepoName(name) || repoURL == "" || seen[name] {
			continue
		}
		seen[name] = true
		spec := sandbox.RepoSpec{Name: name, URL: repoURL, Branch: strings.TrimSpace(r.Branch)}
		if b := strings.TrimSpace(branches[name]); b != "" {
			spec.Branch = b
		}
		if project != nil {
			if p, ok := project(r.ProjectID); ok && p != nil {
				spec.AuthorName, spec.AuthorEmail = p.CommitName, p.CommitEmail
				if id := strings.TrimSpace(p.CredentialID); id != "" {
					creds = append(creds, RepoCredential{Repo: name, URL: repoURL, CredentialID: id})
				}
			}
		}
		specs = append(specs, spec)
	}
	return specs, creds
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
