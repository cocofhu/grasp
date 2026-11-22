package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"backend/internal/service"
)

func TestServiceTierUpdatesAreIndependentAndUnsupportedTargetsReset(t *testing.T) {
	installFakeCodex(t)
	t.Setenv("CONFIG_ROOT", t.TempDir())
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv("GRASP_MODEL_CONNECTION", "")
	t.Setenv("ACP_BRIDGE_REASONING_EFFORT", "high")
	t.Setenv("ACP_BRIDGE_SERVICE_TIER", "default")
	t.Setenv("GRASP_CONFIGURED_MODELS", `[{"id":"primary","serviceTiers":["fast"],"reasoningEfforts":["low","high"]},{"id":"standard","reasoningEfforts":["high"]}]`)
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
		body, model, effort, tier string
		tiers                     []string
	}{
		{`{"serviceTier":"fast"}`, "", "high", "fast", []string{"fast"}},
		{`{"reasoningEffort":"low"}`, "", "low", "fast", []string{"fast"}},
		{`{"model":"standard"}`, "standard", "", "", nil},
		{`{"model":"primary","serviceTier":"fast"}`, "primary", "", "fast", []string{"fast"}},
		{`{"serviceTier":""}`, "primary", "", "", []string{"fast"}},
	} {
		response := do(t, r, http.MethodPost, "/api/model", step.body)
		var payload struct {
			ServiceTier  *string  `json:"serviceTier"`
			ServiceTiers []string `json:"serviceTiers"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.ServiceTier == nil || *payload.ServiceTier != step.tier || !slices.Equal(payload.ServiceTiers, step.tiers) || bridge.Model() != step.model || bridge.ReasoningEffort() != step.effort || other.ServiceTier() != "" {
			t.Fatalf("switch %s: code=%d model=%s effort=%s payload=%s", step.body, response.Code, bridge.Model(), bridge.ReasoningEffort(), response.Body)
		}
	}
	for _, body := range []string{`{"model":"standard","serviceTier":"fast"}`, `{"serviceTier":"turbo"}`} {
		response := do(t, r, http.MethodPost, "/api/model", body)
		var failure map[string]string
		if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure["code"] != "service_tier_unavailable" || bridge.Model() != "primary" || bridge.ServiceTier() != "" {
			t.Fatalf("unsupported tier %s changed the chat: %d %s", body, response.Code, response.Body)
		}
	}
	response := do(t, r, http.MethodGet, "/api/models", "")
	var state struct {
		ServiceTier  string               `json:"serviceTier"`
		ServiceTiers []string             `json:"serviceTiers"`
		Models       []service.AgentModel `json:"models"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &state) != nil || state.ServiceTier != "" || !slices.Equal(state.ServiceTiers, []string{"fast"}) || len(state.Models) != 2 || !slices.Equal(state.Models[0].ServiceTiers, []string{"fast"}) || state.Models[1].ServiceTiers != nil {
		t.Fatalf("speed capabilities missing from readback: %d %s", response.Code, response.Body)
	}
	connected := bridge.ConnectedPayload(bridge.Session())
	if connected["serviceTier"] != "" || !slices.Equal(connected["serviceTiers"].([]string), []string{"fast"}) {
		t.Fatalf("speed state missing from reconnect payload: %+v", connected)
	}
}
