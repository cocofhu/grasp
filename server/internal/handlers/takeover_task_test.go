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

// The same task preview on GET /runs/:id currentNode (Agent nodes only,
// never gates) and on the node events endpoint (first request only; cursor /
// limit pages omit it).
func TestCurrentNodeAndNodeEventsTask(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto}
	g := models.Graph{Nodes: []models.Node{
		{ID: "impl", Type: "agent", Label: "实现", Caps: auto, Config: map[string]any{"prompt": "用 {{vars.tok}} 实现 {{vars.env}}"}},
		{ID: "gate", Type: "human_gate", Label: "评审", Config: map[string]any{"prompt": "gate prompt"}},
	}}
	now := time.Now()
	h.db.Create(&models.Run{ID: "live", Status: "running", WorkflowID: "wf", Graph: g, StartedAt: now})
	h.db.Create(&models.RunVariable{RunID: "live", Name: "tok", Value: "s3cr3t-value", Secret: true})
	h.db.Create(&models.RunVariable{RunID: "live", Name: "env", Value: "预发"})
	h.db.Create(&models.StateRun{RunID: "live", NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now,
		VarsBefore: map[string]any{"tok": "s3cr3t-value", "env": "旧"}})
	h.db.Create(&models.StateRun{RunID: "live", NodeID: "impl", NodeType: "agent", Iteration: 2, Status: "running", StartedAt: &now,
		VarsBefore: map[string]any{"tok": "s3cr3t-value", "env": "预发"},
		Events:     []models.AcpEvent{{Kind: "message", Text: "hi"}}})
	const want = "用 ••• 实现 预发"

	w := h.do(http.MethodGet, "/api/runs/live", nil)
	cn, _ := parseJSON(t, w)["currentNode"].(map[string]any)
	if cn["id"] != "impl" || cn["iteration"] != float64(2) || cn["task"] != want {
		t.Fatalf("currentNode: %v", cn)
	}

	ev := parseJSON(t, h.do(http.MethodGet, "/api/runs/live/nodes/impl/events", nil))
	if ev["task"] != want || ev["live"] != false {
		t.Fatalf("node events: %v", ev)
	}
	if evs, _ := ev["events"].([]any); len(evs) != 1 {
		t.Fatalf("events: %v", ev)
	}
	page := parseJSON(t, h.do(http.MethodGet, "/api/runs/live/nodes/impl/events?limit=10", nil))
	if _, ok := page["task"]; ok {
		t.Fatalf("paginated request must omit task: %v", page)
	}

	// A gate as current node: no task; the gate's events request has none either.
	h.db.Create(&models.StateRun{RunID: "live", NodeID: "gate", NodeType: "human_gate", Iteration: 1, Status: "waiting_human", StartedAt: &now})
	h.db.Model(&models.Run{}).Where("id = ?", "live").Update("status", "waiting_human")
	h.db.Create(&models.Gate{RunID: "live", NodeID: "gate", Iteration: 1, Title: "评审", RequestedAt: now})
	cn, _ = parseJSON(t, h.do(http.MethodGet, "/api/runs/live", nil))["currentNode"].(map[string]any)
	if cn["id"] != "gate" || cn["task"] != nil {
		t.Fatalf("gate currentNode must omit task: %v", cn)
	}
	if ev := parseJSON(t, h.do(http.MethodGet, "/api/runs/live/nodes/gate/events", nil)); ev["task"] != nil {
		t.Fatalf("gate events must omit task: %v", ev)
	}
}

// GET /runs/:id/inbox-context: a takeover conversation's clarify object
// carries takeover:true and the same task as clarifyByNode; a review
// conversation carries neither.
func TestInboxContextTakeoverTask(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto}
	review := &models.AgentCapabilities{Interaction: models.InteractionAuto, Review: true}
	g := models.Graph{Nodes: []models.Node{
		{ID: "impl", Type: "agent", Label: "实现", Caps: auto, Config: map[string]any{"prompt": "部署 {{vars.tok}}"}},
		{ID: "rev", Type: "agent", Label: "复审", Caps: review, Config: map[string]any{"prompt": "复审任务"}},
	}}
	now := time.Now()
	for _, id := range []string{"ib-to", "ib-rev"} {
		h.db.Create(&models.Run{ID: id, Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: now})
		h.db.Create(&models.RunVariable{RunID: id, Name: "tok", Value: "s3cr3t-value", Secret: true})
	}
	h.db.Create(&models.StateRun{RunID: "ib-to", NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "waiting_human",
		Mode: models.NodeModeManual, StartedAt: &now, VarsBefore: map[string]any{"tok": "s3cr3t-value"}})
	h.db.Create(&models.ReactConversation{RunID: "ib-to", NodeID: "impl", Iteration: 1, Takeover: true,
		Messages: []models.ReactMessage{{Role: "agent", Text: "已中断"}}})
	h.db.Create(&models.StateRun{RunID: "ib-rev", NodeID: "rev", NodeType: "agent", Iteration: 1, Status: "waiting_human", StartedAt: &now})
	h.db.Create(&models.ReactConversation{RunID: "ib-rev", NodeID: "rev", Iteration: 1,
		Messages: []models.ReactMessage{{Role: "agent", Text: "请复审"}}})

	w := h.do(http.MethodGet, "/api/runs/ib-to/nodes/impl/context?iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	cl, _ := parseJSON(t, w)["clarify"].(map[string]any)
	if cl["takeover"] != true || cl["task"] != "部署 •••" {
		t.Fatalf("takeover clarify: %v", cl)
	}
	w = h.do(http.MethodGet, "/api/runs/ib-rev/nodes/rev/context?iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	cl, _ = parseJSON(t, w)["clarify"].(map[string]any)
	if _, ok := cl["takeover"]; ok {
		t.Fatalf("review clarify must omit takeover: %v", cl)
	}
	if _, ok := cl["task"]; ok {
		t.Fatalf("review clarify must omit task: %v", cl)
	}
}
