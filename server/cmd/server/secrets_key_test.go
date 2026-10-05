package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func secretsCfg(mode, dbPath, key string) *config.Config {
	c := &config.Config{}
	c.Server.DeploymentMode = mode
	c.Database.Driver = "sqlite"
	c.Database.Path = dbPath
	c.Security.SecretsKey = key
	return c
}

func TestSecretsKeyFallbackLocalDemoCreatesStableKeyFile(t *testing.T) {
	dir := t.TempDir()
	cfg := secretsCfg("local-demo", filepath.Join(dir, "grasp.db"), "")
	first := secretsKeyFallback(cfg)
	if first == "" {
		t.Fatal("local-demo without a key must fall back to a generated key")
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.key")); err != nil {
		t.Fatalf("key file not written: %v", err)
	}
	if again := secretsKeyFallback(cfg); again != first {
		t.Fatal("restart must reuse the same key")
	}
}

func TestSecretsKeyFallbackSkipsConfiguredAndProduction(t *testing.T) {
	dir := t.TempDir()
	if got := secretsKeyFallback(secretsCfg("local-demo", filepath.Join(dir, "grasp.db"), "configured")); got != "" {
		t.Fatalf("configured key must win, got fallback %q", got)
	}
	if got := secretsKeyFallback(secretsCfg("production", filepath.Join(dir, "grasp.db"), "")); got != "" {
		t.Fatalf("production must not auto-generate, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.key")); !os.IsNotExist(err) {
		t.Fatalf("no key file expected, stat err = %v", err)
	}
}

// start.sh relies on scripts/ensure-secrets-key.sh to write one stable key into .env.
func TestEnsureSecretsKeyScriptWritesStableKey(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	script, err := filepath.Abs("../../../scripts/ensure-secrets-key.sh")
	if err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("GRASP_PORT=8080\nGRASP_SECRETS_KEY=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func() string {
		out, err := exec.Command(bash, script, envFile).Output()
		if err != nil {
			t.Fatalf("script: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	first := run()
	raw, err := base64.StdEncoding.DecodeString(first)
	if err != nil || len(raw) != 32 {
		t.Fatalf("generated key %q is not base64 of 32 bytes", first)
	}
	if second := run(); second != first {
		t.Fatalf("second run = %q, want the existing key %q", second, first)
	}
	b, _ := os.ReadFile(envFile)
	if n := strings.Count(string(b), "GRASP_SECRETS_KEY="); n != 1 {
		t.Fatalf(".env has %d GRASP_SECRETS_KEY lines:\n%s", n, b)
	}
	if !strings.Contains(string(b), "GRASP_SECRETS_KEY="+first+"\n") || !strings.Contains(string(b), "GRASP_PORT=8080") {
		t.Fatalf(".env not updated in place:\n%s", b)
	}
}
