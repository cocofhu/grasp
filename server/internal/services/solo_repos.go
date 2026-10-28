package services

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// SoloRepoChoice is one repository selected when a Solo session starts.
// An empty Branch keeps that repository's own default branch.
type SoloRepoChoice struct {
	ProjectID string `json:"projectId"`
	Branch    string `json:"branch"`
}

// soloChoices prefers an explicit repos list (including an empty one, which
// means an empty workspace). A missing list falls back to the legacy single
// projectId and branch.
func soloChoices(in SoloCreateInput) []SoloRepoChoice {
	if in.Repos != nil {
		return in.Repos
	}
	if pid := strings.TrimSpace(in.ProjectID); pid != "" {
		return []SoloRepoChoice{{ProjectID: pid, Branch: in.Branch}}
	}
	return nil
}

func (s *SoloService) resolveSessionRepos(in SoloCreateInput) ([]models.SessionRepo, []*models.Project, error) {
	choices := soloChoices(in)
	if len(choices) == 0 {
		if strings.ContainsAny(strings.TrimSpace(in.Branch), " |,") {
			return nil, nil, soloErr(http.StatusBadRequest, "branch_invalid", "分支名不能包含空格、| 或 ,")
		}
		return nil, nil, nil
	}
	snaps := make([]models.SessionRepo, 0, len(choices))
	projects := make([]*models.Project, 0, len(choices))
	for i, choice := range choices {
		id := strings.TrimSpace(choice.ProjectID)
		if id == "" {
			return nil, nil, soloErr(http.StatusNotFound, "project_not_found", "仓库不存在")
		}
		p, err := s.projects.Get(id)
		if err != nil {
			return nil, nil, err
		}
		projects = append(projects, p)
		snaps = append(snaps, sessionRepoFromProject(p, choice.Branch, i))
	}
	if err := validateSessionRepos(snaps); err != nil {
		return nil, nil, err
	}
	return snaps, projects, nil
}

func sessionRepoFromProject(p *models.Project, branch string, position int) models.SessionRepo {
	b := strings.TrimSpace(branch)
	if b == "" {
		b = strings.TrimSpace(p.DefaultBranch)
	}
	return models.SessionRepo{
		Position:    position,
		ProjectID:   p.ID,
		Name:        p.Name,
		RepoURL:     strings.TrimSpace(p.RepoURL),
		RepoDirName: resolvedRepoDir(p.RepoDirName, p.RepoURL),
		Branch:      b,
	}
}

func resolvedRepoDir(dir, repoURL string) string {
	name := strings.TrimSpace(dir)
	if name == "" {
		name = sandbox.RepoNameFromURL(repoURL)
	}
	if name == "" {
		name = "repo"
	}
	return name
}

func validateSessionRepos(repos []models.SessionRepo) error {
	if err := validateRepoDirs(repos); err != nil {
		return err
	}
	return validateRepoBranches(repos)
}

func validateRepoDirs(repos []models.SessionRepo) error {
	order := make([]string, 0)
	names := map[string][]string{}
	for _, repo := range repos {
		dir := repo.RepoDirName
		if _, ok := names[dir]; !ok {
			order = append(order, dir)
		}
		names[dir] = append(names[dir], repo.Name)
	}
	for _, dir := range order {
		if len(names[dir]) < 2 {
			continue
		}
		return soloErrParams(http.StatusBadRequest, "repo_dir_conflict",
			"目录名 %s 被仓库 %s 同时使用，请到仓库配置里修改目录名。",
			map[string]any{"dir": dir, "repos": names[dir]},
			dir, strings.Join(names[dir], "、"))
	}
	return nil
}

func validateRepoBranches(repos []models.SessionRepo) error {
	for _, repo := range repos {
		if strings.ContainsAny(repo.Branch, " |,") {
			return soloErrParams(http.StatusBadRequest, "branch_invalid_repo",
				"分支名不能包含空格、| 或 ,（%s）",
				map[string]any{"repo": repo.Name}, repo.Name)
		}
	}
	return nil
}

// effectiveSessionRepos returns the frozen list. Legacy rows that only stored
// one project snapshot are read as a single-element list.
func effectiveSessionRepos(row *models.SoloSession) []models.SessionRepo {
	if row == nil {
		return nil
	}
	if len(row.Repos) > 0 {
		return row.Repos
	}
	return legacySessionRepos(row)
}

func legacySessionRepos(row *models.SoloSession) []models.SessionRepo {
	p := row.ProjectSnapshot
	if p == nil || strings.TrimSpace(p.RepoURL) == "" {
		return nil
	}
	return []models.SessionRepo{sessionRepoFromProject(p, row.Branch, 0)}
}

// SoloRepoSpecsList converts a frozen snapshot into the sandbox clone list.
// Branch and directory were resolved when the snapshot was written.
func SoloRepoSpecsList(repos []models.SessionRepo) []sandbox.RepoSpec {
	out := make([]sandbox.RepoSpec, 0, len(repos))
	for _, repo := range repos {
		url := strings.TrimSpace(repo.RepoURL)
		if url == "" {
			continue
		}
		name := strings.TrimSpace(repo.RepoDirName)
		if name == "" {
			name = resolvedRepoDir("", url)
		}
		out = append(out, sandbox.RepoSpec{Name: name, URL: url, Branch: strings.TrimSpace(repo.Branch)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
