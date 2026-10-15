package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// sharedSandboxFingerprint digests everything that is fixed when a container
// is created: backend, image, layout, container env, ConfigHome inputs and the
// selected credential ids. Secret values are reduced to presence so the digest
// stays stable across token refresh and never encodes a secret. Keys applied
// per bridge chat (model, node id) are excluded.
func sharedSandboxFingerprint(backend AcpBackend, spec sandbox.Spec, home sandbox.ConfigHomeSpec, credentialIDs map[string]string) string {
	env := make(map[string]string, len(spec.Env))
	for k, v := range spec.Env {
		switch {
		case k == EnvACPBridgeModel || k == "GRASP_NODE_ID":
			continue
		case envauth.IsSecretEnvKey(k):
			v = "<set>"
		}
		env[k] = v
	}
	payload := struct {
		Backend      AcpBackend
		Image        string
		ConfigRoot   string
		WorkspaceDir string
		Env          map[string]string
		Home         sandbox.ConfigHomeSpec
		Credentials  map[string]string
		SSHKey       bool
		SSHHosts     bool
	}{
		Backend: backend, Image: spec.Image, ConfigRoot: spec.ConfigRoot, WorkspaceDir: spec.WorkspaceDir,
		Env: env, Home: home, Credentials: credentialIDs,
		SSHKey: strings.TrimSpace(spec.SSHPrivateKey) != "", SSHHosts: strings.TrimSpace(spec.SSHKnownHosts) != "",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// sharedCredentialIDs lists the credential selections that land in the shared
// container (git / SSH, and the Codex login file in ConfigHome). AI keys of
// other backends ride the per-node overlay and do not split containers.
func sharedCredentialIDs(backend AcpBackend, cfg agentFile) map[string]string {
	ids := map[string]string{
		"git":     strings.TrimSpace(cfg.GitCredentialID),
		"gitType": strings.TrimSpace(cfg.GitCredentialType),
		"ssh":     strings.TrimSpace(cfg.SshHostsCredentialID),
	}
	if backend == BackendCodex {
		ids["ai"] = strings.TrimSpace(cfg.AiCredentialID)
	}
	return ids
}

// isNodeSessionEnvKey reports keys that must ride the ACP connect overlay for
// a shared run sandbox instead of the durable container environment. AI/CLI
// auth secrets and Agent-declared env vary per node; git clone tokens stay in
// the container so workspace bootstrap still works at create time.
func isNodeSessionEnvKey(k string, agentEnvKeys map[string]struct{}) bool {
	k = strings.TrimSpace(k)
	if k == "" {
		return false
	}
	if isContainerReservedEnvKey(k) {
		return false
	}
	switch k {
	case envauth.EnvGitHubToken, envauth.EnvGitLabToken,
		envauth.EnvGitSSHPrivateKey, envauth.EnvGitSSHKnownHosts,
		envauth.EnvCodexAuthFile:
		// Clone / SSH inject / Codex login file stay run-scoped.
		return false
	case "GRASP_NODE_ID":
		return true
	}
	if envauth.IsPlatformAuthEnvKey(k) || envauth.IsSecretEnvKey(k) {
		return true
	}
	if _, ok := agentEnvKeys[k]; ok {
		return true
	}
	return false
}

// isContainerReservedEnvKey is the set of platform keys that always belong in
// the shared container env and must never be overridden by a node overlay.
func isContainerReservedEnvKey(k string) bool {
	k = strings.TrimSpace(k)
	if k == "" {
		return false
	}
	// Secret/credential keys are classified by isNodeSessionEnvKey (AI → overlay,
	// git clone tokens → container). They must not be treated as reserved here
	// or node AI credentials would be stuck in the shared container env.
	if envauth.IsSecretEnvKey(k) {
		return false
	}
	if IsDeniedRunSandboxEnvKey(k) {
		// GRASP_NODE_ID is denied in StartRun snapshots but is injected per
		// node session via overlay — treat it as overlay-eligible below.
		if k == "GRASP_NODE_ID" {
			return false
		}
		return true
	}
	if envauth.IsPlatformReservedEnvKey(k) && k != "GRASP_NODE_ID" {
		return true
	}
	switch k {
	case "CODEX_HOME", "GIT_REPOS", "SSH_KEY", "SKIP_INNER_DOCKER",
		"SANDBOX_INJECT", "SANDBOX_INJECT_HEADERS", "GRASP_RUNTIME_URL":
		return true
	}
	if strings.HasPrefix(k, "SANDBOX_") || strings.HasPrefix(k, "GRASP_RUNTIME_") {
		return true
	}
	return false
}

// partitionSharedSandboxEnv splits a fully merged Spec.Env into the durable
// container environment and the per-node ACP session overlay.
func partitionSharedSandboxEnv(full map[string]string, agentEnvKeys map[string]struct{}) (container, overlay map[string]string) {
	container = make(map[string]string, len(full))
	overlay = make(map[string]string)
	for k, v := range full {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if isNodeSessionEnvKey(k, agentEnvKeys) {
			overlay[k] = v
			continue
		}
		container[k] = v
	}
	return container, sanitizeNodeEnvOverlay(overlay)
}

// sanitizeNodeEnvOverlay drops reserved platform keys so a buggy caller cannot
// shadow passwords, layout, or artifact endpoints via connect overlay.
func sanitizeNodeEnvOverlay(overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(overlay))
	for k, v := range overlay {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if k == "GRASP_NODE_ID" {
			out[k] = v
			continue
		}
		if isContainerReservedEnvKey(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// markKeys records every key present in src into dst.
func markKeys(dst map[string]struct{}, src map[string]string) {
	if dst == nil {
		return
	}
	for k := range src {
		k = strings.TrimSpace(k)
		if k != "" {
			dst[k] = struct{}{}
		}
	}
}
