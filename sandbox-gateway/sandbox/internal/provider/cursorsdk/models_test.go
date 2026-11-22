package cursorsdk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// testdata/models.json is a recorded slice of Cursor.models.list().
func loadCatalog(t *testing.T) []sdkModel {
	t.Helper()
	raw, err := os.ReadFile("testdata/models.json")
	if err != nil {
		t.Fatal(err)
	}
	var models []sdkModel
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatal(err)
	}
	return models
}

func TestResolveModel(t *testing.T) {
	catalog := loadCatalog(t)
	for _, tc := range []struct {
		model, effort, tier string
		want                string
		max                 bool
	}{
		{"", "", "", "default", false},
		{"auto", "", "", "default", false},
		// Open parameters take the default variant; fast stays off.
		{"claude-opus-5-5", "", "", "claude-opus-5-5 cyber=false context=1m effort=medium fast=false", true},
		{"claude-opus-5-5", "max", "", "claude-opus-5-5 cyber=false context=1m effort=max fast=false", true},
		{"composer-2.5", "", "", "composer-2.5 fast=false", false},
		{"composer-2.5", "", "fast", "composer-2.5 fast=true", false},
		// The platform's xhigh is extra-high on older GPT models.
		{"gpt-5.5", "xhigh", "", "gpt-5.5 context=1m reasoning=extra-high fast=false", true},
		// Fast only exists with the smaller context, so that variant wins.
		{"gpt-5.5", "high", "fast", "gpt-5.5 context=272k reasoning=high fast=true", false},
		{"grok-4.7", "low", "", "grok-4.7 context=500k reasoning_effort=low fast=false", true},
		{"claude-opus-4-5", "", "", "claude-opus-4-5 thinking=true", false},
		{"gemini-3.1-pro", "", "", "gemini-3.1-pro", false},
		// An alias resolves to its model.
		{"composer", "", "", "composer-2.5 fast=false", false},
	} {
		got, err := resolveModel(catalog, tc.model, tc.effort, tc.tier)
		if err != nil {
			t.Errorf("%s/%s/%s: %v", tc.model, tc.effort, tc.tier, err)
			continue
		}
		if got.label() != tc.want || got.MaxMode != tc.max {
			t.Errorf("%s/%s/%s = %q max=%v, want %q max=%v", tc.model, tc.effort, tc.tier, got.label(), got.MaxMode, tc.want, tc.max)
		}
	}
}

func TestResolveModelRejectsWhatCursorDoesNotOffer(t *testing.T) {
	catalog := loadCatalog(t)
	for _, tc := range []struct{ model, effort, tier, want string }{
		{"auto", "high", "", "自动选择"},
		{"gpt-4", "", "", "不提供模型"},
		{"claude-opus-4-5", "high", "", "不支持设置思考强度"},
		{"gpt-5.5", "max", "", "不支持思考强度"},
		{"claude-opus-4-5", "", "fast", "没有 fast 档位"},
		{"gpt-5.5", "", "flex", "不支持速度档位"},
		{"gpt-5.5", "none", "fast", ""},
	} {
		_, err := resolveModel(catalog, tc.model, tc.effort, tc.tier)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s/%s/%s: %v", tc.model, tc.effort, tc.tier, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s/%s/%s: err = %v, want %q", tc.model, tc.effort, tc.tier, err, tc.want)
		}
	}
}

func TestSDKMCPServers(t *testing.T) {
	raw := json.RawMessage(`[
		{"name":"store","type":"http","url":"http://127.0.0.1:9/mcp","headers":[{"name":"Authorization","value":"Bearer x"}]},
		{"name":"events","type":"sse","url":"http://127.0.0.1:9/sse"},
		{"name":"fs","command":"mcp-fs","args":["--root","/w"],"env":[{"name":"A","value":"1"}]},
		{"name":"","command":"ignored"}
	]`)
	got, err := sdkMCPServers(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `{"events":{"type":"sse","url":"http://127.0.0.1:9/sse"},` +
		`"fs":{"args":["--root","/w"],"command":"mcp-fs","env":{"A":"1"},"type":"stdio"},` +
		`"store":{"headers":{"Authorization":"Bearer x"},"type":"http","url":"http://127.0.0.1:9/mcp"}}`
	if string(b) != want {
		t.Fatalf("got %s\nwant %s", b, want)
	}
	if got, err := sdkMCPServers(nil); err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
}
