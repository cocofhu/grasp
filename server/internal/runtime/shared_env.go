package runtime

// EnvACPBridgeReasoningEffort configures Codex reasoning effort.
const EnvACPBridgeReasoningEffort = "ACP_BRIDGE_REASONING_EFFORT"

// ModelIndependentEnv copies environment values excluding model routing/auth.
// Shared workspace settings cannot select or override an Agent model.
func ModelIndependentEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	ClearModelRoutingEnv(out)
	delete(out, EnvACPBridgeReasoningEffort)
	return out
}
