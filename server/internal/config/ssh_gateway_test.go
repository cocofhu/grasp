package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHGatewayEnvAndDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "none.yaml")
	c, err := parse(missing)
	if err != nil {
		t.Fatal(err)
	}
	want := SSHGatewayConfig{
		Listen: ":2222", PublicPort: 2222, HostKeyPath: "data/ssh-gateway/host_ed25519_key",
		MaxConns: 200, MaxConnsPerSandbox: 10,
	}
	if c.SSHGateway != want {
		t.Fatalf("defaults = %+v", c.SSHGateway)
	}

	t.Setenv("GRASP_SSH_GATEWAY_ENABLED", "true")
	t.Setenv("GRASP_SSH_GATEWAY_LISTEN", "0.0.0.0:2200")
	t.Setenv("GRASP_SSH_GATEWAY_PUBLIC_HOST", "ssh.grasp.example.com")
	t.Setenv("GRASP_SSH_GATEWAY_HOST_KEY_PATH", "/data/host_key")
	t.Setenv("GRASP_SSH_GATEWAY_MAX_CONNS", "50")
	t.Setenv("GRASP_SSH_GATEWAY_MAX_CONNS_PER_SANDBOX", "4")
	t.Setenv("GRASP_SSH_GATEWAY_ADMINS_ONLY", "true")
	c, err = parse(missing)
	if err != nil {
		t.Fatal(err)
	}
	want = SSHGatewayConfig{
		Enabled: true, Listen: "0.0.0.0:2200", PublicHost: "ssh.grasp.example.com", PublicPort: 2200,
		HostKeyPath: "/data/host_key", MaxConns: 50, MaxConnsPerSandbox: 4, AdminsOnly: true,
	}
	if c.SSHGateway != want {
		t.Fatalf("env = %+v", c.SSHGateway)
	}

	t.Setenv("GRASP_SSH_GATEWAY_PUBLIC_PORT", "443")
	if c, err = parse(missing); err != nil || c.SSHGateway.PublicPort != 443 {
		t.Fatalf("public port override = %+v %v", c, err)
	}

	t.Setenv("GRASP_SSH_GATEWAY_LISTEN", "2222")
	if _, err := parse(missing); err == nil {
		t.Fatal("listen without a port separator must fail startup")
	}
}

func TestValidateSSHGateway(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  SSHGatewayConfig
		ok   bool
	}{
		{"disabled ignores fields", SSHGatewayConfig{Listen: "bad"}, true},
		{"ok", SSHGatewayConfig{Enabled: true, Listen: ":2222", PublicPort: 2222, PublicHost: "ssh.example.com"}, true},
		{"bad listen", SSHGatewayConfig{Enabled: true, Listen: "localhost"}, false},
		{"zero port", SSHGatewayConfig{Enabled: true, Listen: ":0"}, false},
		{"bad public port", SSHGatewayConfig{Enabled: true, Listen: ":2222", PublicPort: 70000}, false},
		{"host with scheme", SSHGatewayConfig{Enabled: true, Listen: ":2222", PublicHost: "ssh://x"}, false},
	} {
		if err := validateSSHGateway(tc.cfg); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestSSHGatewayEmptyEnvKeepsYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grasp.yaml")
	if err := os.WriteFile(path, []byte("ssh_gateway:\n  enabled: true\n  admins_only: true\n  listen: \":2200\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"GRASP_SSH_GATEWAY_ENABLED", "GRASP_SSH_GATEWAY_ADMINS_ONLY", "GRASP_SSH_GATEWAY_LISTEN", "GRASP_SSH_GATEWAY_PUBLIC_PORT", "GRASP_SSH_GATEWAY_MAX_CONNS"} {
		t.Setenv(key, "") // what Compose passes for an unset ${VAR:-}
	}
	c, err := parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SSHGateway.Enabled || !c.SSHGateway.AdminsOnly || c.SSHGateway.Listen != ":2200" || c.SSHGateway.PublicPort != 2200 {
		t.Fatalf("empty env overrode YAML: %+v", c.SSHGateway)
	}
}
