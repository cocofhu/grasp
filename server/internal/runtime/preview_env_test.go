package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestApplyPreviewEnvMapsDirectPreview(t *testing.T) {
	caps := &models.AgentCapabilities{Tools: []string{models.ToolSetPreview}}

	env := map[string]string{}
	ApplyPreviewEnv(env, caps)
	if env["PREVIEW_DIRECT"] != "1" || env["PREVIEW_AUTO_INJECT"] != "1" {
		t.Fatalf("preview env not set: %v", env)
	}
	if len(env) != 3 {
		t.Fatalf("only the direct-preview keys may be set, got %v", env)
	}

	env = map[string]string{}
	ApplyPreviewEnv(env, &models.AgentCapabilities{})
	if len(env) != 0 {
		t.Fatalf("no set_preview: env must stay empty, got %v", env)
	}
}
