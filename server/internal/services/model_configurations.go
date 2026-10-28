package services

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/execution"
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
	if execution.Local() {
		_ = s.EnsureLocalDefaults()
	}
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

// EnsureLocalDefaults seeds default host-auth model configurations for installed
// Agent CLIs when running in local execution mode and no configurations exist yet.
func (s *ModelConfigurationService) EnsureLocalDefaults() error {
	if !execution.Local() || s == nil || s.db == nil {
		return nil
	}
	var existing []models.ModelConfiguration
	if err := s.db.Select("id", "backend").Find(&existing).Error; err != nil {
		return err
	}
	backends := execution.InstalledProviders()
	if len(existing) == 0 && len(backends) == 0 {
		backends = execution.KnownProviders
	}
	seenID := make(map[string]bool, len(existing))
	seenBackend := make(map[string]bool, len(existing))
	for _, row := range existing {
		seenID[row.ID] = true
		seenBackend[row.Backend] = true
	}
	now := time.Now()
	for i, backend := range backends {
		id := "model-local-" + strings.ReplaceAll(backend, "_", "-")
		if seenID[id] || seenBackend[backend] {
			continue
		}
		row := models.ModelConfiguration{
			ID:        id,
			Name:      localToolLabel(backend),
			Backend:   backend,
			Binding:   modelrouting.Binding{Kind: "local"},
			State:     "active",
			Revision:  1,
			CreatedAt: now.Add(-time.Duration(i) * time.Millisecond),
			UpdatedAt: now.Add(-time.Duration(i) * time.Millisecond),
		}
		if err := s.db.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func localToolLabel(backend string) string {
	switch backend {
	case "claude_code":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "cursor":
		return "Cursor"
	case "opencode":
		return "OpenCode"
	case "deepseek_harness":
		return "DeepSeek Harness"
	case "kimi_code":
		return "Kimi Code"
	case "codebuddy":
		return "CodeBuddy"
	case "trae":
		return "Trae"
	default:
		return backend
	}
}

func (s *ModelConfigurationService) Get(id string) (modelrouting.Configuration, error) {
	if execution.Local() {
		_ = s.EnsureLocalDefaults()
	}
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
	if execution.Local() && (configuration.Binding.Kind == "" || (configuration.Binding.Kind == "platform" && strings.TrimSpace(configuration.Binding.CredentialID) == "")) {
		configuration.Binding.Kind = "local"
	}
	if configuration.Binding.Kind != "platform" && configuration.Binding.Kind != "connection" && !(execution.Local() && configuration.Binding.Kind == "local") {
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
