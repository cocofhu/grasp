package runtime

import "strings"

// EnvACPBridgeReasoningEffort is Codex's model_reasoning_effort, set next to
// the model in the Runtime card.
const EnvACPBridgeReasoningEffort = "ACP_BRIDGE_REASONING_EFFORT"

// cliModelEnvKeys hold a model pick that only one CLI understands.
var cliModelEnvKeys = []string{EnvACPBridgeModel, EnvACPBridgeReasoningEffort}

// SharedEnvForBackend returns the shared env an Agent on backend inherits.
// Model ids are not portable between CLIs, so the shared model pick is dropped
// when the Agent runs a different CLI than the shared baseline. The input map
// is never modified.
func SharedEnvForBackend(env map[string]string, sharedBackend, backend string) map[string]string {
	sharedBackend = strings.TrimSpace(sharedBackend)
	if sharedBackend == "" || sharedBackend == strings.TrimSpace(backend) {
		return env
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	for _, k := range cliModelEnvKeys {
		delete(out, k)
	}
	return out
}
