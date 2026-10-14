package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
)

func TestPartitionSharedSandboxEnvMovesNodeSecretsToOverlay(t *testing.T) {
	full := map[string]string{
		"AGENT_PROVIDER":     "cursor",
		"CONFIG_ROOT":        "/root/.cursor",
		"ACP_BRIDGE_PASSWORD": "pw",
		"GRASP_RUN_ID":       "run-1",
		"GRASP_NODE_ID":      "node-a",
		"CURSOR_API_KEY":     "secret-a",
		"GITHUB_TOKEN":       "gh-tok",
		"FEATURE_FLAG":       "from-agent",
		"RUN_ONLY":           "run-snap",
	}
	agentKeys := map[string]struct{}{"FEATURE_FLAG": {}}
	container, overlay := partitionSharedSandboxEnv(full, agentKeys)

	if container["CURSOR_API_KEY"] != "" || overlay["CURSOR_API_KEY"] != "secret-a" {
		t.Fatalf("AI key must be overlay-only: container=%q overlay=%q", container["CURSOR_API_KEY"], overlay["CURSOR_API_KEY"])
	}
	if overlay["GRASP_NODE_ID"] != "node-a" {
		t.Fatalf("GRASP_NODE_ID overlay=%q", overlay["GRASP_NODE_ID"])
	}
	if _, ok := container["GRASP_NODE_ID"]; ok {
		t.Fatal("GRASP_NODE_ID must not stay in shared container env")
	}
	if container["GITHUB_TOKEN"] != "gh-tok" {
		t.Fatal("git token must remain run-level for clone")
	}
	if _, ok := overlay["GITHUB_TOKEN"]; ok {
		t.Fatal("git token must not be session overlay")
	}
	if overlay["FEATURE_FLAG"] != "from-agent" || container["FEATURE_FLAG"] != "" {
		t.Fatalf("agent env must be overlay-only, got overlay=%q container=%q", overlay["FEATURE_FLAG"], container["FEATURE_FLAG"])
	}
	if container["RUN_ONLY"] != "run-snap" || container["AGENT_PROVIDER"] != "cursor" {
		t.Fatalf("run/platform keys missing from container: %#v", container)
	}
	if _, ok := overlay["ACP_BRIDGE_PASSWORD"]; ok {
		t.Fatal("password must not appear in overlay")
	}
}

func TestSanitizeNodeEnvOverlayDropsReserved(t *testing.T) {
	got := sanitizeNodeEnvOverlay(map[string]string{
		"CURSOR_API_KEY":      "k",
		"ACP_BRIDGE_PASSWORD": "pw",
		"AGENT_PROVIDER":      "evil",
		"CONFIG_ROOT":         "/evil",
		"GRASP_NODE_ID":       "n1",
		envauth.EnvGitHubToken: "t",
	})
	if got["CURSOR_API_KEY"] != "k" || got["GRASP_NODE_ID"] != "n1" {
		t.Fatalf("got %#v", got)
	}
	for _, k := range []string{"ACP_BRIDGE_PASSWORD", "AGENT_PROVIDER", "CONFIG_ROOT"} {
		if _, ok := got[k]; ok {
			t.Fatalf("reserved %s leaked into overlay", k)
		}
	}
}
