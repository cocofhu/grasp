package services

import (
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm/clause"
)

// CreateFixture configures the singleton workspace row, creating it when the
// test database was migrated without the startup seed.
func (s *WorkspaceService) CreateFixture(name, description string, vars []models.WorkspaceVariable) (models.Workspace, error) {
	if vars == nil {
		vars = []models.WorkspaceVariable{}
	}
	sanitizedVars, err := sanitizeWorkspaceVars(vars)
	if err != nil {
		return models.Workspace{}, err
	}
	now := time.Now()
	ws := models.Workspace{
		ID:           models.WorkspaceID,
		Name:         name,
		Description:  description,
		Variables:    sanitizedVars,
		NotifyPolicy: models.DefaultWorkspaceNotifyPolicy(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "description", "variables", "updated_at"}),
	}).Create(&ws).Error; err != nil {
		return models.Workspace{}, err
	}
	return s.Workspace()
}
