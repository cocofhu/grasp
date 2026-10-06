package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// IsDeniedRunSandboxEnvKey reports keys that must not appear in a StartRun
// run-scoped sandbox env snapshot: secret keys (project credentials only),
// ApplyPasswords, mcpVars reserved keys, and manager injects
// (AGENT_PROVIDER / CONFIG_ROOT / SSH_KEY / GIT_REPOS). Callers reject the whole
// start when any such key is present (no silent drop).
func IsDeniedRunSandboxEnvKey(k string) bool {
	k = strings.TrimSpace(k)
	if k == "" {
		return false
	}
	if envauth.IsSecretEnvKey(k) {
		return true
	}
	switch k {
	case // ApplyPasswords
		"ROOT_PASSWORD", sandbox.BridgePasswordEnv,
		// mcpVars reserved (exact)
		"GRASP_ARTIFACT_URL", "GRASP_ARTIFACT_TOKEN",
		"GRASP_RUN_ID", "GRASP_NODE_ID",
		// platform write-backs / manager injects
		"AGENT_PROVIDER", "CONFIG_ROOT", "SSH_KEY", "GIT_REPOS":
		return true
	}
	return strings.HasPrefix(k, "GRASP_ARTIFACT_")
}
