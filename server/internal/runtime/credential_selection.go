package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
)

// AgentCredentialChoice is the one credential an Agent picked for its coding
// backend and, separately, for its Git method. An empty id keeps the legacy
// single-slot injection when creds still carries that key. When the
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
type SelectedCredentialFunc func(credentialID string) (envKey, value string, ok bool)

// CredentialKindFunc reports the env key stored on one workspace credential.
// ok is false when the id is unknown, revoked, cleared, or has no secret.
// Implementations must not log the secret; this callback does not receive it.
type CredentialKindFunc func(credentialID string) (envKey string, ok bool)

// MergeCodingCredentialIDs chooses the credential ids for a merged Agent.
// A non-empty Agent id wins when kindOf is nil or that id resolves to the
// backend's env key. An id kindOf rejects is dangling: the shared id is used
// when it resolves to that key, matching the inherit-default screen. If the
// shared id is also missing or mismatched, the dangling Agent id is kept so
// injection fail-closes instead of substituting another secret. OpenCode
// consults only the OpenCode slot; every other backend consults only the AI
// slot. The unused slot keeps the individual Agent's id and is never filled
// from shared. A nil kindOf does not copy a shared id.
func MergeCodingCredentialIDs(backend, agentAI, agentOC, sharedAI, sharedOC string, kindOf CredentialKindFunc) (aiID, ocID string) {
	if AcpBackend(strings.TrimSpace(backend)) == BackendOpenCode {
		return strings.TrimSpace(agentAI), pickCredentialSlot(agentOC, sharedOC, envauth.EnvOpenCodeAPIKey, kindOf)
	}
	return pickCredentialSlot(agentAI, sharedAI, CredentialEnvKeyForBackend(backend), kindOf), strings.TrimSpace(agentOC)
}

func pickCredentialSlot(agentID, sharedID, expected string, kindOf CredentialKindFunc) string {
	agentID = strings.TrimSpace(agentID)
	sharedID = strings.TrimSpace(sharedID)
	expected = strings.TrimSpace(expected)
	if agentID != "" && (kindOf == nil || expected == "" || credentialKindMatches(kindOf, agentID, expected)) {
		return agentID
	}
	if sharedID != "" && expected != "" && kindOf != nil && credentialKindMatches(kindOf, sharedID, expected) {
		return sharedID
	}
	return agentID
}

func credentialKindMatches(kindOf CredentialKindFunc, id, expected string) bool {
	key, ok := kindOf(id)
	return ok && strings.TrimSpace(key) == expected
}

// CredentialEnvKeyForBackend is the workspace-credential env key for a coding
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
	case BackendDeepSeekHarness:
		return envauth.EnvDeepSeekAPIKey
	default:
		return ""
	}
}

// CredentialEnvKeyForGit is the workspace-credential env key for a Git method.
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
// and the credential map used for SSH files. A set id removes that
// kind's key first, so a stale or cleared row cannot leave another secret in
// place. An empty id leaves a key that creds still provides (the only
// slot of that kind). If creds omits the key, the empty id deletes it
// so a cleared selection cannot keep or restore another row's secret.
func ApplyAgentCredentialChoice(env, creds map[string]string, choice AgentCredentialChoice, resolve SelectedCredentialFunc) {
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
			if creds != nil {
				if _, ok := creds[expected]; !ok {
					delete(env, expected)
				}
			}
			return
		}
		key, value, ok := resolve(id)
		matched := ok && strings.TrimSpace(key) == expected && strings.TrimSpace(value) != ""
		if env != nil {
			delete(env, expected)
			if matched {
				env[expected] = value
			}
		}
		if creds != nil {
			delete(creds, expected)
			if matched {
				creds[expected] = value
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

// ApplyCredentialBindings applies explicit per-Agent choices after inherited defaults.
// A missing binding inherits; nil and unresolved IDs explicitly remove the value.
func ApplyCredentialBindings(env, creds map[string]string, bindings map[string]*string, resolve SelectedCredentialFunc) {
	for key, id := range bindings {
		key = strings.TrimSpace(key)
		if key == "" || envauth.IsPlatformReservedEnvKey(key) {
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

// CredentialIDForWriteBack records the same row selected for injection. Explicit
// disables and invalid choices never acquire an inherited credential identity.
func CredentialIDForWriteBack(envKey, value, selected string, bindings map[string]*string, resolve SelectedCredentialFunc, inherited func(envKey, value string) string) string {
	if id, explicit := bindings[envKey]; explicit {
		if id == nil {
			return ""
		}
		selected = strings.TrimSpace(*id)
		if selected == "" {
			return ""
		}
	}
	if selected = strings.TrimSpace(selected); selected != "" {
		if resolve != nil {
			key, actual, ok := resolve(selected)
			if ok && key == envKey && actual == value && value != "" {
				return selected
			}
		}
		return ""
	}
	if inherited != nil && value != "" {
		return inherited(envKey, value)
	}
	return ""
}
