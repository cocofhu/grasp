package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayConfigurationValidation(t *testing.T) {
	base := SandboxConfig{GatewayURL: "http://legacy.example.test", GatewayTransport: "direct"}
	for _, tc := range []struct {
		name   string
		change func(*SandboxConfig)
	}{
		{"scheme", func(s *SandboxConfig) { s.GatewayURL = "file:///tmp/socket" }},
		{"userinfo", func(s *SandboxConfig) { s.GatewayURL = "http://secret@gateway.test" }},
		{"query", func(s *SandboxConfig) { s.GatewayURL += "?secret=value" }},
		{"empty-query", func(s *SandboxConfig) { s.GatewayURL += "?" }},
		{"header-key", func(s *SandboxConfig) { s.GatewayAPIKey = "synthetic\nkey" }},
		{"fragment", func(s *SandboxConfig) { s.GatewayURL += "#value" }},
		{"transport", func(s *SandboxConfig) { s.GatewayTransport = "auto" }},
		{"missing-key", func(s *SandboxConfig) { s.GatewayTransport = "gateway" }},
		{"missing-default", func(s *SandboxConfig) { s.DefaultGateway = "missing" }},
		{"invalid-id", func(s *SandboxConfig) {
			s.Gateways = []GatewayConnectionConfig{{ID: "../e2b", URL: "http://e2b.test", Transport: "direct"}}
		}},
		{"reserved-id", func(s *SandboxConfig) {
			s.Gateways = []GatewayConnectionConfig{{ID: "legacy", URL: "http://e2b.test", Transport: "direct"}}
		}},
		{"duplicate-id", func(s *SandboxConfig) {
			s.Gateways = []GatewayConnectionConfig{{ID: "e2b", URL: "http://one.test", Transport: "direct"}, {ID: "e2b", URL: "http://two.test", Transport: "direct"}}
		}},
		{"duplicate-url", func(s *SandboxConfig) {
			s.Gateways = []GatewayConnectionConfig{{ID: "one", URL: "http://same.test/", Transport: "direct"}, {ID: "two", URL: "http://same.test", Transport: "direct"}}
		}},
		{"bad-named-url", func(s *SandboxConfig) {
			s.Gateways = []GatewayConnectionConfig{{ID: "e2b", URL: "://bad", Transport: "direct"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.change(&cfg)
			if err := cfg.ValidateGateways(); err == nil {
				t.Fatal("accepted invalid gateway configuration")
			}
		})
	}
	base.Gateways = []GatewayConnectionConfig{{ID: "e2b", URL: "https://e2b.test", APIKeyEnv: "E2B_GATEWAY_KEY", Transport: "gateway"}}
	base.DefaultGateway = "e2b"
	if err := base.ValidateGateways(); err != nil {
		t.Fatal(err)
	}
	if !base.UsesGatewayTransport() {
		t.Fatal("named gateway transport not detected")
	}
	base.GatewayTransport = "gateway"
	if !base.UsesGatewayTransport() {
		t.Fatal("legacy gateway transport not detected")
	}
}

func TestGatewayTransportEnablesIsolatedPreviews(t *testing.T) {
	t.Setenv("GRASP_SANDBOX_GATEWAY_TRANSPORT", "gateway")
	t.Setenv("GRASP_SANDBOX_GATEWAY_API_KEY", "synthetic-key")
	t.Setenv("GRASP_PREVIEW_ADVERTISE", "http://preview.example.test:8080")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("sandbox:\n  gateways:\n    - id: e2b\n      url: http://e2b.example.test\n      api_key_env: E2B_GATEWAY_KEY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Sandbox.PrivateEndpoints || cfg.Sandbox.GatewayTransport != "gateway" || cfg.Sandbox.Gateways[0].Transport != "gateway" {
		t.Fatalf("transport defaults: %+v", cfg.Sandbox)
	}
	t.Setenv("GRASP_SANDBOX_DEFAULT_GATEWAY", "e2b")
	cfg, err = parse(path)
	if err != nil || cfg.Sandbox.DefaultGateway != "e2b" {
		t.Fatalf("default override: %v", err)
	}
	t.Setenv("GRASP_PREVIEW_ADVERTISE", "")
	if _, err := parse(path); err == nil {
		t.Fatal("transport without preview origin accepted")
	}
	t.Setenv("GRASP_SANDBOX_GATEWAY_TRANSPORT", "invalid")
	if _, err := parse(path); err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("bad transport: %v", err)
	}
}
