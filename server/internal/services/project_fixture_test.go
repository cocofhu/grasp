package services

import (
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"strings"
	"time"
)

// Fixtures construct legacy shapes explicitly; production cannot create projects.
func (s *ProjectService) CreateFixture(name, description string, vars []models.ProjectVariable) (models.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Project{}, ErrEmptyProjectName
	}
	if s.NameExists(name, "") {
		return models.Project{}, ErrProjectNameExists
	}
	if vars == nil {
		vars = []models.ProjectVariable{}
	}
	sanitizedVars, err := sanitizeProjectVars(vars)
	if err != nil {
		return models.Project{}, err
	}
	now := time.Now()
	p := models.Project{
		ID:           "proj-" + uuid.NewString()[:8],
		Name:         name,
		Description:  description,
		Variables:    sanitizedVars,
		NotifyPolicy: models.DefaultProjectNotifyPolicy(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&p).Error; err != nil {
		return models.Project{}, err
	}
	return p, nil
}

func (s *ProjectService) DeleteFixture(id string) error {
	if _, ok := s.Get(id); !ok {
		return ErrProjectNotFound
	}
	if s.WorkflowCount(id) > 0 {
		return ErrProjectHasWorkflows
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", id).Delete(&models.RequirementDraft{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.ProjectCredential{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Project{}, "id = ?", id).Error
	})
}
