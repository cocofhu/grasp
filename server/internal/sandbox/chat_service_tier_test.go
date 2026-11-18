package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndependentServiceTierPreservesModelEffortAndChatScope(t *testing.T) {
	tier := ""
	posts := 0
	legacy := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Query().Get("chat") != "preview/independent" {
			t.Errorf("service tier touched a different chat: %s", r.URL)
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
			if body.Model != "gpt-6.2-sol" || body.ReasoningEffort != "ultra" || body.ServiceTier == nil {
				t.Errorf("independent tier update lost its explicit settings: %+v", body)
			}
			if body.ServiceTier != nil {
				tier = *body.ServiceTier
			}
			posts++
		}
		body := map[string]any{
			"configured": true, "selected": "gpt-6.2-sol", "reasoningEffort": "ultra",
			"models": []map[string]any{{"id": "gpt-6.2-sol", "serviceTiers": []string{"fast"}}},
		}
		if !legacy {
			body["serviceTier"] = tier
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	host, port := splitTestHost(t, srv.URL)
	client := NewACPClient(host, port).WithPassword("test").WithChat("preview/independent")
	for i, want := range []string{"fast", "fast", ""} {
		changed, err := client.SetIndependentModelSettings(context.Background(), "gpt-6.2-sol", "ultra", &want)
		if err != nil || changed != (i != 1) || tier != want {
			t.Fatalf("request %d: changed=%v tier=%q err=%v", i, changed, tier, err)
		}
	}
	if posts != 2 {
		t.Fatalf("tier toggles caused %d restarts, want 2", posts)
	}
	if changed, err := client.SetIndependentModelSettings(context.Background(), "gpt-6.2-sol", "ultra", nil); err != nil || changed {
		t.Fatalf("omitted tier restarted the chat: changed=%v err=%v", changed, err)
	}
	legacy = true
	fast := "fast"
	if _, err := client.SetIndependentModelSettings(context.Background(), "gpt-6.2-sol", "ultra", &fast); err == nil || posts != 2 {
		t.Fatal("older runtime silently accepted a service tier")
	}
}
