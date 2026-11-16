package models

import (
	"strings"
	"testing"
)

func TestParseTrigger(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"manual", TriggerManual, false},
		{"api", TriggerAPI, false},
		{"pm_mcp", TriggerPMMCP, false},
		{"Manual", "", true},
		{"API", "", true},
		{"手动触发", "", true},
		{"API 触发", "", true},
		{"PM MCP", "", true},
		{"webhook", "", true},
		{"custom:nightly-bug", "", true},
		{" manual ", TriggerManual, false},
	}
	for _, tc := range cases {
		got, err := ParseTrigger(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseTrigger(%q): want error", tc.in)
				continue
			}
			if !strings.Contains(err.Error(), "manual|api|pm_mcp") {
				t.Errorf("ParseTrigger(%q): error %q missing allow-list", tc.in, err.Error())
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTrigger(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseTrigger(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveTrigger(t *testing.T) {
	got, err := ResolveTrigger("", TriggerManual)
	if err != nil || got != TriggerManual {
		t.Fatalf("empty → default: got %q err %v", got, err)
	}
	got, err = ResolveTrigger("api", TriggerManual)
	if err != nil || got != TriggerAPI {
		t.Fatalf("explicit wins: got %q err %v", got, err)
	}
	if _, err := ResolveTrigger("webhook", TriggerAPI); err == nil {
		t.Fatal("illegal must error")
	}
}

func TestValidTrigger(t *testing.T) {
	if !validTrigger(TriggerManual) || !validTrigger(TriggerAPI) || !validTrigger(TriggerPMMCP) {
		t.Fatal("whitelist codes should be valid")
	}
	if validTrigger("手动触发") || validTrigger("Manual") || validTrigger("") {
		t.Fatal("non-whitelist values should be invalid")
	}
}

func TestAllowedTriggers(t *testing.T) {
	if len(AllowedTriggers) != 3 {
		t.Fatalf("len=%d want 3", len(AllowedTriggers))
	}
	for _, code := range AllowedTriggers {
		if !validTrigger(code) {
			t.Fatalf("%q in AllowedTriggers but not ValidTrigger", code)
		}
	}
}
