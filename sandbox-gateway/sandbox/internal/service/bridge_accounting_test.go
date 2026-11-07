package service

import (
	"encoding/json"
	"testing"
	"time"

	"backend/internal/provider"
)

func TestCompletionAccountsForWatchdogContinuationAndPersistsIdentity(t *testing.T) {
	b := NewBridge()
	b.activeTurn = &promptTurn{opID: "original", carriedUsage: map[string]provider.TokenUsage{"m": {InputTokens: 5, CacheReadTokens: 2}}}
	raw := []byte(`{"op":"event","opId":"original","data":{"type":"prompt_done","usage":{"m":{"inputTokens":7,"outputTokens":3}}}}`)
	var frame struct {
		Data struct {
			OpID  string                         `json:"opId"`
			At    time.Time                      `json:"reportedAt"`
			Usage map[string]provider.TokenUsage `json:"usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b.stampUsageEvent(raw), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Data.OpID != "original" || frame.Data.At.IsZero() || frame.Data.Usage["m"].InputTokens != 12 || frame.Data.Usage["m"].CacheReadTokens != 2 {
		t.Fatalf("completion=%+v", frame)
	}
}
