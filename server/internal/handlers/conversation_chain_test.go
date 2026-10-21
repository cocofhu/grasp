package handlers_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func chainGraph(longPrompt string) models.Graph {
	clarify := &models.AgentCapabilities{Interaction: models.InteractionClarify, Tools: []string{models.ToolAskQuestion}}
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto}
	review := &models.AgentCapabilities{Interaction: models.InteractionAuto, Review: true}
	return models.Graph{Nodes: []models.Node{
		{ID: "input", Type: "input", Label: "输入"},
		{ID: "clarify", Type: "agent", Label: "需求澄清", Caps: clarify, Config: map[string]any{"prompt": "  澄清「{{vars.topic}}」  "}},
		{ID: "design", Type: "agent", Label: "方案设计", Caps: auto, Config: map[string]any{"prompt": longPrompt}},
		{ID: "impl", Type: "agent", Label: "实现", Caps: review, Config: map[string]any{"prompt": "实现:{{nodes.design.outputs.content}}"}},
		{ID: "quiet", Type: "agent", Label: "无提示", Caps: auto, Config: map[string]any{"prompt": "   "}},
		{ID: "gate", Type: "human_gate", Label: "评审", Config: map[string]any{"prompt": "gate prompt must not show"}},
		{ID: "dev", Type: "agent", Label: "开发", Caps: auto, Config: map[string]any{"prompt": "开发 {{vars.topic}}"}},
	}}
}

type chainResp struct {
	RunID       string         `json:"runId"`
	Status      string         `json:"status"`
	CurrentNode map[string]any `json:"currentNode"`
	Segments    []struct {
		NodeID     string           `json:"nodeId"`
		Iteration  int              `json:"iteration"`
		Name       string           `json:"name"`
		Kind       string           `json:"kind"`
		Mode       string           `json:"mode"`
		Status     string           `json:"status"`
		StartedAt  *time.Time       `json:"startedAt"`
		EndedAt    *time.Time       `json:"endedAt"`
		Turns      []map[string]any `json:"turns"`
		EventCount *int             `json:"eventCount"`
		Decision   string           `json:"decision"`
		Task       *string          `json:"task"`
		Handoff    string           `json:"handoff"`
		HandoffSrc string           `json:"handoffSource"`
		raw        map[string]any
	} `json:"segments"`
}

