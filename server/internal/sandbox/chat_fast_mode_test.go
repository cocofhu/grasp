package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndependentFastModePreservesModelEffortAndChatScope(t *testing.T) {
	fastMode := false
	posts := 0
	legacy := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Query().Get("chat") != "preview/independent" {
			t.Errorf("Fast mode touched a different chat: %s", r.URL)
		}
		if r.URL.Path == "/api/model" {
			var body struct {
				Model           string `json:"model"`
				ReasoningEffort string `json:"reasoningEffort"`
				FastMode        *bool  `json:"fastMode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Model != "gpt-6.2-sol" || body.ReasoningEffort != "ultra" || body.FastMode == nil {
				t.Errorf("independent Fast update lost its explicit settings: %+v", body)
			}
			if body.FastMode != nil {
				fastMode = *body.FastMode
			}
			posts++
		}
		body := map[string]any{
			"configured": true, "selected": "gpt-6.2-sol", "reasoningEffort": "ultra",
			"models": []map[string]any{{"id": "gpt-6.2-sol", "fastModeSupported": true}},
		}
		if !legacy {
			body["fastMode"] = fastMode
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	host, port := splitTestHost(t, srv.URL)
	client := NewACPClient(host, port).WithPassword("test").WithChat("preview/independent")
	for i, want := range []bool{true, true, false} {
		changed, err := client.SetIndependentModelSettings(context.Background(), "gpt-6.2-sol", "ultra", want)
		if err != nil || changed != (i != 1) || fastMode != want {
			t.Fatalf("request %d: changed=%v fast=%v err=%v", i, changed, fastMode, err)
		}
	}
	if posts != 2 {
		t.Fatalf("Fast toggles caused %d restarts, want 2", posts)
	}
	legacy = true
	if _, err := client.SetIndependentModelSettings(context.Background(), "gpt-6.2-sol", "ultra", true); err == nil || posts != 2 {
		t.Fatal("older runtime silently accepted Fast mode")
	}
}
