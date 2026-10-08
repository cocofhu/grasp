package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
)

// AgentCredentialChoice is the one credential an Agent picked for its coding
// backend and, separately, for its Git method. Empty ids keep the legacy
// single-slot injection. A non-empty id replaces that kind and never falls
// back to another row when the id is missing or the wrong kind.
type AgentCredentialChoice struct {
	Backend              string
	GitCredentialType    string
	AiCredentialID       string
	GitCredentialID      string
	SshHostsCredentialID string
}

// SelectedCredentialFunc returns one credential's env key and plaintext.
// ok is false when the id is unknown, cleared, or has no secret.
type SelectedCredentialFunc func(projectID, credentialID string) (envKey, value string, ok bool)

// CredentialEnvKeyForBackend is the project-credential env key for a coding
// backend. OpenCode is selected through its own resolver and returns empty.
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
	default:
		return ""
	}
}

// CredentialEnvKeyForGit is the project-credential env key for a Git method.
func CredentialEnvKeyForGit(gitType string) string {
	switch strings.TrimSpace(gitType) {
	case "github_https":
		return envauth.EnvGitHubToken
	case "gitlab_https":
		return envauth.EnvGitLabToken
	case "ssh":
		return envauth.EnvGitSSHPrivateKey
	default:
		return ""
	}
}

// ApplyAgentCredentialChoice writes the Agent's chosen credentials into env
// and the project credential map used for SSH files. A set id removes that
// kind's key first, so a stale or cleared row cannot leave another secret in
// place. An empty id does not touch the key.
func ApplyAgentCredentialChoice(env, projectCreds map[string]string, projectID string, choice AgentCredentialChoice, resolve SelectedCredentialFunc) {
	if resolve == nil {
		return
	}
	apply := func(id, expected string) {
		id = strings.TrimSpace(id)
		expected = strings.TrimSpace(expected)
		if id == "" || expected == "" {
			return
		}
		key, value, ok := resolve(projectID, id)
		matched := ok && strings.TrimSpace(key) == expected && strings.TrimSpace(value) != ""
		if env != nil {
			delete(env, expected)
			if matched {
				env[expected] = value
			}
		}
		if projectCreds != nil {
			delete(projectCreds, expected)
			if matched {
				projectCreds[expected] = value
			}
		}
	}
	if AcpBackend(strings.TrimSpace(choice.Backend)) != BackendOpenCode {
		apply(choice.AiCredentialID, CredentialEnvKeyForBackend(choice.Backend))
	}
	apply(choice.GitCredentialID, CredentialEnvKeyForGit(choice.GitCredentialType))
	if strings.TrimSpace(choice.GitCredentialType) == "ssh" {
		apply(choice.SshHostsCredentialID, envauth.EnvGitSSHKnownHosts)
	}
}
