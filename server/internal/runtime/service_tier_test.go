package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestAlignServiceTierIndependentlyAndResetAtDefaultRoute(t *testing.T) {
	tier := ""
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path == "/api/model" {
			var body struct {
				Model           string  `json:"model"`
				ReasoningEffort string  `json:"reasoningEffort"`
				ServiceTier     *string `json:"serviceTier"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Model != "" || body.ReasoningEffort != "ultra" || body.ServiceTier == nil {
				t.Errorf("speed changed model/effort or omitted explicit standard: %+v", body)
			}
			if body.ServiceTier != nil {
				tier = *body.ServiceTier
			}
			posts++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"selected": "", "reasoningEffort": "ultra", "serviceTier": tier})
	}))
	defer srv.Close()
	host, port := splitRouteHost(t, srv.URL)
	p := &acpProvider{}
	sess := &reactSession{sb: &sandbox.Sandbox{Host: host, Port: port, Password: "test"}}
	for i, want := range []string{"fast", "fast", "", ""} {
		if err := p.alignChatSettings(context.Background(), sess, "", "ultra", want); err != nil {
			t.Fatal(err)
		}
		if tier != want || posts != i/2+1 {
			t.Fatalf("tier=%q posts=%d after request %d", tier, posts, i)
		}
	}
	if chatSettingsMatch(sandbox.ChatModelSettings{}, "", "", "fast") {
		t.Fatal("an older runtime cannot acknowledge a service tier")
	}
}

func TestConfiguredServiceTiersFollowTheModelRoute(t *testing.T) {
	official := modelrouting.Connection{
		BaseURL: "https://api.openai.com/v1", State: "active", Protocol: modelrouting.ProtocolResponses, Auth: modelrouting.Auth{Scheme: "bearer"},
		Models: []modelrouting.Model{{ID: "gpt-6.2-sol"}, {ID: "gpt-6.2-astra"}, {ID: "unknown"}},
	}
	gateway := official
	gateway.BaseURL = "https://compatible.example.test/v1"
	for _, tc := range []struct {
		connection *modelrouting.Connection
		want       []string
	}{
		{nil, []string{"fast"}},
		{&official, []string{"fast"}},
		{&gateway, nil},
	} {
		raw, err := configuredModelsJSON(modelrouting.Resolved{
			Backend: "codex", Connection: tc.connection,
			Binding: modelrouting.Binding{ModelID: "gpt-6.2-sol", AuxiliaryModelIDs: []string{"gpt-6.2-astra", "unknown"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			ServiceTiers []string `json:"serviceTiers"`
		}
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 || !slices.Equal(rows[0].ServiceTiers, tc.want) || !slices.Equal(rows[1].ServiceTiers, tc.want) || rows[2].ServiceTiers != nil {
			t.Fatalf("service tiers crossed their declared source/model boundary: %s", raw)
		}
	}
}

func TestServiceTierEnvRoundTrip(t *testing.T) {
	for tier, env := range map[string]string{"": "default", "fast": "fast", " turbo ": "turbo"} {
		if got := ServiceTierEnv(tier); got != env {
			t.Errorf("ServiceTierEnv(%q) = %q, want %q", tier, got, env)
		}
	}
	for env, tier := range map[string]string{"": "", "default": "", "fast": "fast"} {
		if got := ParseServiceTierEnv(env); got != tier {
			t.Errorf("ParseServiceTierEnv(%q) = %q, want %q", env, got, tier)
		}
	}
}
