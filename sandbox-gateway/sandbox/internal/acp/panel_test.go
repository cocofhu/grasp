package acp

import (
	"encoding/json"
	"testing"
)

func TestConfigOptionsFlatStandardGroupedAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"flat", `[{"id":"model","currentValue":"pro","options":[{"value":"flash","name":"Flash"},{"value":"pro","name":"Pro"}]}]`},
		{"standard groups", `[{"id":"model","currentValue":"pro","options":[{"group":"deepseek","name":"DeepSeek","options":[{"value":"flash","name":"Flash"},{"value":"pro","name":"Pro"}]}]}]`},
		{"legacy groups", `[{"id":"model","currentValue":"pro","groups":[{"id":"deepseek","options":[{"value":"flash","name":"Flash"},{"value":"pro","name":"Pro"}]}]}]`},
		{"mixed", `[{"id":"model","currentValue":"pro","options":[{"value":"flash","name":"Flash"},{"group":"deepseek","options":[{"value":"pro","name":"Pro"}]}]}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Panel{}
			p.parseAndSyncConfigOptions(json.RawMessage(tc.raw))
			if p.ModelID != "pro" || p.ModelName != "Pro" || len(p.ConfigOptions) != 1 || countOptions(p.ConfigOptions[0]) != 2 {
				t.Fatalf("unexpected parsed state: %+v", p)
			}
		})
	}
}

func TestConfigOptionsInvalidPreservesExistingState(t *testing.T) {
	p := &Panel{ModelID: "old", ModelName: "Old"}
	for _, raw := range []string{"", "null", "{", `[{"id":"model","options":[{"options":"invalid"}]}]`} {
		p.parseAndSyncConfigOptions(json.RawMessage(raw))
		if p.ModelID != "old" || p.ModelName != "Old" {
			t.Fatalf("invalid config cleared model: %+v", p)
		}
	}
	p.parseAndSyncConfigOptions(json.RawMessage(`[{"id":"model","currentValue":"unknown","options":[]}]`))
	if p.ModelName != "unknown" {
		t.Fatalf("unknown value should preserve ID as display name: %+v", p)
	}
}
