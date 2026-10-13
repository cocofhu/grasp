package runtime

import "testing"

func TestSharedEnvForBackend(t *testing.T) {
	env := map[string]string{EnvACPBridgeModel: "gpt-6.1-sol", EnvACPBridgeReasoningEffort: "high", "KEEP": "1"}

	if got := SharedEnvForBackend(env, "codex", "codex"); got[EnvACPBridgeModel] != "gpt-6.1-sol" {
		t.Fatalf("same CLI keeps the model: %#v", got)
	}
	if got := SharedEnvForBackend(env, "", "cursor"); got[EnvACPBridgeModel] != "gpt-6.1-sol" {
		t.Fatalf("a shared baseline without a backend is kept as-is: %#v", got)
	}

	got := SharedEnvForBackend(env, "codex", "cursor")
	if _, ok := got[EnvACPBridgeModel]; ok {
		t.Fatalf("model leaked across CLIs: %#v", got)
	}
	if _, ok := got[EnvACPBridgeReasoningEffort]; ok {
		t.Fatalf("effort leaked across CLIs: %#v", got)
	}
	if got["KEEP"] != "1" {
		t.Fatalf("other keys stay: %#v", got)
	}
	if env[EnvACPBridgeModel] != "gpt-6.1-sol" {
		t.Fatal("input mutated")
	}
}

func TestOverlayAgentFileDropsSharedModelForOtherCLI(t *testing.T) {
	shared := SharedAgentView{AcpBackend: "codex", Env: map[string]string{EnvACPBridgeModel: "gpt-6.1-sol"}}

	if got := overlayAgentFile(shared, agentFile{}, nil); got.AcpBackend != "codex" || got.Env[EnvACPBridgeModel] != "gpt-6.1-sol" {
		t.Fatalf("an Agent inheriting the CLI inherits the model: %#v", got)
	}
	if got := overlayAgentFile(shared, agentFile{AcpBackend: "cursor"}, nil); got.Env[EnvACPBridgeModel] != "" {
		t.Fatalf("a Codex model must not reach Cursor: %#v", got.Env)
	}
}
