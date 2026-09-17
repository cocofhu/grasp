package envauth

import "strings"

// IsPlatformAuthEnvKey reports official ACP CLI auth keys that must not be
// injected via platform-level sandbox.env / opts.Env. Project sandbox env may
// store them as a workflow baseline (forced Secret); Agent env overrides.
//
// Kept in a tiny dependency-free package so services and runtime can share the
// rule without services→runtime coupling.
func IsPlatformAuthEnvKey(k string) bool {
	switch k {
	case "CURSOR_API_KEY", "ANTHROPIC_API_KEY", "CODEBUDDY_API_KEY",
		"TRAE_API_KEY", "TRAECLI_PERSONAL_ACCESS_TOKEN", "OPENCODE_API_KEY", "OPENAI_API_KEY", "CODEX_API_KEY":
		return true
	default:
		return false
	}
}

// TokenEnvKeys is the canonical list of Token-class environment variable names
// (ACP auth keys + aliases, and Git tokens). Shared Agent config is the
// preferred place to edit these; Agent Studio should not newly inject them.
func TokenEnvKeys() []string {
	return []string{
		"GRASP_CURSOR_API_KEY", "CURSOR_API_KEY",
		"GRASP_CLAUDE_API_KEY", "ANTHROPIC_API_KEY",
		"GRASP_CODEBUDDY_API_KEY", "CODEBUDDY_API_KEY",
		"GRASP_TRAE_API_KEY", "TRAE_API_KEY", "TRAECLI_PERSONAL_ACCESS_TOKEN",
		"GRASP_OPENCODE_API_KEY", "OPENCODE_API_KEY",
		"OPENAI_API_KEY", "CODEX_API_KEY",
		"GITHUB_TOKEN", "GITLAB_TOKEN", "GIT_SSH_PRIVATE_KEY",
	}
}

// IsTokenEnvKey reports whether k is a Token-class env key (literal name match).
// Does not include GIT_REPOS, URL hosts, known_hosts, or region keys.
func IsTokenEnvKey(k string) bool {
	switch k {
	case "GRASP_CURSOR_API_KEY", "CURSOR_API_KEY",
		"GRASP_CLAUDE_API_KEY", "ANTHROPIC_API_KEY",
		"GRASP_CODEBUDDY_API_KEY", "CODEBUDDY_API_KEY",
		"GRASP_TRAE_API_KEY", "TRAE_API_KEY", "TRAECLI_PERSONAL_ACCESS_TOKEN",
		"GRASP_OPENCODE_API_KEY", "OPENCODE_API_KEY",
		"OPENAI_API_KEY", "CODEX_API_KEY",
		"GITHUB_TOKEN", "GITLAB_TOKEN", "GIT_SSH_PRIVATE_KEY":
		return true
	default:
		return false
	}
}

// MergeEnvSharedTokenPriority: non-Token keys use Agent-over-shared; Token keys
// keep shared when the key exists on shared, otherwise keep Agent stock.
func MergeEnvSharedTokenPriority(shared, agent map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range shared {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	for k, v := range agent {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if IsTokenEnvKey(k) {
			if _, ok := shared[k]; ok {
				continue
			}
		}
		out[k] = v
	}
	return out
}
