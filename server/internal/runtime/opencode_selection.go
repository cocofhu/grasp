package runtime

import "strings"

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
