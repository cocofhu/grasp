package config

import (
	"strings"
)

// knownSandboxBackends are acpBackend values that accept an optional
// GRASP_SANDBOX_IMAGE_<BACKEND> override. The published image is one
// universal-sandbox that ships the supported CLIs.
var knownSandboxBackends = []string{"cursor", "claude_code", "codebuddy", "trae", "opencode", "codex"}

// DefaultSandboxImage is the local tag built from sandbox-gateway/sandbox
// (see ./start.sh sandbox). backend is ignored: one image serves every
// acpBackend; runtime AGENT_PROVIDER selects the live CLI.
func DefaultSandboxImage(backend string) string {
	_ = backend
	return "universal-sandbox:local"
}

// ResolveSandboxImage picks the sandbox image for an acpBackend:
//  1. sandbox.image / GRASP_SANDBOX_IMAGE when non-empty (global force)
//  2. sandbox.images[backend] / GRASP_SANDBOX_IMAGE_<BACKEND>
//  3. DefaultSandboxImage(backend)
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
		return DefaultSandboxImage(b)
	}
	return DefaultSandboxImage(backend)
}
