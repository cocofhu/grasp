package kimicode

import (
	"strings"
	"testing"
)

func TestExplainClassifiesFailuresWithoutTheKey(t *testing.T) {
	secret := "sk-live-secret-value"
	cases := []struct {
		in, want string
	}{
		{"KIMI_CODE_API_KEY is unset", MsgMissingKey},
		{"api_key_env KIMI_CODE_API_KEY missing", MsgMissingKey},
		{"401 Unauthorized: invalid api key " + secret, MsgSiteMismatch},
		{`{"error":{"type":"invalid_api_key"}}`, MsgSiteMismatch},
		{"404 resource_not_found_error model " + secret, MsgModelUnavailable},
		{"model does not exist", MsgModelUnavailable},
		{"模型不存在", MsgModelUnavailable},
	}
	for _, tc := range cases {
		got := Explain(tc.in, secret)
		if got != tc.want || strings.Contains(got, secret) {
			t.Fatalf("Explain(%q)=%q", tc.in, got)
		}
	}
	other := Explain("workspace is busy "+secret, secret)
	if strings.Contains(other, secret) || !strings.Contains(other, "••••") {
		t.Fatal(other)
	}
}
