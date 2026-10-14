package provider

import (
	"strings"
)

// IsReservedSessionEnvKey reports platform / process keys that a connect-time
// session overlay must never shadow. The platform injects these into the
// container (or the process itself); a node overlay can only add node-scoped
// values such as Agent credentials.
func IsReservedSessionEnvKey(k string) bool {
	k = strings.TrimSpace(k)
	if k == "" {
		return false
	}
	switch k {
	case "ROOT_PASSWORD", "ACP_BRIDGE_PASSWORD",
		"AGENT_PROVIDER", "CONFIG_ROOT", "CODEX_HOME",
		"SSH_KEY", "GIT_REPOS",
		"GRASP_RUN_ID",
		"PATH", "HOME", "USER", "PWD", "TERM", "SHELL", "LANG", "LC_ALL":
		return true
	}
	for _, prefix := range []string{
		"GRASP_ARTIFACT_", "GRASP_MEMORY_", "GRASP_CONTEXT_",
		"GRASP_SCHEDULER_", "GRASP_PM_", "SANDBOX_", "GRASP_RUNTIME_",
	} {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// ApplyEnvOverlay merges overlay onto a subprocess environ (KEY=VALUE entries).
// Reserved keys in overlay are dropped so a node cannot shadow platform values
// already present in base. Empty overlay returns a copy of base.
func ApplyEnvOverlay(base []string, overlay map[string]string) []string {
	if len(overlay) == 0 {
		out := make([]string, len(base))
		copy(out, base)
		return out
	}
	idx := make(map[string]int, len(base))
	out := make([]string, len(base))
	copy(out, base)
	for i, kv := range out {
		if j := strings.IndexByte(kv, '='); j > 0 {
			idx[kv[:j]] = i
		}
	}
	for k, v := range overlay {
		k = strings.TrimSpace(k)
		if k == "" || IsReservedSessionEnvKey(k) {
			continue
		}
		entry := k + "=" + v
		if i, ok := idx[k]; ok {
			out[i] = entry
			continue
		}
		idx[k] = len(out)
		out = append(out, entry)
	}
	return out
}

// SanitizeSessionEnv returns a copy of overlay with reserved keys removed.
func SanitizeSessionEnv(overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(overlay))
	for k, v := range overlay {
		k = strings.TrimSpace(k)
		if k == "" || IsReservedSessionEnvKey(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SessionEnvEqual reports whether two sanitized session overlays carry the
// same keys and values.
func SessionEnvEqual(a, b map[string]string) bool {
	a = SanitizeSessionEnv(a)
	b = SanitizeSessionEnv(b)
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// CloneSessionEnv returns a shallow copy of env (nil stays nil).
func CloneSessionEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}
