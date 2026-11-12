package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

// snapshotRunRepos freezes the repositories a run clones. A nil picked list
// takes every repository bound to the workflow on its default branch; an
// explicit list (including an empty one) may only pick bound repositories.
func (e *Engine) snapshotRunRepos(def models.WorkflowDef, picked *[]models.RepoChoice) ([]models.SessionRepo, error) {
	var bound []models.RepoChoice
	if def.Defaults != nil {
		bound = def.Defaults.Repos
	}
	choices := bound
	if picked != nil {
		allowed := make(map[string]bool, len(bound))
		for _, b := range bound {
			allowed[strings.TrimSpace(b.ProjectID)] = true
		}
		choices = make([]models.RepoChoice, 0, len(*picked))
		seen := map[string]bool{}
		for _, c := range *picked {
			id := strings.TrimSpace(c.ProjectID)
			if !allowed[id] {
				return nil, fmt.Errorf("仓库 %s 没有绑定到这个工作流", id)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			choices = append(choices, models.RepoChoice{ProjectID: id, Branch: c.Branch})
		}
	}
	if len(choices) == 0 {
		return nil, nil
	}
	out := make([]models.SessionRepo, 0, len(choices))
	for i, c := range choices {
		var p models.Project
		if err := e.db.First(&p, "id = ?", strings.TrimSpace(c.ProjectID)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("工作流绑定的仓库 %s 已删除，请在工作流设置里移除", c.ProjectID)
			}
			return nil, err
		}
		out = append(out, services.SessionRepoFromProject(&p, c.Branch, i))
	}
	if err := services.ValidateSessionRepos(out); err != nil {
		return nil, err
	}
	return out, nil
}
