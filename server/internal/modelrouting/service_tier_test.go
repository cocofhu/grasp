package modelrouting

import (
	"slices"
	"testing"
)

func TestServiceTiersUseExactOfficialModelCapabilities(t *testing.T) {
	fast := []string{"fast"}
	for _, tc := range []struct {
		backend, model string
		connection     *Connection
		want           []string
	}{
		{"codex", "gpt-6.2-sol", nil, fast},
		{"codex", "gpt-6.2-astra", nil, fast},
		{"codex", " gpt-6.1-terra ", nil, fast},
		{"codex", "gpt-6-astra", nil, fast},
		{"codex", "gpt-6.2-sol", &Connection{}, nil},
		{"opencode", "gpt-6.2-sol", nil, nil},
		{"cursor", "gpt-6.2-sol", nil, nil},
		{"codex", "gpt-6.2-sol-fast", nil, nil},
		{"codex", "gpt-future", nil, nil},
		{"codex", "auto", nil, nil},
		{"codex", "", nil, nil},
	} {
		if got := ServiceTiers(tc.backend, tc.model, tc.connection); !slices.Equal(got, tc.want) {
			t.Errorf("%s/%s connection=%t: got %v, want %v", tc.backend, tc.model, tc.connection != nil, got, tc.want)
		}
	}
}

func TestServiceTiersForOpenAIResponses(t *testing.T) {
	official := Connection{
		BaseURL: "https://api.openai.com/v1", State: "active", Protocol: ProtocolResponses, Auth: Auth{Scheme: "bearer"},
		Models: []Model{
			{ID: "gpt-6.2-sol"}, {ID: "gpt-6.2-astra"}, {ID: "gpt-5.6-terra"},
			{ID: "gpt-5.6-luna"}, {ID: "gpt-5.5"}, {ID: "gpt-5.4"}, {ID: "gpt-5.4-mini"},
			{ID: "gpt-5.4-nano"}, {ID: "gpt-5-codex"}, {ID: "gpt-4o-2024-11-20"}, {ID: "o4-mini"}, {ID: "custom"},
		},
	}
	for _, tc := range []struct {
		name, backend, model string
		change               func(*Connection)
		wantFast             bool
	}{
		{name: "official", backend: "codex", model: "gpt-6.2-sol", wantFast: true},
		{name: "GPT-5.6 Terra", backend: "codex", model: "gpt-5.6-terra", wantFast: true},
		{name: "GPT-5.6 Luna", backend: "codex", model: "gpt-5.6-luna", wantFast: true},
		{name: "GPT-5.5", backend: "codex", model: "gpt-5.5", wantFast: true},
		{name: "GPT-5.4", backend: "codex", model: "gpt-5.4", wantFast: true},
		{name: "GPT-5.4 mini", backend: "codex", model: "gpt-5.4-mini", wantFast: true},
		{name: "GPT-5.4 nano", backend: "codex", model: "gpt-5.4-nano"},
		{name: "GPT-5 Codex", backend: "codex", model: "gpt-5-codex", wantFast: true},
		{name: "listed snapshot", backend: "codex", model: "gpt-4o-2024-11-20", wantFast: true},
		{name: "o4-mini", backend: "codex", model: "o4-mini", wantFast: true},
		{name: "standard HTTPS port", backend: "codex", model: "gpt-6.2-astra", wantFast: true, change: func(c *Connection) { c.BaseURL = "https://API.OPENAI.COM:443/v1/" }},
		{name: "compatible gateway", backend: "codex", model: "gpt-6.2-sol", change: func(c *Connection) { c.BaseURL = "https://api.openai.com.example.test/v1" }},
		{name: "alternate port", backend: "codex", model: "gpt-6.2-sol", change: func(c *Connection) { c.BaseURL = "https://api.openai.com:8443/v1" }},
		{name: "alternate route", backend: "codex", model: "gpt-6.2-sol", change: func(c *Connection) { c.BaseURL = "https://api.openai.com/other/v1" }},
		{name: "non-HTTPS", backend: "codex", model: "gpt-6.2-sol", change: func(c *Connection) { c.BaseURL = "http://api.openai.com/v1" }},
		{name: "chat completions", backend: "codex", model: "gpt-6.2-sol", change: func(c *Connection) { c.Protocol = ProtocolChat }},
		{name: "disabled connection", backend: "codex", model: "gpt-6.2-sol", change: func(c *Connection) { c.State = "disabled" }},
		{name: "other tool", backend: "opencode", model: "gpt-6.2-sol"},
		{name: "uncatalogued", backend: "codex", model: "gpt-6.1-sol"},
		{name: "unsupported", backend: "codex", model: "custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connection := official
			if tc.change != nil {
				tc.change(&connection)
			}
			want := []string(nil)
			if tc.wantFast {
				want = []string{"fast"}
			}
			if got := ServiceTiers(tc.backend, tc.model, &connection); !slices.Equal(got, want) {
				t.Fatalf("service tiers = %v, want %v", got, want)
			}
		})
	}
}
