package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
)

// AgentCredentialChoice is the one credential an Agent picked for its coding
// backend and, separately, for its Git method. An empty id keeps the legacy
// single-slot injection when projectCreds still carries that key. When the
// key is absent — a second row of that kind exists, or the chosen row was
// cleared — the empty id removes the key instead of falling back. A non-empty
// id replaces that kind and never falls back to another row.
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

// CredentialKindFunc reports the env key stored on one project credential.
// ok is false when the id is unknown, revoked, cleared, or has no secret.
// Implementations must not log the secret; this callback does not receive it.
type CredentialKindFunc func(credentialID string) (envKey string, ok bool)

// MergeCodingCredentialIDs chooses the credential ids for a merged Agent.
// A non-empty id on the individual Agent wins. Otherwise the shared id is
// used only when its env key matches the effective backend. OpenCode consults
// only the OpenCode slot; every other backend consults only the AI slot.
// The unused slot keeps the individual Agent's id and is never filled from
// shared. A nil kindOf, an unknown id, or a mismatched key leaves that slot
// empty so callers stay on the existing empty-selection path.
func MergeCodingCredentialIDs(backend, agentAI, agentOC, sharedAI, sharedOC string, kindOf CredentialKindFunc) (aiID, ocID string) {
	if AcpBackend(strings.TrimSpace(backend)) == BackendOpenCode {
		return strings.TrimSpace(agentAI), pickCredentialSlot(agentOC, sharedOC, envauth.EnvOpenCodeAPIKey, kindOf)
	}
	return pickCredentialSlot(agentAI, sharedAI, CredentialEnvKeyForBackend(backend), kindOf), strings.TrimSpace(agentOC)
}

func pickCredentialSlot(agentID, sharedID, expected string, kindOf CredentialKindFunc) string {
	if id := strings.TrimSpace(agentID); id != "" {
		return id
	}
	sharedID = strings.TrimSpace(sharedID)
	expected = strings.TrimSpace(expected)
	if sharedID == "" || expected == "" || kindOf == nil {
		return ""
	}
	key, ok := kindOf(sharedID)
	if !ok || strings.TrimSpace(key) != expected {
		return ""
	}
	return sharedID
}

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
// place. An empty id leaves a key that projectCreds still provides (the only
// slot of that kind). If projectCreds omits the key, the empty id deletes it
// so a cleared selection cannot keep or restore another row's secret.
func ApplyAgentCredentialChoice(env, projectCreds map[string]string, projectID string, choice AgentCredentialChoice, resolve SelectedCredentialFunc) {
	if resolve == nil {
		return
	}
	apply := func(id, expected string) {
		id = strings.TrimSpace(id)
		expected = strings.TrimSpace(expected)
		if expected == "" {
			return
		}
		if id == "" {
			if projectCreds != nil {
				if _, ok := projectCreds[expected]; !ok {
					delete(env, expected)
				}
			}
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
