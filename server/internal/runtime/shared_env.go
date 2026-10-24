package runtime

// EnvACPBridgeReasoningEffort configures Codex reasoning effort.
const EnvACPBridgeReasoningEffort = "ACP_BRIDGE_REASONING_EFFORT"

// ModelIndependentEnv copies environment values excluding model routing/auth.
// Model selections use explicit configuration references instead of environment values.
func ModelIndependentEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	ClearModelRoutingEnv(out)
	delete(out, EnvACPBridgeReasoningEffort)
	return out
}

// ApplyInheritedModelEnv makes the default model's CLI site travel with its
// authorization. Local site settings remain stored for switching inheritance off.
func ApplyInheritedModelEnv(env, shared map[string]string) {
	for _, key := range []string{EnvCodeBuddyRegion, EnvTraeRegion, EnvCodeBuddyInternet, EnvCodeBuddyBaseURL, EnvTraeCLIHost} {
		delete(env, key)
		if value, exists := shared[key]; exists {
			env[key] = value
		}
	}
}
