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
		{"codex", "gpt-6.1-sol-ultra", nil, nil},
		{"codex", "", nil, nil},
		{"codex", "auto", nil, nil},
	} {
		if got := ReasoningEfforts(tc.backend, tc.id, tc.connection); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s/%q: got %v, want %v", tc.backend, tc.id, got, tc.want)
		}
	}
}
