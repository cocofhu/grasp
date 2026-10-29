package services

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// selectSoloConfiguration selects one of an effective Agent's explicitly bound
// groups. It only constructs the execution view; the saved Agent is untouched.
func selectSoloConfiguration(db *gorm.DB, agent Agent, configurationID string) (Agent, error) {
	if err := modelrouting.ValidateConfigurationReference(agent.ModelBinding); err != nil {
		return Agent{}, soloErr(http.StatusBadRequest, "model_not_in_configuration", "%s", err)
	}
	id := strings.TrimSpace(configurationID)
	if id == "" {
		id = agent.ModelBinding.ConfigurationID
	}
	if !modelrouting.HasConfiguration(agent.ModelBinding, id) {
		return Agent{}, soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型组未绑定到该 Agent")
	}
	configuration, err := NewModelConfigurationService(db).Get(id)
	if err != nil || configuration.State != "active" {
		return Agent{}, soloErr(http.StatusBadRequest, "model_unavailable", "所选模型组不存在或已停用")
	}
	if _, err := ParseAcpBackend(configuration.Backend); err != nil {
		return Agent{}, fmt.Errorf("selected model group: %w", err)
	}
	previousBackend := agent.AcpBackend
	agent.AcpBackend = configuration.Backend
	agent.ModelBinding = modelrouting.ConfigurationReference(id)
	if agent.Layout.ConfigRoot == "" || agent.Layout.ConfigRoot == DefaultConfigRootForBackend(previousBackend) {
		agent.Layout.ConfigRoot = DefaultConfigRootForBackend(configuration.Backend)
	}
	return agent, nil
}

func chatConfigurationID(row *models.SoloSession) string {
	if row.CurrentModelSet {
		return strings.TrimSpace(row.CurrentConfigurationID)
	}
	return strings.TrimSpace(row.InitialConfigurationID)
}

func appliedChatConfigurationID(row *models.SoloSession) string {
	if row.AppliedModelSet {
		return strings.TrimSpace(row.AppliedConfigurationID)
	}
	return strings.TrimSpace(row.InitialConfigurationID)
}

func (s *SoloService) sessionAgent(row *models.SoloSession) (Agent, error) {
	if s.agents == nil {
		return Agent{}, fmt.Errorf("solo unavailable")
	}
	agent, ok := s.agents.Get(row.AgentName)
	if !ok {
		return Agent{}, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 不存在", row.AgentName)
	}
	if s.sbx != nil {
		agent = s.sbx.effectiveAgent(agent)
	} else {
		agent = s.agents.Effective(agent)
	}
	return selectSoloConfiguration(s.db, agent, chatConfigurationID(row))
}

func (s *SoloService) defaultConfigurationID(name string) string {
	if s.agents == nil {
		return ""
	}
	agent, ok := s.agents.Get(name)
	if !ok {
		return ""
	}
	if s.sbx != nil {
		agent = s.sbx.effectiveAgent(agent)
	} else {
		agent = s.agents.Effective(agent)
	}
	if agent.ModelBinding == nil {
		return ""
	}
	return agent.ModelBinding.ConfigurationID
}

// pinLegacyConfigurationIDs initializes only the newly introduced route fields.
// Old sessions used the Agent's one default group. Freeze that reference at
// startup so later editing the default cannot silently change their account.
func (s *SoloService) pinLegacyConfigurationIDs() error {
	if s.db == nil || s.agents == nil {
		return nil
	}
	var rows []models.SoloSession
	if err := s.db.Select("id", "agent_name", "initial_configuration_id", "current_configuration_id", "applied_configuration_id").
		Where("COALESCE(initial_configuration_id, '') = '' OR COALESCE(current_configuration_id, '') = '' OR COALESCE(applied_configuration_id, '') = ''").Find(&rows).Error; err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			initial := row.InitialConfigurationID
			if initial == "" {
				initial = s.defaultConfigurationID(row.AgentName)
			}
			if initial == "" {
				continue
			}
			// When current/applied model markers were absent, both followed the
			// opening route. Explicit group IDs are always preserved.
			for _, column := range []string{"initial_configuration_id", "current_configuration_id", "applied_configuration_id"} {
				if err := tx.Model(&models.SoloSession{}).Where("id = ? AND COALESCE("+column+", '') = ''", row.ID).UpdateColumn(column, initial).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
