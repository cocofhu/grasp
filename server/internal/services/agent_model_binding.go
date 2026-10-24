package services

import (
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// SetModelBindingResolver wires validation for explicitly saved bindings.
// Agents can save only named model configuration references.
func (s *AgentService) SetModelBindingResolver(validate func(string, *modelrouting.Binding) error) {
	s.validateModelBinding = validate
}

// SetModelConfigurationResolver derives the CLI from the saved model configuration.
func (s *AgentService) SetModelConfigurationResolver(resolve func(*modelrouting.Binding) (string, error)) {
	s.resolveModelConfiguration = resolve
}

func (s *AgentService) SetGitBindingResolver(validate func(*models.GitBindings) error) {
	s.validateGitBindings = validate
}

// SetSharedAgent supplies defaults for explicitly opted-in model/Git inheritance.
func (s *AgentService) SetSharedAgent(shared *SharedAgentService) { s.shared = shared }

func (s *AgentService) Effective(agent Agent) Agent {
	if s.shared == nil {
		return ExtendOverlay(SharedAgentConfig{}, agent)
	}
	return ExtendOverlay(s.shared.Get(), agent)
}

func withoutModelRoutingEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	runtime.ClearModelRoutingEnv(out)
	return out
}
