package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSetDefaults(t *testing.T) {
	c := &Config{}
	setDefaults(c)

	if c.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", c.Server.Port)
	}
	if c.Database.Path != "grasp.db" {
		t.Errorf("default db = %q, want grasp.db", c.Database.Path)
	}
	if c.Engine.ExecProvider != "sandbox" {
		t.Errorf("default exec_provider = %q, want sandbox", c.Engine.ExecProvider)
	}
	if c.Engine.MaxConcurrentRuns != 5 {
		t.Errorf("default max runs = %d, want 5", c.Engine.MaxConcurrentRuns)
	}
	if c.Engine.ProfilesRoot != "data/profiles" {
		t.Errorf("default profiles_root = %q", c.Engine.ProfilesRoot)
	}
	if c.Engine.PlatformRulesRoot != "data/platform-rules" {
		t.Errorf("default platform_rules_root = %q", c.Engine.PlatformRulesRoot)
	}
	if c.Storage.Driver != "local" {
		t.Errorf("default storage.driver = %q", c.Storage.Driver)
	}
	if c.Storage.BlobsRoot != "data/blobs" {
		t.Errorf("default storage.blobs_root = %q", c.Storage.BlobsRoot)
	}
	if c.Sandbox.AgentChatTimeoutSeconds != 600 {
		t.Errorf("default timeout = %d, want 600", c.Sandbox.AgentChatTimeoutSeconds)
	}
	if c.Sandbox.ChatIdleTimeoutSeconds != 720 {
		t.Errorf("default idle timeout = %d, want 720", c.Sandbox.ChatIdleTimeoutSeconds)
	}
	want := fmt.Sprintf("http://host.docker.internal:%d", c.Server.Port)
	if c.Server.MCPAdvertise != want {
		t.Errorf("default mcp_advertise = %q, want %q", c.Server.MCPAdvertise, want)
	}
}

