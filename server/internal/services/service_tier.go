package services

import (
	"net/http"
	"slices"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func serviceTierUnavailable() error {
	return soloErr(http.StatusBadRequest, "service_tier_unavailable", "所选模型不支持该速度档位")
}

func configurationServiceTiers(configuration modelrouting.Configuration, modelID string) []string {
	if configuration.State != "active" || configuration.Binding.Kind != "platform" {
		return nil
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = strings.TrimSpace(configuration.Binding.ModelID)
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, modelID) {
		return nil
	}
	return modelrouting.ServiceTiers(configuration.Backend, modelID, nil)
}

// configuredServiceTiers never returns nil so API views always carry a list.
func configuredServiceTiers(db *gorm.DB, configurationID, modelID string) []string {
	if db == nil || strings.TrimSpace(configurationID) == "" {
		return []string{}
	}
	configuration, err := NewModelConfigurationService(db).Get(configurationID)
	if err != nil {
		return []string{}
	}
	if tiers := configurationServiceTiers(configuration, modelID); tiers != nil {
		return tiers
	}
	return []string{}
}

// resolveServiceTier applies an optional request to the stored tier. Nil keeps
// the stored tier while the model still offers it; empty is the standard tier.
func resolveServiceTier(stored string, requested *string, tiers []string) (string, error) {
	tier := strings.TrimSpace(stored)
	if requested != nil {
		tier = strings.TrimSpace(*requested)
		if tier != "" && !slices.Contains(tiers, tier) {
			return "", serviceTierUnavailable()
		}
	}
	if !slices.Contains(tiers, tier) {
		tier = ""
	}
	return tier, nil
}

// AcceptServiceTier validates an optional speed selection without changing the
// chosen model or reasoning effort. The standard tier is valid for every tool.
func AcceptServiceTier(db *gorm.DB, configurationID, modelID, tier string) error {
	_, err := resolveServiceTier("", &tier, configuredServiceTiers(db, configurationID, modelID))
	return err
}

func (s *SoloService) serviceTiers(agent Agent, modelID string) []string {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" {
		return []string{}
	}
	return configuredServiceTiers(s.db, agent.ModelBinding.ConfigurationID, modelID)
}

// ServiceTiers describes the session's next-turn model, including its
// selected model group. It does not inherit a previous tool's capability.
func (s *SoloService) ServiceTiers(row *models.SoloSession) []string {
	if row == nil {
		return []string{}
	}
	agent, err := s.sessionAgent(row)
	if err != nil {
		return []string{}
	}
	return s.serviceTiers(agent, chatModelID(row))
}
