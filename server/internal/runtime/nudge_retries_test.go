package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestNudgeRetries(t *testing.T) {
	caps := &models.AgentCapabilities{MaxRounds: 7}
	cases := []struct {
		name string
		cfg  map[string]any
		caps *models.AgentCapabilities
		plan bool
		want int
	}{
		{"default", nil, nil, false, defaultNudgeRetries},
		{"node value", map[string]any{"nudgeRetries": float64(5)}, nil, false, 5},
		{"zero disables", map[string]any{"nudgeRetries": 0}, nil, false, 0},
		{"clamped", map[string]any{"nudgeRetries": 99}, nil, false, models.MaxNudgeRetries},
		{"negative ignored", map[string]any{"nudgeRetries": -1}, nil, false, defaultNudgeRetries},
		{"legacy maxRounds for plan", nil, caps, true, 7},
		{"legacy maxRounds not for products", nil, caps, false, defaultNudgeRetries},
		{"node beats maxRounds", map[string]any{"nudgeRetries": 2}, caps, true, 2},
	}
	for _, tc := range cases {
		if got := nudgeRetries(NodeReq{Config: tc.cfg, Caps: tc.caps}, tc.plan); got != tc.want {
			t.Errorf("%s: nudgeRetries = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestRunAgentNudgeRetriesConfig drives an Agent that never writes its
// required product and counts the StructuredRetry turns it receives.
func TestRunAgentNudgeRetriesConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  any
		want int
	}{
		{"unset defaults to 3", nil, 3},
		{"zero never nudges", 0, 0},
		{"five", 5, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _, mgr, _, req := setupProvider(t, func(int) chatFunc {
				return func(int) turnAction { return turnAction{narration: "no product", outcome: true} }
			})
			req.Caps = testCapsWriting(models.SchemaReview)
			if tc.cfg != nil {
				req.Config["nudgeRetries"] = tc.cfg
			}
			_, _ = p.RunAgent(context.Background(), req)
			got := 0
			for i := 0; ; i++ {
				pr := mgr.bridge(0).promptAt(i)
				if pr == "" {
					break
				}
				if strings.Contains(pr, "本节点尚未写入结构化产物") {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("StructuredRetry turns = %d, want %d", got, tc.want)
			}
		})
	}
}
