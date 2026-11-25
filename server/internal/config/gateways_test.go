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

func TestGatewayTransportPreviewProxyHardeningAndExpose(t *testing.T) {
	t.Setenv("GRASP_SANDBOX_GATEWAY_TRANSPORT", "gateway")
	t.Setenv("GRASP_SANDBOX_GATEWAY_API_KEY", "synthetic-key")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("sandbox:\n  gateways:\n    - id: e2b\n      url: http://e2b.example.test\n      api_key_env: E2B_GATEWAY_KEY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := parse(path)
	if err != nil {
		t.Fatal(err)
	}
	// Gateway transport must NOT overwrite user-explicit PrivateEndpoints,
	// while PreviewProxyHardening() preserves the exact previous hardening behavior.
	if cfg.Sandbox.PrivateEndpoints {
		t.Fatalf("gateway transport must not mutate PrivateEndpoints: %+v", cfg.Sandbox)
	}
	if !cfg.Sandbox.PreviewProxyHardening() || cfg.Sandbox.GatewayTransport != "gateway" || cfg.Sandbox.Gateways[0].Transport != "gateway" {
		t.Fatalf("transport defaults: %+v", cfg.Sandbox)
	}
	t.Setenv("GRASP_SANDBOX_DEFAULT_GATEWAY", "e2b")
	cfg, err = parse(path)
	if err != nil || cfg.Sandbox.DefaultGateway != "e2b" {
		t.Fatalf("default override: %v", err)
	}
	t.Setenv("GRASP_SANDBOX_GATEWAY_TRANSPORT", "invalid")
	if _, err := parse(path); err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("bad transport: %v", err)
	}
}

func TestPreviewExposeConfigAndEnv(t *testing.T) {
	t.Run("subdomain base url from env; retired alias ignored", func(t *testing.T) {
		t.Setenv("GRASP_SANDBOX_GATEWAY_TRANSPORT", "gateway")
		t.Setenv("GRASP_SANDBOX_GATEWAY_API_KEY", "synthetic-key")
		t.Setenv("GRASP_PUBLIC_ADVERTISE", "http://grasp.example.com")
		t.Setenv("GRASP_PREVIEW_ADVERTISE", "http://alias.other-site.com")
		path := filepath.Join(t.TempDir(), "config.yaml")
		cfg, err := parse(path)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if cfg.Preview.Expose.Subdomain.BaseURL != "" || cfg.PreviewExposePolicy().For("", true).Name() != "none" {
			t.Fatalf("retired alias applied: base=%q strategy=%s", cfg.Preview.Expose.Subdomain.BaseURL, cfg.PreviewExposePolicy().For("", true).Name())
		}
		t.Setenv("GRASP_PREVIEW_EXPOSE_SUBDOMAIN_BASE_URL", "http://primary.other-site.com")
		cfg, err = parse(path)
		if err != nil {
			t.Fatalf("parse override: %v", err)
		}
		if cfg.Preview.Expose.Subdomain.BaseURL != "http://primary.other-site.com" || cfg.PreviewExposePolicy().For("", true).Name() != "subdomain" {
			t.Fatalf("env base = %q strategy=%s", cfg.Preview.Expose.Subdomain.BaseURL, cfg.PreviewExposePolicy().For("", true).Name())
		}
	})

	t.Run("per-gateway preview_expose and validation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		yamlBody := `
server:
  public_advertise: https://grasp.example.com
  deployment_mode: production
preview:
  expose:
    strategy: auto
    subdomain:
      base_url: https://shared.other-site.com
sandbox:
  gateway_url: http://k8s.internal.test
  gateway_transport: direct
  gateways:
    - id: e2b
      url: https://e2b.internal.test
      api_key_env: E2B_KEY
      transport: gateway
      preview_expose:
        strategy: subdomain
        subdomain:
          base_url: https://e2b.third-site.com
`
		if err := os.WriteFile(path, []byte(yamlBody), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := parse(path)
		if err != nil {
			t.Fatalf("parse valid multi-gateway: %v", err)
		}
		pol := cfg.PreviewExposePolicy()
		if pol.For("", true).Name() != "direct" {
			t.Fatalf("legacy = %s, want direct", pol.For("", true).Name())
		}
		if pol.For("e2b", true).Name() != "subdomain" {
			t.Fatalf("e2b = %s, want subdomain", pol.For("e2b", true).Name())
		}

		// Reject gateway transport with explicit direct.
		badYAML := `
server:
  public_advertise: http://grasp.example.com
sandbox:
  gateway_url: http://e2b.internal.test
  gateway_transport: gateway
  gateway_api_key: secret
  preview_expose:
    strategy: direct
`
		if err := os.WriteFile(path, []byte(badYAML), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := parse(path); err == nil {
			t.Fatal("expected gateway transport with direct strategy to fail")
		}
	})
}
