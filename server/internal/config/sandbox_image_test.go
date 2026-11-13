package config

import "testing"

func TestResolveSandboxImage(t *testing.T) {
	if got := (*Config)(nil).ResolveSandboxImage("trae"); got != "" {
		t.Fatalf("nil config: %q", got)
	}
	c := &Config{Sandbox: SandboxConfig{
		Images: map[string]string{"cursor": "reg/cursor:pin"},
	}}
	if got := c.ResolveSandboxImage("cursor"); got != "reg/cursor:pin" {
		t.Fatalf("per-backend: %q", got)
	}
	if got := c.ResolveSandboxImage("claude_code"); got != "" {
		t.Fatalf("fallback default: %q", got)
	}
	c.Sandbox.Image = "reg/force:1"
	if got := c.ResolveSandboxImage("claude_code"); got != "reg/force:1" {
		t.Fatalf("global force: %q", got)
	}
}

func TestApplySandboxImageEnv(t *testing.T) {
	t.Setenv("GRASP_SANDBOX_IMAGE_CURSOR", "env/cursor:1")
	t.Setenv("GRASP_SANDBOX_IMAGE_CLAUDE_CODE", "env/claude:1")
	t.Setenv("GRASP_SANDBOX_IMAGE_DEEPSEEK_HARNESS", "env/dsh:1")
	c := &Config{}
	applySandboxImageEnv(c)
	if c.Sandbox.Images["cursor"] != "env/cursor:1" {
		t.Fatalf("cursor env: %v", c.Sandbox.Images)
	}
	if c.Sandbox.Images["claude_code"] != "env/claude:1" {
		t.Fatalf("claude_code env: %v", c.Sandbox.Images)
	}
	if got := c.ResolveSandboxImage("deepseek_harness"); got != "env/dsh:1" {
		t.Fatalf("DeepSeek Harness image = %q", got)
	}
}

func TestApplySandboxImageEnvSkipsEmptyOpenCode(t *testing.T) {
	t.Setenv("GRASP_SANDBOX_IMAGE_OPENCODE", "")
	c := &Config{}
	applySandboxImageEnv(c)
	if _, ok := c.Sandbox.Images["opencode"]; ok {
		t.Fatalf("empty OPENCODE env must not pin a missing GHCR tag: %v", c.Sandbox.Images)
	}
	if got := c.ResolveSandboxImage("opencode"); got != "" {
		t.Fatalf("opencode fallback = %q", got)
	}
}