// seedChainRun builds an execution history:
// input → clarify#1 → design#1 → impl#1 (review) → gate#1 → design#2 (loop)
// → impl#2 (review) → quiet#1 → dev#1 (taken over, waiting_human).
func seedChainRun(t *testing.T, h *harness, longPrompt string) {
	t.Helper()
	g := chainGraph(longPrompt)
	h.db.Create(&models.Run{ID: "chain", Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: time.Now()})
	h.db.Create(&models.RunVariable{RunID: "chain", Name: "topic", Value: "当前值"})
	t0 := time.Date(2026, 10, 21, 5, 0, 0, 0, time.UTC)
	at := func(m int) *time.Time { v := t0.Add(time.Duration(m) * time.Minute); return &v }
	ev := func(n int) []models.AcpEvent { return make([]models.AcpEvent, n) }
	rows := []models.StateRun{
		{NodeID: "input", NodeType: "input", Iteration: 1, Status: "completed", StartedAt: at(0)},
		{NodeID: "clarify", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: at(1), DurationSec: 120,
			VarsBefore: map[string]any{"topic": "导出 CSV"}},
		{NodeID: "design", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: at(5), DurationSec: 60,
			Events: ev(42), Outputs: map[string]any{"content": "方案一", "handoff": "  按方案一实现,注意导出编码  ", "handoff_source": "agent"}},
		{NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: at(7), Events: ev(3)},
		{NodeID: "gate", NodeType: "human_gate", Iteration: 1, Status: "completed", StartedAt: at(9),
			Outputs: map[string]any{"action": "revise"}},
		{NodeID: "design", NodeType: "agent", Iteration: 2, Status: "completed", StartedAt: at(10), Events: ev(7),
			Outputs: map[string]any{"content": "方案二"}},
		{NodeID: "impl", NodeType: "agent", Iteration: 2, Status: "completed", StartedAt: at(12)},
		{NodeID: "quiet", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: at(14)},
		{NodeID: "dev", NodeType: "agent", Iteration: 1, Status: "waiting_human", Mode: "manual", StartedAt: at(15), Events: ev(5)},
	}
	for i := range rows {
		rows[i].RunID = "chain"
		if err := h.db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	msg := func(role, text string) models.ReactMessage {
		return models.ReactMessage{Role: role, Text: text, At: "2026-10-21T05:00:00Z",
			Events:    []models.AcpEvent{{Kind: "tool_call", Title: "raw-event"}},
			Questions: []models.ReactQuestion{{ID: "q", Prompt: "哪一列", Options: []models.ReactOption{{ID: "a", Label: "全部"}}}},
			Tools:     []models.ReactTool{{Title: "Read", Status: "completed"}},
			Parts:     []models.AcpPart{{Kind: "tool", Title: "Read"}, {Kind: "message", Text: text}},
		}
	}
	convs := []models.ReactConversation{
		{NodeID: "clarify", Iteration: 1, Done: true, Messages: []models.ReactMessage{msg("agent", "要导出哪些列?"), msg("human", "全部")}},
		{NodeID: "impl", Iteration: 1, Done: true, Messages: []models.ReactMessage{msg("agent", "第一次复审"), msg("human", "改一下")}},
		{NodeID: "impl", Iteration: 2, Done: true, Messages: []models.ReactMessage{msg("agent", "第二次复审")}},
		{NodeID: "dev", Iteration: 1, Takeover: true, Messages: []models.ReactMessage{msg("agent", "已中断自动执行")}},
	}
	for i := range convs {
		convs[i].RunID = "chain"
		h.db.Create(&convs[i])
	}
}

func getChain(t *testing.T, h *harness, query string) (chainResp, int, string) {
	t.Helper()
	w := h.do("GET", "/api/runs/chain/conversation-chain"+query, nil)
	var out chainResp
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Segments []map[string]any `json:"segments"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &raw)
		for i := range out.Segments {
			out.Segments[i].raw = raw.Segments[i]
		}
	}
	return out, w.Code, w.Body.String()
}

func TestConversationChain(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	long := strings.Repeat("长", 300) + "{{vars.topic}}"
	seedChainRun(t, h, long)
	out, code, body := getChain(t, h, "")
	if code != 200 {
		t.Fatalf("chain: %d %s", code, body)
	}
	if out.RunID != "chain" || out.Status != "waiting_human" {
		t.Fatalf("header: %+v", out)
	}
	if cn := out.CurrentNode; cn["id"] != "dev" || cn["kind"] != "takeover" || cn["mode"] != "manual" || cn["state"] != "waiting_human" || cn["name"] != "开发" {
		t.Fatalf("currentNode: %v", cn)
	}
	type want struct {
		node string
		iter int
		kind string
		mode string
	}
	wants := []want{
		{"input", 1, "agent", "auto"}, {"clarify", 1, "clarify", "manual"}, {"design", 1, "agent", "auto"},
		{"impl", 1, "review", "manual"}, {"gate", 1, "gate", "manual"}, {"design", 2, "agent", "auto"},
		{"impl", 2, "review", "manual"}, {"quiet", 1, "agent", "auto"}, {"dev", 1, "takeover", "manual"},
	}
	if len(out.Segments) != len(wants) {
		t.Fatalf("segments=%d: %s", len(out.Segments), body)
	}
	for i, w := range wants {
		s := out.Segments[i]
		if s.NodeID != w.node || s.Iteration != w.iter || s.Kind != w.kind || s.Mode != w.mode {
			t.Fatalf("segment %d = %s#%d %s/%s, want %+v", i, s.NodeID, s.Iteration, s.Kind, s.Mode, w)
		}
	}
	seg := out.Segments
	// Per-iteration conversations (multi-iteration loop). Turns keep questions,
	// tools and parts; images and the raw event log stay off the chain.
	if len(seg[3].Turns) != 2 || seg[3].Turns[0]["text"] != "第一次复审" || len(seg[6].Turns) != 1 || seg[6].Turns[0]["text"] != "第二次复审" {
		t.Fatalf("impl turns per iteration: #1=%v #2=%v", seg[3].Turns, seg[6].Turns)
	}
	allowedTurnKey := map[string]bool{"role": true, "text": true, "at": true, "questions": true, "tools": true, "parts": true, "forms": true}
	for _, turn := range seg[1].Turns {
		for k := range turn {
			if !allowedTurnKey[k] {
				t.Fatalf("unexpected turn key %q", k)
			}
		}
	}
	qTurn := seg[1].Turns[0]
	qs, _ := qTurn["questions"].([]any)
	tools, _ := qTurn["tools"].([]any)
	parts, _ := qTurn["parts"].([]any)
	if len(qs) != 1 || len(tools) != 1 || len(parts) != 2 {
		t.Fatalf("clarify detail: questions=%v tools=%v parts=%v", qs, tools, parts)
	}
	if _, ok := qTurn["events"]; ok {
		t.Fatal("turns must not carry raw events")
	}
	if seg[1].Turns[1]["role"] != "human" || seg[1].Turns[1]["text"] != "全部" {
		t.Fatalf("clarify turns: %v", seg[1].Turns)
	}
	// Takeover segment carries its conversation.
	if len(seg[8].Turns) != 1 || seg[8].Turns[0]["text"] != "已中断自动执行" {
		t.Fatalf("takeover turns: %v", seg[8].Turns)
	}
	// Auto segments: eventCount, no turns.
	if seg[2].EventCount == nil || *seg[2].EventCount != 42 || seg[5].EventCount == nil || *seg[5].EventCount != 7 {
		t.Fatalf("eventCount: %v %v", seg[2].EventCount, seg[5].EventCount)
	}
	if _, ok := seg[2].raw["turns"]; ok {
		t.Fatal("auto segment must not carry turns")
	}
	// Review / takeover segments also carry their auto-phase eventCount.
	if seg[3].EventCount == nil || *seg[3].EventCount != 3 || seg[6].EventCount == nil || *seg[6].EventCount != 0 ||
		seg[8].EventCount == nil || *seg[8].EventCount != 5 {
		t.Fatalf("review/takeover eventCount: %v %v %v", seg[3].EventCount, seg[6].EventCount, seg[8].EventCount)
	}
	// Gate segment: turns (empty: no discussion), decision, no eventCount, no task.
	if seg[4].Turns == nil || len(seg[4].Turns) != 0 || seg[4].Decision != "rejected" {
		t.Fatalf("gate segment: %v", seg[4].raw)
	}
	if _, ok := seg[4].raw["eventCount"]; ok {
		t.Fatal("gate segment must not carry eventCount")
	}
	if _, ok := seg[1].raw["eventCount"]; ok {
		t.Fatal("clarify segment must not carry eventCount")
	}
	// endedAt = startedAt + duration for finished executions only.
	if seg[1].EndedAt == nil || !seg[1].EndedAt.Equal(seg[1].StartedAt.Add(2*time.Minute)) {
		t.Fatalf("endedAt: %v", seg[1].EndedAt)
	}
	if seg[8].EndedAt != nil {
		t.Fatal("a live execution has no endedAt")
	}

	// Task rules.
	task := func(i int) string {
		if seg[i].Task == nil {
			return "<absent>"
		}
		return *seg[i].Task
	}
	if got := task(1); got != "澄清「导出 CSV」" { // trimmed, VarsBefore interpolation
		t.Fatalf("clarify task=%q", got)
	}
	if got := task(2); len([]rune(got)) != 281 || !strings.HasSuffix(got, "…") || strings.Contains(got, "{{") {
		t.Fatalf("truncated task (%d runes)=%q", len([]rune(got)), got)
	}
	if got := task(3); got != "实现:方案一" { // node outputs as of that execution
		t.Fatalf("impl#1 task=%q", got)
	}
	if got := task(6); got != "实现:方案二" {
		t.Fatalf("impl#2 task=%q", got)
	}
	if got := task(8); got != "开发 当前值" { // no VarsBefore → current run vars
		t.Fatalf("takeover task=%q", got)
	}
	for _, i := range []int{0, 4, 7} { // non-Agent node, gate, blank prompt
		if seg[i].Task != nil {
			t.Fatalf("segment %d (%s) must omit task, got %q", i, seg[i].NodeID, *seg[i].Task)
		}
	}

	// Read-only: nothing was written.
	var vars int64
	h.db.Model(&models.RunVariable{}).Where("run_id = ?", "chain").Count(&vars)
	var r models.Run
	h.db.First(&r, "id = ?", "chain")
	if vars != 1 || r.Status != "waiting_human" {
		t.Fatalf("chain must be read-only: vars=%d status=%s", vars, r.Status)
	}
}

func TestConversationChainHandoff(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	seedChainRun(t, h, "长")
	out, code, body := getChain(t, h, "")
	if code != 200 {
		t.Fatalf("chain: %d %s", code, body)
	}
	for _, s := range out.Segments {
		switch {
		case s.NodeID == "design" && s.Iteration == 1:
			if s.Handoff != "按方案一实现,注意导出编码" || s.HandoffSrc != "agent" {
				t.Fatalf("design#1 handoff: %q %q", s.Handoff, s.HandoffSrc)
			}
		default:
			if _, ok := s.raw["handoff"]; ok {
				t.Fatalf("%s#%d: unexpected handoff %v", s.NodeID, s.Iteration, s.raw["handoff"])
			}
		}
	}
}

func TestConversationChainBoot(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	seedChainRun(t, h, "长")
	ready := time.Date(2026, 10, 21, 5, 5, 30, 0, time.UTC)
	h.db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ? AND iteration = ?", "chain", "design", 1).
		Update("sandbox_ready_at", ready)
	h.db.Create(&models.Sandbox{Name: "sb-dev", Purpose: "run", Status: "creating", RunID: "chain", NodeID: "dev"})
	h.db.Create(&models.Sandbox{Name: "sb-quiet", Purpose: "run", Status: "creating", RunID: "chain", NodeID: "quiet"})
	out, code, body := getChain(t, h, "")
	if code != 200 {
		t.Fatalf("chain: %d %s", code, body)
	}
	for _, s := range out.Segments {
		boot, _ := s.raw["boot"].(map[string]any)
		switch {
		case s.NodeID == "design" && s.Iteration == 1:
			if boot["status"] != "ready" || boot["readyAt"] != "2026-10-21T05:05:30Z" {
				t.Fatalf("design#1 boot: %v", boot)
			}
		case s.NodeID == "dev":
			if boot["status"] != "creating" || boot["readyAt"] != nil {
				t.Fatalf("dev boot: %v", boot)
			}
		default:
			// quiet finished: a leftover creating row is not its boot.
			if boot != nil {
				t.Fatalf("%s#%d: unexpected boot %v", s.NodeID, s.Iteration, boot)
			}
		}
	}
}

func TestConversationChainResumedDialogueShowsOnce(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	g := chainGraph("p")
	h.db.Create(&models.Run{ID: "chain", Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: time.Now()})
	t0 := time.Date(2026, 10, 21, 5, 0, 0, 0, time.UTC)
	h.db.Create(&models.StateRun{RunID: "chain", NodeID: "clarify", NodeType: "agent", Iteration: 1, Status: "cancelled", StartedAt: &t0})
	t1 := t0.Add(time.Minute)
	h.db.Create(&models.StateRun{RunID: "chain", NodeID: "clarify", NodeType: "agent", Iteration: 2, Status: "waiting_human", StartedAt: &t1})
	old := []models.ReactMessage{
		{Role: "human", Text: "把登录做清楚", At: "2026-10-21T05:00:01Z"},
		{Role: "agent", Text: "要支持哪些登录方式?", At: "2026-10-21T05:00:05Z"},
	}
	h.db.Create(&models.ReactConversation{RunID: "chain", NodeID: "clarify", Iteration: 1, Messages: old})
	h.db.Create(&models.ReactConversation{RunID: "chain", NodeID: "clarify", Iteration: 2,
		Messages: append(append([]models.ReactMessage{}, old...), models.ReactMessage{Role: "human", Text: "密码和短信", At: "2026-10-21T05:01:10Z"})})
	out, code, body := getChain(t, h, "")
	if code != 200 || len(out.Segments) != 2 {
		t.Fatalf("chain: %d %s", code, body)
	}
	if n := len(out.Segments[0].Turns); n != 0 {
		t.Fatalf("interrupted visit should hand its turns to the resumed one, has %d", n)
	}
	if n := len(out.Segments[1].Turns); n != 3 {
		t.Fatalf("resumed visit turns = %d, want 3", n)
	}
}

func TestConversationChainUntil(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	seedChainRun(t, h, "p")
	ids := func(out chainResp) string {
		var s []string
		for _, g := range out.Segments {
			s = append(s, g.NodeID+"#"+string(rune('0'+g.Iteration)))
		}
		return strings.Join(s, ",")
	}
	// until without iteration → that node's LAST execution, inclusive.
	out, code, body := getChain(t, h, "?until=impl")
	if code != 200 || ids(out) != "input#1,clarify#1,design#1,impl#1,gate#1,design#2,impl#2" {
		t.Fatalf("until=impl: %d %s / %s", code, ids(out), body)
	}
	out, code, _ = getChain(t, h, "?until=impl&iteration=1")
	if code != 200 || ids(out) != "input#1,clarify#1,design#1,impl#1" {
		t.Fatalf("until=impl&iteration=1: %d %s", code, ids(out))
	}
	if out.CurrentNode["id"] != "dev" {
		t.Fatal("currentNode is the run's, not the cut point")
	}
	for q, wantCode := range map[string]int{
		"?until=impl&iteration=9": 400, "?until=nope": 400, "?iteration=1": 400,
		"?until=impl&iteration=0": 400, "?until=impl&iteration=x": 400,
	} {
		if _, code, body := getChain(t, h, q); code != wantCode {
			t.Fatalf("%s: %d %s", q, code, body)
		}
	}
	if w := h.do("GET", "/api/runs/missing/conversation-chain", nil); w.Code != 404 {
		t.Fatalf("unknown run: %d", w.Code)
	}
}
