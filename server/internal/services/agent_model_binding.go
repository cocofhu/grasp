package services

import (
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/runtime"
)

// SetModelBindingResolver wires production validation and the read-only mapping
// of pre-connection Agent data. Tests and standalone import tooling may omit it.
func (s *AgentService) SetModelBindingResolver(validate func(string, *modelrouting.Binding) error, legacy func(string, string, string, map[string]string) *modelrouting.Binding) {
	s.validateModelBinding = validate
	s.legacyModelBinding = legacy
}

func (s *AgentService) readModelBinding(cfg agentConfig) *modelrouting.Binding {
	if cfg.ModelBinding != nil {
		return cfg.ModelBinding
	}
	if s.legacyModelBinding != nil {
		return s.legacyModelBinding(cfg.AcpBackend, cfg.AiCredentialID, cfg.OpenCodeCredentialID, cfg.Env)
	}
	return nil
}

func clearLegacyModelSelection(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	runtime.ClearModelRoutingEnv(out)
	return out
}
