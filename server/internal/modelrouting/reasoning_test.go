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
		{"opencode", "kimi-k3.1-pro", &Connection{ProviderID: "tokenhub"}, []string{"low", "high", "max"}},
		{"opencode", "kimi-k3.1-pro", &Connection{ProviderID: "moonshot", BaseURL: "https://api.moonshot.cn/v1"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-v4.1-flash", &Connection{ProviderID: "tokenhub"}, []string{"low", "high", "max"}},
		{"opencode", "DeepSeek-V4.1-Flash-1017", &Connection{ProviderID: "tokenhub"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek/deepseek-flash", &Connection{BaseURL: "https://tokenhub-intl.tencentcloudmaas.com/v1"}, []string{"low", "high", "max"}},
		{"opencode", "vendor/deepseek-v4.1-flash", &Connection{ProviderID: "hunyuan"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-flash", &Connection{ProviderID: "deepseek", BaseURL: "https://api.deepseek.com"}, []string{"low", "high", "max"}},
		{"opencode", "glm-5.3", &Connection{ProviderID: "zhipu", BaseURL: "https://open.bigmodel.cn/api/paas/v4"}, []string{"low", "high", "max"}},
		{"opencode", "qwen3.8-flash", &Connection{ProviderID: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"}, []string{"low", "medium", "xhigh"}},
		{"opencode", "qwen3.8-omni-flash", &Connection{BaseURL: "https://ap-southeast-1.maas.qwencloudapi.com/v1"}, []string{"low", "medium", "xhigh"}},
		{"opencode", "kimi/kimi-k3", &Connection{ProviderID: "qwen"}, []string{"max"}},
		{"opencode", "kimi-k3", &Connection{ProviderID: "qwen"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-v4-pro-0813", &Connection{ProviderID: "qwen"}, []string{"low", "high", "max"}},
		{"opencode", "gemini-3.1-flash-lite", &Connection{ProviderID: "google"}, []string{"minimal", "low", "medium", "high"}},
		{"opencode", "gemini-3.1-flash-lite-image", &Connection{ProviderID: "google"}, []string{"minimal", "high"}},
		{"opencode", "gemini-robotics-er-2-preview", &Connection{ProviderID: "google"}, []string{"minimal", "low", "medium", "high"}},
		{"opencode", "deepseek-v4-pro-260425", &Connection{ProviderID: "doubao"}, []string{"minimal", "low", "high", "max"}},
		{"opencode", "MiniMax-M3.1-Flash", &Connection{ProviderID: "minimax", BaseURL: "https://api.minimax.cn/v1"}, []string{"low", "medium", "high", "xhigh", "max"}},
		{"opencode", "MiniMax-M3", &Connection{ProviderID: "minimax"}, nil},
		{"opencode", "glm-5.3", &Connection{ProviderID: "openrouter", BaseURL: "https://openrouter.ai/api/v1"}, nil},
		{"opencode", "claude-opus-4-5", &Connection{ProviderID: "anthropic", BaseURL: "https://api.anthropic.com"}, nil},
		{"opencode", "deepseek-v4-flash", &Connection{ProviderID: "tokenhub", BaseURL: "https://tokenhub.tencentmaas.com/v1"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-v4-pro", &Connection{BaseURL: "https://tokenhub.tencentmaas.com/v1"}, []string{"low", "high", "max"}},
		{"opencode", "deepseek-v4-pro", &Connection{BaseURL: "https://tokenhub.tencentmaas.com.evil.example/v1"}, nil},
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

func TestKimiCodeReasoningEffortsUseExactCLICatalog(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want []string
	}{
		{"k3", []string{"low", "high", "max"}},
		{"k3.1-pro", []string{"low", "high", "max"}},
		{"k3.1-pro-highspeed", []string{"low", "high", "max"}},
		{"k3-256k", []string{"low", "high", "max"}},
		{"kimi-for-coding", []string{"low", "high", "max"}},
		{"kimi-k3", []string{"low", "high", "max"}},
		{"kimi-k3.1-pro", []string{"low", "high", "max"}},
		{"kimi-k3.1-pro-highspeed", []string{"low", "high", "max"}},
		{" kimi-k3 ", []string{"low", "high", "max"}},
		{"kimi-for-coding-highspeed", nil},
		{"kimi-k2.6", nil},
		{"kimi-k2.7-code", nil},
		{"kimi-k2.7-code-highspeed", nil},
		{"kimi-k3-unknown", nil},
		{"k3-unknown", nil},
		{"KIMI-K3", nil},
		{"", nil},
		{"auto", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			no := false
			metadata := &Connection{Models: []Model{{
				ID:               tc.id,
				Capabilities:     ModelCapabilities{Reasoning: &no},
				ReasoningEfforts: []string{"medium", "ultra"},
			}}}
			for _, connection := range []*Connection{nil, metadata} {
				if got := ReasoningEfforts("kimi_code", tc.id, connection); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("got %v, want %v (connection metadata: %t)", got, tc.want, connection != nil)
				}
			}
		})
	}
}
