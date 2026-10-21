package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// GET /runs/:id: a takeover conversation carries task (the visit's
// interpolated node prompt, secret vars masked, 280 runes + "…"); review
// conversations and blank prompts do not.
func TestTakeoverConversationTask(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto}
	review := &models.AgentCapabilities{Interaction: models.InteractionAuto, Review: true}
	g := models.Graph{Nodes: []models.Node{
		{ID: "design", Type: "agent", Label: "设计", Caps: auto, Config: map[string]any{"prompt": "设计"}},
		{ID: "impl", Type: "agent", Label: "实现", Caps: auto,
			Config: map[string]any{"prompt": "  部署 {{vars.tok}} 到 {{vars.env}}，按 {{nodes.design.outputs.content}}  "}},
		{ID: "long", Type: "agent", Label: "长", Caps: auto, Config: map[string]any{"prompt": strings.Repeat("长", 300)}},
		{ID: "quiet", Type: "agent", Label: "空", Caps: auto, Config: map[string]any{"prompt": "  "}},
		{ID: "rev", Type: "agent", Label: "复审", Caps: review, Config: map[string]any{"prompt": "复审任务"}},
	}}
	now := time.Now()
	h.db.Create(&models.Run{ID: "tt", Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: now})
	h.db.Create(&models.RunVariable{RunID: "tt", Name: "tok", Value: "s3cr3t-value", Secret: true})
	h.db.Create(&models.RunVariable{RunID: "tt", Name: "env", Value: "当前"})
	rows := []models.StateRun{
		{NodeID: "design", NodeType: "agent", Iteration: 1, Status: "completed", Outputs: map[string]any{"content": "方案一"}},
		{NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "completed", Mode: models.NodeModeManual,
			VarsBefore: map[string]any{"tok": "s3cr3t-value", "env": "预发"}},
		{NodeID: "long", NodeType: "agent", Iteration: 1, Status: "completed", Mode: models.NodeModeManual},
		{NodeID: "quiet", NodeType: "agent", Iteration: 1, Status: "completed", Mode: models.NodeModeManual},
		{NodeID: "rev", NodeType: "agent", Iteration: 1, Status: "waiting_human"},
	}
	for i := range rows {
		rows[i].RunID, rows[i].StartedAt = "tt", &now
		h.db.Create(&rows[i])
	}
	for _, c := range []models.ReactConversation{
		{NodeID: "impl", Iteration: 1, Takeover: true, Done: true},
		{NodeID: "long", Iteration: 1, Takeover: true, Done: true},
		{NodeID: "quiet", Iteration: 1, Takeover: true, Done: true},
		{NodeID: "rev", Iteration: 1},
	} {
		c.RunID = "tt"
		c.Messages = []models.ReactMessage{{Role: "agent", Text: "x"}}
		h.db.Create(&c)
	}
	w := h.do(http.MethodGet, "/api/runs/tt", nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"task":"部署 s3cr3t`) {
		t.Fatal("secret leaked into task")
	}
	cbn, _ := parseJSON(t, w)["clarifyByNode"].(map[string]any)
	task := func(node string) any {
		c, _ := cbn[node].(map[string]any)
		return c["task"]
	}
	if got := task("impl"); got != "部署 ••• 到 预发，按 方案一" {
		t.Fatalf("impl task=%v", got)
	}
	if got, _ := task("long").(string); len([]rune(got)) != 281 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long task=%q", got)
	}
	if task("quiet") != nil || task("rev") != nil {
		t.Fatalf("blank prompt / review conversation must omit task: %v %v", task("quiet"), task("rev"))
	}
}
