package models

import (
	"encoding/json"
	"math"
	"testing"
)

func TestTokenUsageRejectsInvalidMeasurementsAndKeepsRuntimeFlagPrivate(t *testing.T) {
	for _, usage := range []TokenUsage{
		{InputTokens: -1}, {OutputTokens: -1}, {CacheReadTokens: -1}, {CacheWriteTokens: -1},
		{InputTokens: math.MaxInt64, OutputTokens: 1},
	} {
		if usage.Valid() {
			t.Fatalf("invalid usage accepted: %+v", usage)
		}
	}
	for _, usage := range []TokenUsage{{}, {InputTokens: 10, OutputTokens: 3, CacheReadTokens: 2, CacheWriteTokens: 1}} {
		if !usage.Valid() {
			t.Fatalf("valid usage rejected: %+v", usage)
		}
	}
	u := TokenUsage{InputTokens: 1, LedgerRecorded: true}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var saved TokenUsage
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.InputTokens != 1 || saved.LedgerRecorded {
		t.Fatalf("runtime flag leaked into persisted/API usage: %s", raw)
	}
	copy := AddTokenUsage(nil, &u)
	if !copy.LedgerRecorded {
		t.Fatal("copy lost accounting ownership")
	}
	AddTokenUsage(copy, &TokenUsage{OutputTokens: 2})
	if copy.LedgerRecorded {
		t.Fatal("unrecorded additional consumption was hidden")
	}
}