func TestRewriteMisconfiguredMCPAdvertise(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://spa.example.com", "http://spa.example.com"},
		{"  http://host.docker.internal:8080  ", "http://host.docker.internal:8080"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := RewriteMisconfiguredMCPAdvertise(tc.in); got != tc.want {
			t.Errorf("RewriteMisconfiguredMCPAdvertise(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSetDefaultsPreservesMCPAdvertise(t *testing.T) {
	c := &Config{Server: ServerConfig{Port: 8080, MCPAdvertise: "http://api.example.com"}}
	setDefaults(c)
	if c.Server.MCPAdvertise != "http://api.example.com" {
		t.Errorf("setDefaults mcp_advertise = %q, want unchanged", c.Server.MCPAdvertise)
	}
}

func TestEffectiveMCPAdvertiseUsesLiveConfig(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { StoreConfig(prev) })

	StoreConfig(nil)
	if got := EffectiveMCPAdvertise(); got != "" {
		t.Fatalf("nil config: got %q, want empty", got)
	}

	StoreConfig(&Config{Server: ServerConfig{MCPAdvertise: "http://api.example.com/mcp-base"}})
	if got := EffectiveMCPAdvertise(); got != "http://api.example.com/mcp-base" {
		t.Fatalf("EffectiveMCPAdvertise = %q", got)
	}
}

func TestResolveMCPAdvertiseFallsBack(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { StoreConfig(prev) })

	StoreConfig(nil)
	got := ResolveMCPAdvertise("http://api.example.com")
	if got != "http://api.example.com" {
		t.Fatalf("fallback: got %q", got)
	}
	if got := ResolveMCPAdvertise("http://host.docker.internal:8080"); got != "http://host.docker.internal:8080" {
		t.Fatalf("docker default broken: %q", got)
	}

	StoreConfig(&Config{Server: ServerConfig{MCPAdvertise: "http://live.example.com"}})
	if got := ResolveMCPAdvertise("http://should-not-use"); got != "http://live.example.com" {
		t.Fatalf("live wins: got %q", got)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	c := &Config{}
	t.Setenv("GRASP_PORT", "7000")
	t.Setenv("GRASP_CURSOR_API_KEY", "crsr_env")
	t.Setenv("GRASP_SANDBOX_IMAGE", "env/img:1")
	applyEnvOverrides(c)

	if c.Server.Port != 7000 {
		t.Errorf("GRASP_PORT not applied: %d", c.Server.Port)
	}
	if c.Sandbox.CursorAPIKey != "crsr_env" {
		t.Errorf("GRASP_CURSOR_API_KEY not applied: %q", c.Sandbox.CursorAPIKey)
	}
	if c.Sandbox.Image != "env/img:1" {
		t.Errorf("GRASP_SANDBOX_IMAGE not applied: %q", c.Sandbox.Image)
	}
}

func TestApplyEnvOverridesBareSecretName(t *testing.T) {
	c := &Config{}
	t.Setenv("CURSOR_API_KEY", "bare_key")
	applyEnvOverrides(c)
	if c.Sandbox.CursorAPIKey != "bare_key" {
		t.Errorf("CURSOR_API_KEY not applied: %q", c.Sandbox.CursorAPIKey)
	}
}

func TestApplyEnvOverridesEmptyDoesNotOverwrite(t *testing.T) {
	c := &Config{Sandbox: SandboxConfig{CursorAPIKey: "keep"}}
	t.Setenv("GRASP_CURSOR_API_KEY", "")
	t.Setenv("CURSOR_API_KEY", "")
	applyEnvOverrides(c)
	if c.Sandbox.CursorAPIKey != "keep" {
		t.Errorf("empty env overwrote cursor key: %q", c.Sandbox.CursorAPIKey)
	}
}

func TestLoadRoundTripPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlBody := `
server:
  port: 6000
database:
  path: "/tmp/file.db"
engine:
  exec_provider: cursor
  max_concurrent_runs: 9
sandbox:
  image: "file/img:1"
  cursor_api_key: "file_key"
  agent_chat_timeout_seconds: 120
`
	if err := os.WriteFile(path, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// env wins over file for port + the cursor secret.
	t.Setenv("GRASP_PORT", "7777")
	t.Setenv("GRASP_CURSOR_API_KEY", "env_key")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := GetConfig()
	if c.Server.Port != 7777 {
		t.Errorf("env should win for port: %d", c.Server.Port)
	}
	if c.Sandbox.CursorAPIKey != "env_key" {
		t.Errorf("env should win for cursor key: %q", c.Sandbox.CursorAPIKey)
	}
	if c.Database.Path != "/tmp/file.db" {
		t.Errorf("file value lost: %q", c.Database.Path)
	}
	if c.Engine.MaxConcurrentRuns != 9 {
		t.Errorf("file max runs lost: %d", c.Engine.MaxConcurrentRuns)
	}
	if got := c.AgentChatTimeout().Seconds(); got != 120 {
		t.Errorf("AgentChatTimeout() = %vs, want 120", got)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	// Missing file must NOT error — zero-config boot from env/defaults.
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	if err := Load(path); err != nil {
		t.Fatalf("Load missing file should not error: %v", err)
	}
	c := GetConfig()
	if c.Server.Port != 8080 {
		t.Errorf("missing-file boot lost defaults: port=%d", c.Server.Port)
	}
	if c.Engine.ExecProvider != "sandbox" {
		t.Errorf("missing-file boot lost defaults: exec=%q", c.Engine.ExecProvider)
	}
}

func TestReloadSwapsLiveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("engine:\n  max_concurrent_runs: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := GetConfig().Engine.MaxConcurrentRuns; got != 2 {
		t.Fatalf("initial max runs = %d, want 2", got)
	}

	if err := os.WriteFile(path, []byte("engine:\n  max_concurrent_runs: 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newCfg, err := Reload(path)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if newCfg.Engine.MaxConcurrentRuns != 8 {
		t.Errorf("reloaded max runs = %d, want 8", newCfg.Engine.MaxConcurrentRuns)
	}
	if GetConfig().Engine.MaxConcurrentRuns != 8 {
		t.Errorf("Reload did not swap live config: %d", GetConfig().Engine.MaxConcurrentRuns)
	}
}

func TestParseMalformedYAMLErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server: [not-a-map\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parse(path); err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}
