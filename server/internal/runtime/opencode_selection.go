package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
)

// ApplyLegacyOpenCodeCredential preserves OpenCode's native provider/catalog
// configuration while resolving only the original selected credential. No
// protocol or endpoint is inferred from its provider name.
func ApplyLegacyOpenCodeCredential(env map[string]string, id string, resolve func(string) map[string]string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	if resolve == nil {
		return "", fmt.Errorf("selected OpenCode credential resolver unavailable")
	}
	selected := resolve(id)
	if strings.TrimSpace(selected[EnvGraspOpenCodeAPIKey]) == "" {
		return "", fmt.Errorf("selected OpenCode credential is missing, cleared, revoked or disabled")
	}
	// Keep legacy non-secret route hints and project configuration. Remove
	// competing coding credentials so vendor env cannot restore another key.
	for key := range env {
		if envauth.IsSecretEnvKey(key) && !IsGitAuthorizationEnvKey(key) {
			delete(env, key)
		}
	}
	for _, key := range []string{"OPENROUTER_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENERATIVE_AI_API_KEY", "GEMINI_API_KEY", "MOONSHOT_API_KEY", "DASHSCOPE_API_KEY", "XAI_API_KEY", "AZURE_OPENAI_API_KEY", "GROQ_API_KEY"} {
		delete(env, key)
	}
	ApplyOpenCodeSelection(env, selected)
	data, _ := json.Marshal(struct {
		ID        string            `json:"id"`
		Selection map[string]string `json:"selection"`
	}{id, selected})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ApplyOpenCodeSelection writes one chosen model-vendor credential into env.
// An empty value removes that key so a credential without a base URL or vision
// flag does not keep a stale Agent setting. A nil or empty selection leaves
// env unchanged: callers use that when the Agent has no valid choice, and must
// not substitute another model-vendor key.
func ApplyOpenCodeSelection(env, selected map[string]string) {
	if env == nil || len(selected) == 0 {
		return
	}
	for k, v := range selected {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if strings.TrimSpace(v) == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}
}
