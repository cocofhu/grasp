package config

import (
	"strings"
)

// knownSandboxBackends are acpBackend values that accept an optional
// GRASP_SANDBOX_IMAGE_<BACKEND> override. The published image is one
// universal-sandbox that ships every product CLI, including Codex.
var knownSandboxBackends = []string{"cursor", "claude_code", "codebuddy", "trae", "opencode", "codex", "deepseek_harness", "kimi_code"}

// ResolveSandboxImage picks the sandbox image for an acpBackend:
//  1. sandbox.image / GRASP_SANDBOX_IMAGE when non-empty (global force)
//  2. sandbox.images[backend] / GRASP_SANDBOX_IMAGE_<BACKEND>
//  3. Empty: let the selected gateway choose its default image or template.
func (c *Config) ResolveSandboxImage(backend string) string {
	if c != nil {
		if img := strings.TrimSpace(c.Sandbox.Image); img != "" {
			return img
		}
		b := strings.TrimSpace(backend)
		if b == "" {
			b = "cursor"
		}
		if c.Sandbox.Images != nil {
			if img := strings.TrimSpace(c.Sandbox.Images[b]); img != "" {
				return img
			}
		}
		return ""
	}
	return ""
}
