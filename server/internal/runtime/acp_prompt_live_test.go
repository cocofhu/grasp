package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestPreviewNodePromptExtrasLive(t *testing.T) {
	cases := []struct {
		name     string
		nodeType string
		cfg      map[string]any
		want     bool
	}{
		{"app_preview live", "app_preview", map[string]any{"direct_preview": true, "live_variants": true}, true},
		{"live default on", "app_preview", map[string]any{"direct_preview": true}, true},
		{"live off", "app_preview", map[string]any{"direct_preview": true, "live_variants": false}, false},
		{"not direct", "app_preview", map[string]any{"live_variants": true}, false},
		{"grasp", "grasp", map[string]any{"direct_preview": true, "live_variants": true}, true},
		{"legacy approve default", "approve", map[string]any{"direct_preview": true}, true},
		{"grasp disabled", "grasp", map[string]any{"direct_preview": true, "live_variants": false}, false},
		{"review excluded", "review", map[string]any{"direct_preview": true}, false},
		{"clarify excluded", "react", map[string]any{"direct_preview": true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := NodeReq{NodeType: c.nodeType, Config: c.cfg}
			got := previewNodePromptExtras(req)
			if strings.Contains(got, models.DefaultPreviewLiveIndex) != c.want {
				t.Errorf("live index present=%v, want %v", !c.want, c.want)
			}
			if strings.Contains(got, models.DefaultPreviewLiveContract) {
				t.Error("system prompt must carry only the Live index, not the full contract")
			}
			skills := liveVariantSkills(req)
			if (len(skills) == 1 && skills[0] == liveVariantSkillDir) != c.want {
				t.Errorf("skills=%v, want live=%v", skills, c.want)
			}
		})
	}
}

func TestPreviewNodePromptExtrasManualKeepsPageControl(t *testing.T) {
	got := previewNodePromptExtras(NodeReq{NodeType: "app_preview", Config: map[string]any{
		"direct_preview": true, "auto_inject": false, "live_variants": "true",
	}})
	for _, part := range []string{models.DefaultPreviewDirectManualContract, models.DefaultPreviewPageControlContract, models.DefaultPreviewLiveIndex} {
		if !strings.Contains(got, part) {
			t.Errorf("missing contract part %.40q", part)
		}
	}
}

func TestReactGraspLiveSkillAndContractRehydrate(t *testing.T) {
	for _, nodeType := range []string{"grasp", "approve"} {
		t.Run(nodeType, func(t *testing.T) {
			p, _, _, mgr, _, req := setupProvider(t, func(int) chatFunc { return func(int) turnAction { return turnAction{narration: "Live ready"} } })
			req.NodeType = nodeType
			req.Config["direct_preview"] = true
			p.ReactOpen(context.Background(), req)
			assertSkill := func() {
				t.Helper()
				p.mu.Lock()
				sess := p.sessions[reactKey(req)]
				p.mu.Unlock()
				if sess == nil {
					t.Fatal("missing parked session")
				}
				for _, rel := range []string{"SKILL.md", "reference/typeset.md"} {
					body, err := os.ReadFile(filepath.Join(sess.home, liveVariantSkillDir, rel))
					if err != nil || len(body) == 0 {
						t.Fatalf("missing injected skill %s: %v", rel, err)
					}
				}
			}
			assertSkill()
			human := "## Live 变体请求\n- op: generate\n- sid: grasp01"
			history := []models.ReactMessage{{Role: "human", Text: human}}
			first := p.ReactReply(context.Background(), req, history, human, nil, false)
			if first.Done || first.Err != nil {
				t.Fatalf("Live finished Grasp: %+v", first)
			}
			// The first turn and each subsequent turn override the historical no-edit
			// role rule only for authorized Live requests. A server-lost session also
			// provisions the full skill in its newly created sandbox.
			p.RetireSession(req.RunID, req.NodeID)
			history = append(history, models.ReactMessage{Role: "agent", Text: first.Msg}, models.ReactMessage{Role: "human", Text: human})
			second := p.ReactReply(context.Background(), req, history, human, nil, false)
			if second.Done || second.Err != nil {
				t.Fatalf("rehydrated Live finished Grasp: %+v", second)
			}
			assertSkill()
			if mgr.createCount() != 2 {
				t.Fatalf("sandboxes=%d", mgr.createCount())
			}
			for _, prompt := range []string{mgr.bridge(0).promptAt(0), mgr.bridge(1).promptAt(1)} {
				if !strings.Contains(prompt, models.DefaultPreviewLiveContract) || !strings.Contains(prompt, models.DefaultGraspLiveContract) {
					t.Fatal("fresh/recovered Live turn missing scoped Live contract")
				}
			}
			if priming := mgr.bridge(1).promptAt(0); !strings.Contains(priming, models.DefaultPreviewLiveIndex) || strings.Contains(priming, models.DefaultPreviewLiveContract) {
				t.Fatal("rehydrate priming prompt must carry only the Live index")
			}
			p.RetireSession(req.RunID, req.NodeID)
		})
	}
}

func TestReactPlainTurnOmitsLiveContract(t *testing.T) {
	p, _, _, mgr, _, req := setupProvider(t, func(int) chatFunc { return func(int) turnAction { return turnAction{narration: "ok"} } })
	req.NodeType = "grasp"
	req.Config["direct_preview"] = true
	p.ReactOpen(context.Background(), req)
	turns := []string{
		"帮我用演示账号登录",
		"## Live 上下文(仅评论)\n当前查看会话 `s1` 的变体 1。\n这个按钮颜色不错",
	}
	var history []models.ReactMessage
	for _, human := range turns {
		history = append(history, models.ReactMessage{Role: "human", Text: human})
		got := p.ReactReply(context.Background(), req, history, human, nil, false)
		if got.Err != nil {
			t.Fatalf("plain turn failed: %+v", got)
		}
		history = append(history, models.ReactMessage{Role: "agent", Text: got.Msg})
	}
	for i := range turns {
		prompt := mgr.bridge(0).promptAt(i)
		if strings.Contains(prompt, models.DefaultPreviewLiveContract) || strings.Contains(prompt, models.DefaultGraspLiveContract) {
			t.Errorf("turn %d carries the Live contract: %.80q", i, prompt)
		}
	}
	p.RetireSession(req.RunID, req.NodeID)
}

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
		req.NodeType = "approve"
		req.Config["direct_preview"] = true
		p.ReactOpen(context.Background(), req)
		got := p.ReactReply(context.Background(), req, []models.ReactMessage{{Role: "human", Text: "accept"}}, "accept", nil, false)
		if got.Err == nil || got.Done {
			t.Fatalf("failed Live turn must preserve error without completing node: %+v", got)
		}
		p.RetireSession(req.RunID, req.NodeID)
	}
}
