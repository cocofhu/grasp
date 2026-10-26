package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListCliBackendModels(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)
	hn.cookie = hn.login(t)
	w := hn.do(http.MethodGet, "/api/acp-backends/claude_code/models", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Models []struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Vendor      string   `json:"vendor"`
			Tags        []string `json:"tags"`
			Context     int      `json:"context"`
			Recommended bool     `json:"recommended"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Models) == 0 {
		t.Fatal("empty list")
	}
	first := got.Models[0]
	if first.ID == "" || first.Vendor != "anthropic" || first.Context == 0 || len(first.Tags) == 0 {
		t.Fatalf("fields lost: %+v", first)
	}
}

// An unconfigured OpenCode catalog and unknown tools keep the picker usable.
func TestListCliBackendModelsUnlisted(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)
	hn.cookie = hn.login(t)
	for _, b := range []string{"opencode", "nope"} {
		w := hn.do(http.MethodGet, "/api/acp-backends/"+b+"/models", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", b, w.Code)
		}
		if body := w.Body.String(); body != `{"models":[]}` {
			t.Fatalf("%s: body = %s", b, body)
		}
	}
}

func TestOpenCodePlatformModelsOnlyIncludeZenProvider(t *testing.T) {
	hn := catalogHarness(t, `{
 "opencode":{"models":{"kimi-k3":{"name":"Kimi K3"},"vendor/model":{"name":"Invalid namespace"}}},
 "anthropic":{"models":{"claude-sonnet-5":{"name":"Claude Sonnet 5"}}},
 "opencode-go":{"models":{"kimi-k3":{"name":"Go only"}}}
}`, http.StatusOK)
	w := hn.do(http.MethodGet, "/api/acp-backends/opencode/models", nil)
	var got struct {
		Models []struct{ ID, Name, Vendor string } `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(got.Models) != 1 || got.Models[0].ID != "opencode/kimi-k3" || got.Models[0].Name != "Kimi K3" || got.Models[0].Vendor != "opencode" {
		t.Fatalf("non-Zen model offered for official authorization: %s", w.Body.String())
	}
}

func TestOpenCodePlatformModelsCatalogFailure(t *testing.T) {
	hn := catalogHarness(t, "", http.StatusBadGateway)
	w := hn.do(http.MethodGet, "/api/acp-backends/opencode/models", nil)
	var got struct {
		Models []any  `json:"models"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || got.Models == nil || len(got.Models) != 0 || got.Error == "" {
		t.Fatalf("catalog failure removed model default fallback: %s", w.Body.String())
	}
}
