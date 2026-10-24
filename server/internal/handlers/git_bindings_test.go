package handlers_test

import (
	"encoding/json"
	"testing"
)

func TestGitBindingsAgentAndSharedHTTPContract(t *testing.T) {
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
	if got["gitBindings"].(map[string]any)["preferredCredentialId"] != "gh" || len(got["repoIds"].([]any)) != 0 {
		t.Fatalf("create selection/repo default = %+v", got)
	}
	w = h.do("PUT", "/api/agents/git-http", map[string]any{"acpBackend": "cursor", "gitBindings": selection, "repoIds": []string{"explicit-repo"}})
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	if agent, _ := h.h.Agents.Get("git-http"); agent.RepoIDs == nil || len(*agent.RepoIDs) != 1 {
		t.Fatal("update erased explicit repository grant")
	}
	w = h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"acpBackend": "cursor", "gitBindings": selection})
	if w.Code != 200 {
		t.Fatalf("shared put = %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["gitBindings"].(map[string]any)["items"].([]any)[0].(map[string]any)["scope"] != "https://git.example.com" {
		t.Fatal("shared HTTP body ignored binding")
	}
	w = h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"acpBackend": "cursor", "env": map[string]string{"FEATURE": "keep"}})
	if w.Code != 200 || h.h.SharedAgent.Get().GitBindings == nil {
		t.Fatalf("old client cleared new shared selection: %d %s", w.Code, w.Body.String())
	}
	w = h.do("GET", "/api/workspace/shared-agent-config", nil)
	if w.Code != 200 {
		t.Fatalf("get = %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["gitBindings"].(map[string]any)["preferredCredentialId"] != "gh" {
		t.Fatal("GET omitted persisted shared selection")
	}
	w = h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"gitBindings": map[string]any{"items": []any{map[string]any{"credentialId": "gh"}}}})
	if w.Code != 400 {
		t.Fatalf("bad preferred should be 400: %d %s", w.Code, w.Body.String())
	}
	w = h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"gitBindings": map[string]any{"items": []any{}}})
	if w.Code != 200 || h.h.SharedAgent.Get().GitBindings == nil || len(h.h.SharedAgent.Get().GitBindings.Items) != 0 {
		t.Fatal("explicit shared empty did not replace selection")
	}
	w = h.do("POST", "/api/agents", map[string]any{"name": "bad-git-http", "acpBackend": "cursor", "gitBindings": map[string]any{"items": []any{map[string]any{"credentialId": "gh"}}}})
	if w.Code != 400 || h.h.Agents.Exists("bad-git-http") {
		t.Fatalf("bad selection saved: %d %s", w.Code, w.Body.String())
	}
}
