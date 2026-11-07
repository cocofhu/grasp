package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// IDs are durable routing keys, not backend names. Keep the old entry while
// its sandboxes exist. Secrets can be supplied through APIKeyEnv.
type GatewayConnectionConfig struct {
	ID        string `yaml:"id"`
	URL       string `yaml:"url"`
	APIKey    string `yaml:"api_key" json:"-"`
	APIKeyEnv string `yaml:"api_key_env"`
	Transport string `yaml:"transport"`
	Profile   string `yaml:"profile"`
}

var gatewayIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func (s SandboxConfig) UsesGatewayTransport() bool {
	if s.GatewayTransport == "gateway" {
		return true
	}
	for _, g := range s.Gateways {
		if g.Transport == "gateway" || g.Transport == "" {
			return true
		}
	}
	return false
}

func (s SandboxConfig) ValidateGateways() error {
	if err := validateGatewayConnection("legacy", s.GatewayURL, s.GatewayTransport, s.GatewayAPIKey, ""); err != nil {
		return err
	}
	ids, urls := map[string]bool{}, map[string]bool{}
	for _, g := range s.Gateways {
		if !gatewayIDPattern.MatchString(g.ID) || g.ID == "legacy" || ids[g.ID] {
			return fmt.Errorf("sandbox.gateways: invalid or duplicate id %q", g.ID)
		}
		ids[g.ID] = true
		if err := validateGatewayConnection(g.ID, g.URL, g.Transport, g.APIKey, g.APIKeyEnv); err != nil {
			return err
		}
		u := strings.TrimRight(g.URL, "/")
		if urls[u] {
			return fmt.Errorf("sandbox.gateways: duplicate URL for %q", g.ID)
		}
		urls[u] = true
	}
	if s.DefaultGateway != "" && !ids[s.DefaultGateway] {
		return fmt.Errorf("sandbox.default_gateway %q is not configured", s.DefaultGateway)
	}
	return nil
}

func validateGatewayConnection(id, rawURL, transport, key, keyEnv string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("sandbox gateway %q requires an HTTP(S) URL without credentials, query or fragment", id)
	}
	if transport != "" && transport != "direct" && transport != "gateway" {
		return fmt.Errorf("sandbox gateway %q transport must be direct or gateway", id)
	}
	if transport == "gateway" && strings.TrimSpace(key) == "" && strings.TrimSpace(keyEnv) == "" {
		return fmt.Errorf("sandbox gateway %q transport requires an API key", id)
	}
	if strings.ContainsAny(key, "\r\n") {
		return fmt.Errorf("sandbox gateway %q API key is not a valid header value", id)
	}
	return nil
}
