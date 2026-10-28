package services

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// turnExecutionSnapshot is called once after alignment. row is this turn's
// frozen selection, which may already differ from the next queued turn's choice.
func (s *SoloService) turnExecutionSnapshot(row *models.SoloSession, sandboxID uint, started time.Time) *models.ChatExecution {
	result := &models.ChatExecution{AgentName: row.AgentName, ConfigurationID: chatConfigurationID(row),
		ModelID: chatModelID(row), ReasoningEffort: strings.TrimSpace(row.CurrentReasoningEffort), StartedAt: started.UTC()}
	if s.sbx != nil && s.modelAlign == nil {
		s.sbx.mu.Lock()
		if live := s.sbx.live[sandboxID]; live != nil && live.prepared != nil {
			result.Backend = string(live.prepared.backend)
			if result.ModelID == "" {
				result.ModelID = strings.TrimSpace(live.prepared.spec.Env[runtime.EnvACPBridgeModel])
			}
			if live.soloRouteAgent != nil && live.soloRouteAgent.ModelBinding != nil {
				result.ConfigurationID = live.soloRouteAgent.ModelBinding.ConfigurationID
			}
		}
		s.sbx.mu.Unlock()
		if result.Backend != "" {
			return result
		}
	}
	// A reattached sandbox or test aligner may not retain the prepared bundle.
	// Resolve only the frozen route, never reload the session's current choice.
	if agent, err := s.sessionAgent(row); err == nil {
		result.Backend = agent.AcpBackend
		result.ConfigurationID = agent.ModelBinding.ConfigurationID
		if result.ModelID == "" {
			if configuration, err := NewModelConfigurationService(s.db).Get(result.ConfigurationID); err == nil {
				result.ModelID = strings.TrimSpace(configuration.Binding.ModelID)
			}
		}
	}
	if result.Backend == "" {
		return nil
	}
	return result
}
