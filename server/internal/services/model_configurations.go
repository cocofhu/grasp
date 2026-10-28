package services

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrModelConfigurationNotFound = errors.New("model configuration not found")
	ErrModelConfigurationInvalid  = errors.New("invalid model configuration")
)

type ModelConfigurationService struct{ db *gorm.DB }

func NewModelConfigurationService(db *gorm.DB) *ModelConfigurationService {
	return &ModelConfigurationService{db: db}
}

func (s *ModelConfigurationService) List() ([]modelrouting.Configuration, error) {
	var rows []models.ModelConfiguration
	if err := s.db.Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]modelrouting.Configuration, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelrouting.Configuration(row))
	}
	return items, nil
}

func (s *ModelConfigurationService) Get(id string) (modelrouting.Configuration, error) {
	var row models.ModelConfiguration
	if err := s.db.First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return modelrouting.Configuration{}, ErrModelConfigurationNotFound
		}
		return modelrouting.Configuration{}, err
	}
	return modelrouting.Configuration(row), nil
}

func (s *ModelConfigurationService) Create(configuration modelrouting.Configuration) (modelrouting.Configuration, error) {
	configuration.ID = "model-" + uuid.NewString()[:12]
	configuration.Revision = 1
	configuration.CreatedAt, configuration.UpdatedAt = time.Now(), time.Now()
	if err := s.validate(&configuration); err != nil {
		return modelrouting.Configuration{}, err
	}
	row := models.ModelConfiguration(configuration)
	if err := s.db.Create(&row).Error; err != nil {
		return modelrouting.Configuration{}, err
	}
	return configuration, nil
}

func (s *ModelConfigurationService) Update(id string, configuration modelrouting.Configuration) (modelrouting.Configuration, error) {
	current, err := s.Get(id)
	if err != nil {
		return modelrouting.Configuration{}, err
	}
	if configuration.Backend == "" {
		configuration.Backend = current.Backend
	}
	if strings.TrimSpace(configuration.Backend) != current.Backend {
		return modelrouting.Configuration{}, fmt.Errorf("%w: coding tool cannot change after creation", ErrModelConfigurationInvalid)
	}
	if configuration.State == "" {
		configuration.State = current.State
	}
	configuration.ID, configuration.CreatedAt = current.ID, current.CreatedAt
	configuration.Revision, configuration.UpdatedAt = current.Revision+1, time.Now()
	configuration.Name, configuration.Backend = strings.TrimSpace(configuration.Name), strings.TrimSpace(configuration.Backend)
	// Turning off an unchanged configuration must remain possible after its
	// referenced key/connection has been removed or revoked. Creation, enabling
	// and route edits still require a currently usable concrete binding.
	disableOnly := current.State == "active" && configuration.State == "disabled" && configuration.Name == current.Name && reflect.DeepEqual(configuration.Binding, current.Binding)
	if !disableOnly {
		if err := s.validate(&configuration); err != nil {
			return modelrouting.Configuration{}, err
		}
	}
	row := models.ModelConfiguration(configuration)
	if err := s.db.Save(&row).Error; err != nil {
		return modelrouting.Configuration{}, err
	}
	return configuration, nil
}

func (s *ModelConfigurationService) Delete(id string) error {
	result := s.db.Delete(&models.ModelConfiguration{}, "id = ?", strings.TrimSpace(id))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrModelConfigurationNotFound
	}
	return nil
}

func (s *ModelConfigurationService) validate(configuration *modelrouting.Configuration) error {
	configuration.Name, configuration.Backend = strings.TrimSpace(configuration.Name), strings.TrimSpace(configuration.Backend)
	if configuration.Name == "" || configuration.Backend == "" {
		return fmt.Errorf("%w: name and backend are required", ErrModelConfigurationInvalid)
	}
	if configuration.State == "" {
		configuration.State = "active"
	}
	if configuration.State != "active" && configuration.State != "disabled" {
		return fmt.Errorf("%w: state must be active or disabled", ErrModelConfigurationInvalid)
	}
	if configuration.Binding.Kind != "platform" && configuration.Binding.Kind != "connection" {
		return fmt.Errorf("%w: a concrete platform or connection binding is required", ErrModelConfigurationInvalid)
	}
	if configuration.Binding.ConfigurationID != "" || len(configuration.Binding.ConfigurationIDs) != 0 {
		return fmt.Errorf("%w: concrete model groups cannot reference other configurations", ErrModelConfigurationInvalid)
	}
	if err := modelrouting.NormalizeAuxiliaryModels(&configuration.Binding); err != nil {
		return fmt.Errorf("%w: %w", ErrModelConfigurationInvalid, err)
	}
	resolved, err := NewModelConnectionService(s.db).Resolve(configuration.Backend, configuration.Binding)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrModelConfigurationInvalid, err)
	}
	// Auxiliary models must belong to the same connection at save time. A later
	// catalog removal does not fail automation: only the primary model is required
	// to run, and a missing auxiliary is shown as unavailable instead.
	if resolved.Connection != nil {
		for _, id := range configuration.Binding.AuxiliaryModelIDs {
			if !modelrouting.ModelInCatalog(*resolved.Connection, id) {
				return fmt.Errorf("%w: auxiliary model %s is absent from this connection", ErrModelConfigurationInvalid, id)
			}
		}
	}
	return nil
}
