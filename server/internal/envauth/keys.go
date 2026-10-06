package envauth

import "strings"

// Env keys written by the built-in project credential presets. Each ACP
// backend has exactly one credential key; runtime maps it onto the CLI key.
const (
	EnvCursorAPIKey    = "GRASP_CURSOR_API_KEY"
	EnvClaudeAPIKey    = "GRASP_CLAUDE_API_KEY"
	EnvCodeBuddyAPIKey = "GRASP_CODEBUDDY_API_KEY"
	EnvTraeAPIKey      = "GRASP_TRAE_API_KEY"
	EnvOpenCodeAPIKey  = "GRASP_OPENCODE_API_KEY"
	// EnvCodexAuthFile is the project-credential slot for a ChatGPT login file
	// (~/.codex/auth.json). It is a secret identity key only: the file body is
	// written to the sandbox as auth.json and must not be injected as an env var.
	EnvCodexAuthFile    = "GRASP_CODEX_AUTH_JSON"
	EnvGitHubToken      = "GITHUB_TOKEN"
	EnvGitLabToken      = "GITLAB_TOKEN"
	EnvGitSSHPrivateKey = "GIT_SSH_PRIVATE_KEY"
	EnvGitSSHKnownHosts = "GIT_SSH_KNOWN_HOSTS"
)

// cliAuthEnvKeys are the env names the in-container ACP CLIs read. They are
// filled from the matching credential key at sandbox start.
var cliAuthEnvKeys = []string{
	"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "CODEBUDDY_API_KEY",
	"TRAECLI_PERSONAL_ACCESS_TOKEN", "OPENCODE_API_KEY",
}

// credentialEnvKeys are the secret preset keys of project credentials.
var credentialEnvKeys = []string{
	EnvCursorAPIKey, EnvClaudeAPIKey, EnvCodeBuddyAPIKey, EnvTraeAPIKey, EnvOpenCodeAPIKey, EnvCodexAuthFile,
	EnvGitHubToken, EnvGitLabToken, EnvGitSSHPrivateKey, EnvGitSSHKnownHosts,
}

// IsPlatformAuthEnvKey reports official ACP CLI auth keys.
func IsPlatformAuthEnvKey(k string) bool {
	for _, key := range cliAuthEnvKeys {
		if k == key {
			return true
		}
	}
	return false
}

// SecretEnvKeys lists env keys that may only be supplied by project
// credentials: the credential preset keys plus the CLI auth keys they map to.
// Agent env, shared Agent env, run env and platform sandbox.env never carry them.
func SecretEnvKeys() []string {
	out := make([]string, 0, len(credentialEnvKeys)+len(cliAuthEnvKeys))
	out = append(out, credentialEnvKeys...)
	return append(out, cliAuthEnvKeys...)
}

// IsSecretEnvKey reports whether k (exact name) is in SecretEnvKeys.
func IsSecretEnvKey(k string) bool {
	k = strings.TrimSpace(k)
	for _, key := range credentialEnvKeys {
		if k == key {
			return true
		}
	}
	return IsPlatformAuthEnvKey(k)
}

// IsPlatformReservedEnvKey reports env keys the platform injects into every
// sandbox (run coordinates, platform MCP endpoints/tokens, layout, passwords).
// User-managed credentials must never shadow them.
func IsPlatformReservedEnvKey(k string) bool {
	switch k {
	case "GRASP_RUN_ID", "GRASP_NODE_ID", "CONFIG_ROOT", "AGENT_PROVIDER",
		"ROOT_PASSWORD", "ACP_BRIDGE_PASSWORD":
		return true
	}
	for _, prefix := range []string{"GRASP_ARTIFACT_", "GRASP_MEMORY_", "GRASP_CONTEXT_", "GRASP_SCHEDULER_", "GRASP_PM_"} {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// OverlayEnv merges shared (base) and agent (overlay) env; Agent wins per key.
func OverlayEnv(shared, agent map[string]string) map[string]string {
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
		out[k] = v
	}
	return out
}

// StripSecretEnvKeys returns a copy of env without SecretEnvKeys.
func StripSecretEnvKeys(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if IsSecretEnvKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}
