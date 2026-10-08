package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestApplyPreviewEnvTurnsOnAgentDesktop(t *testing.T) {
	caps := &models.AgentCapabilities{Tools: []string{models.ToolSetPreview}}

	env := map[string]string{}
	applyPreviewEnv(env, caps)
	for _, k := range []string{"PREVIEW_DIRECT", "VNC_PREVIEW", "BROWSER_MCP"} {
		if env[k] != "1" {
			t.Fatalf("%s = %q, want 1 (env %v)", k, env[k], env)
		}
	}

	env = map[string]string{"BROWSER_MCP": "0"}
	applyPreviewEnv(env, caps)
	if env["BROWSER_MCP"] != "0" || env["VNC_PREVIEW"] != "1" {
		t.Fatalf("explicit opt-out must win: %v", env)
	}

	env = map[string]string{}
	applyPreviewEnv(env, &models.AgentCapabilities{})
	if len(env) != 0 {
		t.Fatalf("no set_preview: env must stay empty, got %v", env)
	}
}
