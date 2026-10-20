package services

import (
	"errors"
	"github.com/cocofhu/grasp/internal/models"
	"strings"
)

var ErrWorkspaceMigrationRequired = errors.New("workspace migration required: expected exactly one workspace")
var ErrWorkspaceScope = errors.New("project scope does not match the workspace")
var ErrWorkspaceProtected = errors.New("the single workspace cannot be created or deleted")

func (s *ProjectService) Workspace() (models.Project, error) {
	var rows []models.Project
	if s == nil || s.db == nil {
		return models.Project{}, ErrWorkspaceMigrationRequired
	}
	if err := s.db.Limit(2).Find(&rows).Error; err != nil {
		return models.Project{}, err
	}
	if len(rows) != 1 {
		return models.Project{}, ErrWorkspaceMigrationRequired
	}
	return rows[0], nil
}
func (s *ProjectService) ResolveWorkspaceID(requested string) (string, error) {
	p, err := s.Workspace()
	if err != nil {
		return "", err
	}
	if id := strings.TrimSpace(requested); id != "" && id != p.ID {
		return "", ErrWorkspaceScope
	}
	return p.ID, nil
}

// ValidateWorkspaceAgents refuses silently adopting file-based identities whose
// home is absent or belongs to an unmigrated workspace.
func (s *ProjectService) ValidateWorkspaceAgents(agents *AgentService) error {
	p, err := s.Workspace()
	if err != nil {
		return err
	}
	for _, a := range agents.List() {
		if strings.TrimSpace(a.ProjectID) != p.ID {
			return ErrWorkspaceMigrationRequired
		}
	}
	return nil
}
