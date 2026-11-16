package oneshot

import (
	"context"
	"testing"

	"backend/internal/provider"
)

func TestCLIServesConfig(t *testing.T) {
	catalog := []provider.Model{{ID: "m1"}}
	c := NewCLI(CLIConfig{
		AgentName: provider.Pi, Bin: "pi", Runtime: "pi-runtime", ConfigRoot: "/root/.pi",
		AuthEnvFn: func(env []string) []string { return append(env, "PI_KEY=x") },
		Catalog:   catalog,
	})
	if c.AgentName() != provider.Pi || c.Bin() != "pi" || c.Runtime() != "pi-runtime" || c.ConfigRoot() != "/root/.pi" {
		t.Fatalf("identity: %+v", c)
	}
	if !c.ReportsUsage() || c.PromptViaStdin() {
		t.Fatal("defaults: usage on, prompt on argv")
	}
	if env := c.AuthEnv([]string{"A=1"}); len(env) != 2 || env[1] != "PI_KEY=x" {
		t.Fatalf("auth env: %v", env)
	}
	if models, err := c.Models(context.Background()); err != nil || len(models) != 1 || models[0].ID != "m1" {
		t.Fatalf("models: %v %v", models, err)
	}
	if env := NewCLI(CLIConfig{}).AuthEnv([]string{"A=1"}); len(env) != 1 {
		t.Fatalf("nil AuthEnvFn must pass env through: %v", env)
	}
}
