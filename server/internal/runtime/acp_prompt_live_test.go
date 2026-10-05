package runtime

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestIsLiveTurn(t *testing.T) {
	cases := map[string]bool{
		"## Live 变体请求\n- sid: `a`": true,
		"## Live 上下文\n用户当前正在看":     true,
		"## Live 上下文(仅评论)\n当前查看会话": false,
		"这只是登录请求":                  false,
	}
	for human, want := range cases {
		if got := isLiveTurn(human); got != want {
			t.Errorf("isLiveTurn(%q)=%v, want %v", human, got, want)
		}
	}
}

func TestReactLiveFailuresPropagateToSettlement(t *testing.T) {
	for _, action := range []turnAction{{sendError: "connection lost"}, {failed: true, errorText: "provider failed"}} {
		p, _, _, _, _, req := setupProvider(t, func(int) chatFunc { return func(int) turnAction { return action } })
		req.NodeType = "agent"
		req.Caps = testClarifyCaps
		req.Config["direct_preview"] = true
		p.ReactOpen(context.Background(), req)
		got := p.ReactReply(context.Background(), req, []models.ReactMessage{{Role: "human", Text: "accept"}}, "accept", nil, false)
		if got.Err == nil || got.Done {
			t.Fatalf("failed Live turn must preserve error without completing node: %+v", got)
		}
		p.RetireSession(req.RunID, req.NodeID)
	}
}
