package modelrouting

import (
	"reflect"
	"testing"
)

func TestReasoningEffortsRespectAdapterAndExactMetadata(t *testing.T) {
	no := false
	connection := &Connection{Models: []Model{
		{ID: "kimi", ReasoningEfforts: []string{" quiet ", "deep", "deep", ""}},
		{ID: "gpt-6.1-sol"},
		{ID: "gpt-6-luna", Capabilities: ModelCapabilities{Reasoning: &no}, ReasoningEfforts: []string{"high"}},
	}}
	for _, tc := range []struct {
		backend, id string
		connection  *Connection
		want        []string
	}{
		{"deepseek_harness", "kimi", connection, []string{"quiet", "deep"}},
		{"codex", "kimi", connection, []string{"quiet", "deep"}},
		{"codex", "gpt-6.1-sol", connection, []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
		{"codex", "gpt-6-luna", nil, []string{"low", "medium", "high", "xhigh", "max"}},
		{"codex", "gpt-6-luna", connection, nil},
		{"codex", "gpt-6-astra", connection, nil},
		{"deepseek_harness", "gpt-6.1-sol", connection, nil},
		{"cursor", "kimi", connection, nil},
		{"opencode", "kimi", connection, nil},
		{"opencode", "kimi", &Connection{ProviderID: "tokenhub", Models: connection.Models}, nil},
		{"opencode", "deepseek-v4.1-flash", &Connection{ProviderID: "tokenhub"}, []string{"low", "high", "max"}},
		{"opencode", "vendor/deepseek-v4.1-flash", &Connection{ProviderID: "hunyuan"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-v4-flash", &Connection{ProviderID: "tokenhub", BaseURL: "https://tokenhub.tencentmaas.com/v1"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-v4-pro", &Connection{ProviderID: "tokenhub"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-v4-pro", nil, []string{"high", "max"}},
		{"opencode", "opencode/gpt-5.5", nil, []string{"none", "low", "medium", "high", "xhigh"}},
		{"opencode", "opencode/gpt-5.6-sol", nil, []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{"opencode", "opencode/gpt-5.5-pro", nil, []string{"medium", "high", "xhigh"}},
		{"opencode", "opencode/claude-haiku-4-5", nil, []string{"high", "max"}},
		{"opencode", "opencode/claude-haiku-5-5", nil, []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{"opencode", "opencode/qwen3.5-plus", nil, []string{"none", "high", "max"}},
		{"opencode", "opencode/qwen3.8-max", nil, []string{"low", "medium", "xhigh"}},
		{"opencode", "opencode/minimax-m3-free", nil, []string{"none", "thinking"}},
		{"opencode", "opencode/minimax-m3", nil, nil},
		{"opencode", "opencode/glm-4.6", nil, nil},
		{"opencode", "opencode/kimi-k2.5", nil, nil},
		{"opencode", "opencode/kimi-k2.7-code", nil, nil},
		{"opencode", "deepseek-v4.1-flash", connection, nil},
		{"opencode", "opencode/gpt-5.5", connection, nil},
		{"cursor", "opencode/gpt-5.5", nil, nil},
		{"codex", "gpt-6.1-sol-ultra", nil, nil},
		{"codex", "", nil, nil},
		{"codex", "auto", nil, nil},
	} {
		if got := ReasoningEfforts(tc.backend, tc.id, tc.connection); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s/%q: got %v, want %v", tc.backend, tc.id, got, tc.want)
		}
	}
}
