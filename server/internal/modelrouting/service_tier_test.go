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
