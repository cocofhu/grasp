package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
)

func TestDeepSeekHarnessCredentialSelectionFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"env":{"DEEPSEEK_API_KEY":"unselected"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolve := func(id string) (string, string, bool) {
		if id == "chosen" {
			return envauth.EnvDeepSeekAPIKey, "selected", true
		}
		return envauth.EnvOpenCodeAPIKey, "wrong-backend", true
	}
	for _, id := range []string{"chosen", "missing-or-wrong-kind"} {
		env := map[string]string{envauth.EnvDeepSeekAPIKey: "other-slot"}
		ApplyAgentCredentialChoice(env, nil, AgentCredentialChoice{Backend: "deepseek_harness", AiCredentialID: id}, resolve)
		out, err := PrepareAuthEnv(BackendDeepSeekHarness, env, dir)
		if id == "chosen" {
			if err != nil || out["DEEPSEEK_API_KEY"] != "selected" {
				t.Fatalf("chosen key not mapped: err=%v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), envauth.EnvDeepSeekAPIKey) {
			t.Fatalf("wrong credential must fail despite settings.json: %v", err)
		}
	}
	if got := ResolveConfigRoot(BackendDeepSeekHarness, ""); got != "/root/.dsh" {
		t.Fatalf("default config root = %q", got)
	}
}

func TestRunAgent_DeepSeekHarness(t *testing.T) {
	p, _, store, _, _, req := setupProviderBackend(t, BackendDeepSeekHarness, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "dsh complete", produces: map[string]string{"report.md": "dsh report"}}
		}
	})
	spec, err := p.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	defer removeHome(spec.ConfigHome)
	if spec.Env["AGENT_PROVIDER"] != "deepseek_harness" || spec.Env["DEEPSEEK_API_KEY"] != "fake" || spec.ConfigRoot != "/root/.dsh" {
		t.Fatal("incorrect DeepSeek Harness sandbox routing or auth")
	}
	instructions, err := os.ReadFile(filepath.Join(spec.ConfigHome, "AGENTS.md"))
	if err != nil || !strings.Contains(string(instructions), "基础约定") {
		t.Fatalf("platform instructions missing: %v", err)
	}
	result, err := p.RunAgent(context.Background(), req)
	if err != nil || result.OutputMd == "" {
		t.Fatalf("RunAgent failed: %v", err)
	}
	if body, ok := store.Get("run-1", "report.md"); !ok || body != "dsh report" {
		t.Fatal("DeepSeek Harness artifact delivery failed")
	}
}

func TestReact_DeepSeekHarness(t *testing.T) {
	testReactBackend(t, BackendDeepSeekHarness)
}

func TestDeepSeekHarnessRejectsBrokenInstructions(t *testing.T) {
	p, _, _, _, _, req := setupProviderBackend(t, BackendDeepSeekHarness, func(int) chatFunc {
		return func(int) turnAction { return turnAction{} }
	})
	workspace := filepath.Join(p.opts.ProfilesRoot, "test-agent", "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "rules", "broken.md"), []byte("---\nalwaysApply: [broken\n---\nrule"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.spec(req); err == nil || !strings.Contains(err.Error(), "build backend config home") {
		t.Fatalf("instruction adaptation must fail before starting an unconfigured dsh: %v", err)
	}
}

func TestDeepSeekHarnessAliasesCannotComeFromAgentSharedOrPlatformEnv(t *testing.T) {
	root := writeAgent(t, "dsh-agent", `{"acpBackend":"deepseek_harness","env":{"ACP_DEEPSEEK_API_KEY":"agent-key","ACP_DEEPSEEK_HARNESS_API_KEY":"agent-key"}}`)
	p := &acpProvider{
		backend: BackendDeepSeekHarness,
		opts: Options{
			ProfilesRoot: root,
			Env:          map[string]string{"ACP_DEEPSEEK_API_KEY": "platform-key", "ACP_DEEPSEEK_HARNESS_API_KEY": "platform-key"},
			SharedAgent: func() SharedAgentView {
				return SharedAgentView{Env: map[string]string{"ACP_DEEPSEEK_API_KEY": "shared-key", "ACP_DEEPSEEK_HARNESS_API_KEY": "shared-key"}}
			},
			Credentials: testCredentials,
		},
	}
	req := NodeReq{RunID: "run-dsh-secret-alias", Config: map[string]any{"agent_profile": "dsh-agent"}}
	spec, err := p.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	defer removeHome(spec.ConfigHome)
	for _, alias := range []string{"ACP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_HARNESS_API_KEY"} {
		if _, present := spec.Env[alias]; present {
			t.Errorf("%s reached the sandbox outside workspace credentials", alias)
		}
	}
	if spec.Env["DEEPSEEK_API_KEY"] != "fake" {
		t.Fatal("selected workspace credential did not remain authoritative")
	}
}
