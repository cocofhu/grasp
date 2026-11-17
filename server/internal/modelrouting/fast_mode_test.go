package modelrouting

import "testing"

func TestFastModeUsesExactOfficialModelCapabilities(t *testing.T) {
	for _, tc := range []struct {
		backend, model string
		connection     *Connection
		want           bool
	}{
		{"codex", "gpt-6.2-sol", nil, true},
		{"codex", "gpt-6.2-astra", nil, true},
		{"codex", " gpt-6.1-terra ", nil, true},
		{"codex", "gpt-6-astra", nil, true},
		{"codex", "gpt-6.2-sol", &Connection{}, false},
		{"opencode", "gpt-6.2-sol", nil, false},
		{"cursor", "gpt-6.2-sol", nil, false},
		{"codex", "gpt-6.2-sol-fast", nil, false},
		{"codex", "gpt-future", nil, false},
		{"codex", "auto", nil, false},
		{"codex", "", nil, false},
	} {
		if got := FastModeSupported(tc.backend, tc.model, tc.connection); got != tc.want {
			t.Errorf("%s/%s connection=%t: got %t, want %t", tc.backend, tc.model, tc.connection != nil, got, tc.want)
		}
	}
}
