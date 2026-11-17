package services

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func fastModeUnavailable() error {
	return soloErr(http.StatusBadRequest, "fast_mode_unavailable", "所选模型不支持 Fast 模式")
}

func configurationFastModeSupported(configuration modelrouting.Configuration, modelID string) bool {
	if configuration.State != "active" || configuration.Binding.Kind != "platform" {
		return false
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = strings.TrimSpace(configuration.Binding.ModelID)
	}
	return modelrouting.AcceptsConfiguredModel(configuration.Binding, modelID) && modelrouting.FastModeSupported(configuration.Backend, modelID, nil)
}

func configuredFastModeSupported(db *gorm.DB, configurationID, modelID string) bool {
	if db == nil || strings.TrimSpace(configurationID) == "" {
		return false
	}
	configuration, err := NewModelConfigurationService(db).Get(configurationID)
	return err == nil && configurationFastModeSupported(configuration, modelID)
}

// AcceptFastMode validates an optional speed selection without changing the
// chosen model or reasoning effort. A false value is valid for every tool.
func AcceptFastMode(db *gorm.DB, configurationID, modelID string, enabled bool) error {
	if enabled && !configuredFastModeSupported(db, configurationID, modelID) {
		return fastModeUnavailable()
	}
	return nil
}

func (s *SoloService) fastModeSupported(agent Agent, modelID string) bool {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" {
		return false
	}
	return configuredFastModeSupported(s.db, agent.ModelBinding.ConfigurationID, modelID)
}

// FastModeSupported describes the session's next-turn model, including its
// selected model group. It does not inherit a previous tool's capability.
func (s *SoloService) FastModeSupported(row *models.SoloSession) bool {
	if row == nil {
		return false
	}
	agent, err := s.sessionAgent(row)
	return err == nil && s.fastModeSupported(agent, chatModelID(row))
}
