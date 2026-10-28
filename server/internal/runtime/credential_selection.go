package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
)

// SelectedCredentialFunc returns one credential's env key and plaintext.
// ok is false when the id is unknown, cleared, or has no secret.
type SelectedCredentialFunc func(credentialID string) (envKey, value string, ok bool)

// GitCredentialValue resolves the selected secret and its allowed host from the
// same stored row. The host is an authority boundary, not an Agent override.
type GitCredentialValue struct {
	EnvKey string `json:"envKey"`
	Secret string `json:"-"`
	Host   string `json:"host,omitempty"`
}

// GitCredentialFunc returns one selected Git/SSH credential with its metadata.
type GitCredentialFunc func(credentialID string) (GitCredentialValue, bool)

// CredentialEnvKeyForBackend is the workspace-credential env key for a coding
// backend. Every platform credential is bound to its selected coding tool.
func CredentialEnvKeyForBackend(backend string) string {
	switch AcpBackend(strings.TrimSpace(backend)) {
	case BackendCursor:
		return envauth.EnvCursorAPIKey
	case BackendClaudeCode:
		return envauth.EnvClaudeAPIKey
	case BackendCodeBuddy:
		return envauth.EnvCodeBuddyAPIKey
	case BackendTrae:
		return envauth.EnvTraeAPIKey
	case BackendCodex:
		return envauth.EnvCodexAuthFile
	case BackendOpenCode:
		return envauth.EnvOpenCodeAPIKey
	case BackendDeepSeekHarness:
		return envauth.EnvDeepSeekAPIKey
	default:
		return ""
	}
}

// ApplyCredentialBindings applies explicit per-Agent choices after inherited defaults.
// A missing binding inherits; nil and unresolved IDs explicitly remove the value.
func ApplyCredentialBindings(env, creds map[string]string, bindings map[string]*string, resolve SelectedCredentialFunc) {
	for key, id := range bindings {
		key = strings.TrimSpace(key)
		if key == "" || envauth.IsPlatformReservedEnvKey(key) || envauth.IsSecretEnvKey(key) || IsGitAuthorizationEnvKey(key) {
			continue
		}
		delete(env, key)
		delete(creds, key)
		if id == nil || resolve == nil {
			continue
		}
		actual, value, ok := resolve(strings.TrimSpace(*id))
		if ok && actual == key && strings.TrimSpace(value) != "" {
			if env != nil {
				env[key] = value
			}
			if creds != nil {
				creds[key] = value
			}
		}
	}
}
