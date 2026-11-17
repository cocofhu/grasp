package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestAlignFastModeIndependentlyAndResetAtDefaultRoute(t *testing.T) {
	fastMode := false
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
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
			if body.Model != "" || body.ReasoningEffort != "ultra" || body.FastMode == nil {
				t.Errorf("speed changed model/effort or omitted explicit off: %+v", body)
			}
			if body.FastMode != nil {
				fastMode = *body.FastMode
			}
			posts++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"selected": "", "reasoningEffort": "ultra", "fastMode": fastMode})
	}))
	defer srv.Close()
	host, port := splitRouteHost(t, srv.URL)
	p := &acpProvider{}
	sess := &reactSession{sb: &sandbox.Sandbox{Host: host, Port: port, Password: "test"}}
	for i, want := range []bool{true, true, false, false} {
		if err := p.alignChatSettings(context.Background(), sess, "", "ultra", want); err != nil {
			t.Fatal(err)
		}
		if fastMode != want || posts != i/2+1 {
			t.Fatalf("fast=%v posts=%d after request %d", fastMode, posts, i)
		}
	}
	if chatSettingsMatch(sandbox.ChatModelSettings{}, "", "", true) {
		t.Fatal("an older runtime cannot acknowledge Fast mode")
	}
}

func TestConfiguredFastCapabilityDoesNotLeakToCompatibleConnections(t *testing.T) {
	for _, connection := range []*modelrouting.Connection{nil, {ProviderID: "openai"}} {
		raw, err := configuredModelsJSON(modelrouting.Resolved{
			Backend: "codex", Connection: connection,
			Binding: modelrouting.Binding{ModelID: "gpt-6.2-sol", AuxiliaryModelIDs: []string{"gpt-6.2-astra", "unknown"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			FastModeSupported bool `json:"fastModeSupported"`
		}
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 || rows[0].FastModeSupported != (connection == nil) || rows[1].FastModeSupported != (connection == nil) || rows[2].FastModeSupported {
			t.Fatalf("Fast capability crossed its declared source/model boundary: %s", raw)
		}
	}
}
