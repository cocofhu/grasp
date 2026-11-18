package runtime

import "strings"

// EnvACPBridgeReasoningEffort is the variant for this run. OpenCode receives it
// as model#variant. Tools that already negotiate an effort keep their own path.
const EnvACPBridgeReasoningEffort = "ACP_BRIDGE_REASONING_EFFORT"

// EnvACPBridgeServiceTier selects the chat's independent speed tier. The
// value "default" is an explicit standard reset; unset follows the CLI config.
const EnvACPBridgeServiceTier = "ACP_BRIDGE_SERVICE_TIER"

const serviceTierStandardEnv = "default"

// ServiceTierEnv encodes a chat speed tier; empty is the standard tier.
func ServiceTierEnv(tier string) string {
	if tier = strings.TrimSpace(tier); tier != "" {
		return tier
	}
	return serviceTierStandardEnv
}

// ParseServiceTierEnv decodes ServiceTierEnv; unset and "default" are standard.
func ParseServiceTierEnv(value string) string {
	if value = strings.TrimSpace(value); value != serviceTierStandardEnv {
		return value
	}
	return ""
}

// ModelIndependentEnv copies environment values excluding model routing/auth.
// Model selections use explicit configuration references instead of environment values.
func ModelIndependentEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	ClearModelRoutingEnv(out)
	delete(out, EnvACPBridgeReasoningEffort)
	delete(out, EnvACPBridgeServiceTier)
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
