package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// The public share / embed preview of a taken-over node shows the auto
// phase's final narration on the opening turn (narration), never the raw
// auto-phase events (tool calls / paths).
func TestTakeoverSharePreviewNarration(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.WorkflowDef{ID: "wf-to", Name: "to", Version: 1, PublishedVersion: 1})
	h.db.Create(&models.Run{ID: "run-to", WorkflowID: "wf-to", WorkflowName: "to", Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "impl", Type: "agent", Label: "实现",
			Caps: &models.AgentCapabilities{Interaction: models.InteractionAuto}}}}})
	h.db.Create(&models.StateRun{RunID: "run-to", NodeID: "impl", NodeType: "agent", Iteration: 1,
		Status: "waiting_human", Mode: models.NodeModeManual, StartedAt: &now})
	h.db.Create(&models.ReactConversation{RunID: "run-to", NodeID: "impl", Iteration: 1, Takeover: true,
		Messages: []models.ReactMessage{{
			Role: "agent", Text: "已中断自动执行，本节点转为人工接管。", At: now.Format(time.RFC3339),
			Narration: "我已经完成了登录页的初稿。",
			Events: []models.AcpEvent{
				{Kind: "tool_call", Title: "Read /srv/secret/path/config.yaml", Text: "LEAK-TOOL-OUTPUT"},
				{Kind: "agent_message_chunk", Text: "我已经完成了登录页的初稿。"},
			},
		}}})

	w := h.do(http.MethodPost, "/api/runs/run-to/reviews/impl/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if w.Code != http.StatusOK {
		t.Fatalf("create share link for takeover: %d %s", w.Code, w.Body.String())
	}
	url, _ := parseJSON(t, w)["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	body := prev.Body.String()
	if prev.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", prev.Code, body)
	}
	p := parseJSON(t, prev)
	turns, _ := p["turns"].([]any)
	if len(turns) != 1 {
		t.Fatalf("turns: %s", body)
	}
	first, _ := turns[0].(map[string]any)
	if first["narration"] != "我已经完成了登录页的初稿。" || !strings.HasPrefix(first["text"].(string), "已中断自动执行") {
		t.Fatalf("opening turn: %v", first)
	}
	for _, leak := range []string{"/srv/secret/path", "LEAK-TOOL-OUTPUT", `"events"`, "run-to"} {
		if strings.Contains(body, leak) {
			t.Fatalf("preview leaked %q: %s", leak, body)
		}
	}
}
