package runtime

import "testing"

func TestIsDeniedRunSandboxEnvKey(t *testing.T) {
	denied := []string{
		"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "CODEBUDDY_API_KEY",
		EnvTraeCLIToken,
		"GITHUB_TOKEN", "GITLAB_TOKEN", "GIT_SSH_PRIVATE_KEY", "GIT_SSH_KNOWN_HOSTS",
		"GRASP_CURSOR_API_KEY", "GRASP_CLAUDE_API_KEY",
		"GRASP_CODEBUDDY_API_KEY", "GRASP_TRAE_API_KEY",
		"GRASP_OPENCODE_API_KEY", "OPENCODE_API_KEY",
		"ROOT_PASSWORD", "ACP_BRIDGE_PASSWORD",
		"GRASP_ARTIFACT_URL", "GRASP_ARTIFACT_TOKEN", "GRASP_ARTIFACT_FOO",
		"GRASP_RUN_ID", "GRASP_NODE_ID",
		"AGENT_PROVIDER", "CONFIG_ROOT", "SSH_KEY", "GIT_REPOS",
	}
	for _, k := range denied {
		if !IsDeniedRunSandboxEnvKey(k) {
			t.Fatalf("expected denied: %s", k)
		}
	}
	allowed := []string{"LOG_LEVEL", "FEATURE_FLAG", "DB_PASSWORD", "MY_GRASP_CUSTOM", "TRAE_API_KEY", "GITLAB_URL", ""}
	for _, k := range allowed {
		if IsDeniedRunSandboxEnvKey(k) {
			t.Fatalf("expected allowed: %q", k)
		}
	}
}
