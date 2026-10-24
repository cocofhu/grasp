package handlers_test

import (
	"encoding/json"
	"testing"
)

func TestGitBindingsAgentHTTPContract(t *testing.T) {
	h := newHarness(t)
	selection := map[string]any{"items": []any{map[string]any{"credentialId": "gh", "scope": "https://Git.Example.com/"}}, "preferredCredentialId": "gh"}
	w := h.do("POST", "/api/agents", map[string]any{"name": "git-http", "acpBackend": "cursor", "gitBindings": selection})
	if w.Code != 201 {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["gitBindings"].(map[string]any)["preferredCredentialId"] != "gh" {
		t.Fatal("preferred authorization was lost")
	}
	if _, exists := got["repoIds"]; exists {
		t.Fatal("Agent response exposed an obsolete repository authorization source")
	}
	w = h.do("PUT", "/api/agents/git-http", map[string]any{"acpBackend": "cursor", "gitBindings": selection})
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	agent, _ := h.h.Agents.Get("git-http")
	if agent.GitBindings == nil || agent.GitBindings.Items[0].Scope != "https://git.example.com" {
		t.Fatal("Agent selection was not normalized and saved")
	}
	w = h.do("POST", "/api/agents", map[string]any{"name": "bad-git-http", "acpBackend": "cursor", "gitBindings": map[string]any{"items": []any{map[string]any{"credentialId": "gh"}}}})
	if w.Code != 400 || h.h.Agents.Exists("bad-git-http") {
		t.Fatalf("invalid preferred selection saved: %d %s", w.Code, w.Body.String())
	}
	w = h.do("PUT", "/api/agents/git-http", map[string]any{"acpBackend": "cursor", "gitBindings": map[string]any{"items": []any{}}})
	if w.Code != 200 {
		t.Fatalf("clear = %d %s", w.Code, w.Body.String())
	}
	agent, _ = h.h.Agents.Get("git-http")
	if agent.GitBindings == nil || len(agent.GitBindings.Items) != 0 {
		t.Fatal("explicit empty selection did not clear Agent Git authorizations")
	}
}

func TestSharedConfigurationHasNoModelOrGitSource(t *testing.T) {
	h := newHarness(t)
	w := h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"env": map[string]string{"FEATURE": "keep", "ACP_BRIDGE_MODEL": "ignored"}})
	if w.Code != 200 {
		t.Fatalf("put = %d %s", w.Code, w.Body.String())
	}
	w = h.do("GET", "/api/workspace/shared-agent-config", nil)
	if w.Code != 200 {
		t.Fatalf("get = %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"gitBindings", "repoIds", "acpBackend", "aiCredentialId", "openCodeCredentialId"} {
		if _, exists := got[key]; exists {
			t.Fatalf("shared config still exposed %s", key)
		}
	}
	env := got["env"].(map[string]any)
	if env["FEATURE"] != "keep" || env["ACP_BRIDGE_MODEL"] != nil {
		t.Fatal("shared config did not restrict environment to non-model settings")
	}
}
