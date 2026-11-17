package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/service"
)

func TestFastModeUpdatesAreIndependentAndUnsupportedTargetsReset(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("CONFIG_ROOT", t.TempDir())
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv("GRASP_MODEL_CONNECTION", "")
	t.Setenv("ACP_BRIDGE_REASONING_EFFORT", "high")
	t.Setenv("ACP_BRIDGE_FAST_MODE", "false")
	t.Setenv("GRASP_CONFIGURED_MODELS", `[{"id":"primary","fastModeSupported":true,"reasoningEfforts":["low","high"]},{"id":"standard","reasoningEfforts":["high"]}]`)
	chats := service.NewChatManager()
	chats.SetDefaultModel("primary")
	bridge := chats.Default()
	other, err := chats.Create("other", "primary")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if session := bridge.Session(); session != nil {
			_ = session.Close()
		}
	})
	r := newChatsEngine(chats)
	for _, step := range []struct {
		body, model, effort string
		fast, supported     bool
	}{
		{`{"fastMode":true}`, "", "high", true, true},
		{`{"reasoningEffort":"low"}`, "", "low", true, true},
		{`{"model":"standard"}`, "standard", "", false, false},
		{`{"model":"primary","fastMode":true}`, "primary", "", true, true},
		{`{"fastMode":false}`, "primary", "", false, true},
	} {
		response := do(t, r, http.MethodPost, "/api/model", step.body)
		var payload struct {
			FastMode          *bool `json:"fastMode"`
			FastModeSupported bool  `json:"fastModeSupported"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.FastMode == nil || *payload.FastMode != step.fast || payload.FastModeSupported != step.supported || bridge.Model() != step.model || bridge.ReasoningEffort() != step.effort || other.FastMode() {
			t.Fatalf("switch %s: code=%d model=%s effort=%s payload=%s", step.body, response.Code, bridge.Model(), bridge.ReasoningEffort(), response.Body)
		}
	}
	response := do(t, r, http.MethodPost, "/api/model", `{"model":"standard","fastMode":true}`)
	var failure map[string]string
	if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure["code"] != "fast_mode_unavailable" || bridge.Model() != "primary" || bridge.FastMode() {
		t.Fatalf("unsupported Fast changed the chat: %d %s", response.Code, response.Body)
	}
	response = do(t, r, http.MethodGet, "/api/models", "")
	var state struct {
		FastMode          bool                 `json:"fastMode"`
		FastModeSupported bool                 `json:"fastModeSupported"`
		Models            []service.AgentModel `json:"models"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &state) != nil || state.FastMode || !state.FastModeSupported || len(state.Models) != 2 || !state.Models[0].FastModeSupported || state.Models[1].FastModeSupported {
		t.Fatalf("speed capabilities missing from readback: %d %s", response.Code, response.Body)
	}
	connected := bridge.ConnectedPayload(bridge.Session())
	if connected["fastMode"] != false || connected["fastModeSupported"] != true {
		t.Fatalf("speed state missing from reconnect payload: %+v", connected)
	}
}
