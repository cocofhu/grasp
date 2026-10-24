package services

import (
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/runtime"
)

// SetModelBindingResolver wires validation for explicitly saved bindings.
// Agents can save only named model configuration references.
func (s *AgentService) SetModelBindingResolver(validate func(string, *modelrouting.Binding) error) {
	s.validateModelBinding = validate
}

func withoutModelRoutingEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	runtime.ClearModelRoutingEnv(out)
	return out
}
