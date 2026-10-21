package handlers_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// gateReactChainRun: impl#1 (auto) → gate#1 (hot-revised twice, then
// rejected) → impl#2 (auto) → gate#2 (open, legacy untagged discussion).
func gateReactChainRun(t *testing.T, h *harness) {
	t.Helper()
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto}
	g := models.Graph{Nodes: []models.Node{
		{ID: "impl", Type: "agent", Label: "实现", Caps: auto, Config: map[string]any{"prompt": "实现"}},
		{ID: "gate", Type: "human_gate", Label: "评审", Config: map[string]any{"body_template": "{{nodes.impl.outputs.page}}"}},
	}}
	h.db.Create(&models.Run{ID: "gr", Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: time.Now()})
	t0 := time.Date(2026, 10, 21, 5, 0, 0, 0, time.UTC)
	at := func(sec int) *time.Time { v := t0.Add(time.Duration(sec) * time.Second); return &v }
	ts := func(sec int) string { return t0.Add(time.Duration(sec) * time.Second).Format(time.RFC3339) }
	rows := []models.StateRun{
		{NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: at(0), DurationSec: 50, Events: make([]models.AcpEvent, 9)},
		{NodeID: "gate", NodeType: "human_gate", Iteration: 1, Status: "completed", StartedAt: at(60), DurationSec: 100,
			Outputs: map[string]any{"action": "revise"}},
		{NodeID: "impl", NodeType: "agent", Iteration: 2, Status: "completed", StartedAt: at(200), DurationSec: 50, Events: make([]models.AcpEvent, 4)},
		{NodeID: "gate", NodeType: "human_gate", Iteration: 2, Status: "waiting_human", StartedAt: at(300)},
	}
	for i := range rows {
		rows[i].RunID = "gr"
		if err := h.db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	h.db.Create(&models.ReactConversation{RunID: "gr", NodeID: "impl", Iteration: 1, Messages: []models.ReactMessage{
		{Role: "agent", Text: "summary", At: ts(70), GateNodeID: "gate"},
		{Role: "human", Text: "gate 里的修改意见", At: ts(70), GateNodeID: "gate"},
		{Role: "agent", Text: "已修改", At: ts(90), GateNodeID: "gate"},
	}})
	// Legacy rows (no gateNodeId): attributed by time window. The first turn is
	// in the same second as gate#2's start and stays with the producer.
	h.db.Create(&models.ReactConversation{RunID: "gr", NodeID: "impl", Iteration: 2, Messages: []models.ReactMessage{
		{Role: "human", Text: "确认", At: ts(300)},
		{Role: "agent", Text: "summary2", At: ts(310)},
		{Role: "human", Text: "再改", At: ts(311)},
	}})
}

func TestConversationChainGateReactSegments(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	gateReactChainRun(t, h)
	w := h.do("GET", "/api/runs/gr/conversation-chain", nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var out chainResp
	decodeChain(t, w.Body.Bytes(), &out)
	got := ""
	for _, s := range out.Segments {
		got += fmt.Sprintf("%s#%d/%s/%s;", s.NodeID, s.Iteration, s.Kind, s.Mode)
	}
	// impl#2 keeps the untagged same-second turn, so it is a review.
	if got != "impl#1/agent/auto;gate#1/gate/manual;impl#2/review/manual;gate#2/gate/manual;" {
		t.Fatalf("chain=%s body=%s", got, w.Body.String())
	}
	s := out.Segments
	if s[0].Turns != nil || s[0].EventCount == nil || *s[0].EventCount != 9 {
		t.Fatalf("producer keeps kind/eventCount and loses gate turns: %+v", s[0])
	}
	if len(s[1].Turns) != 3 || s[1].Turns[1]["text"] != "gate 里的修改意见" || s[1].Decision != "rejected" || s[1].EndedAt == nil {
		t.Fatalf("gate#1: %+v", s[1])
	}
	if s[1].Task != nil || s[1].EventCount != nil {
		t.Fatalf("gate segment carries no task/eventCount: %+v", s[1])
	}
	if len(s[2].Turns) != 1 || s[2].Turns[0]["text"] != "确认" || s[2].EventCount == nil || *s[2].EventCount != 4 {
		t.Fatalf("impl#2: %+v", s[2])
	}
	if len(s[3].Turns) != 2 || s[3].Turns[0]["text"] != "summary2" || s[3].Decision != "" || s[3].EndedAt != nil {
		t.Fatalf("gate#2: %+v", s[3])
	}
	// until cuts after impl#1: gate#1 turns are not leaked back onto it.
	w = h.do("GET", "/api/runs/gr/conversation-chain?until=impl&iteration=1", nil)
	var cut chainResp
	decodeChain(t, w.Body.Bytes(), &cut)
	if len(cut.Segments) != 1 || cut.Segments[0].Kind != "agent" || cut.Segments[0].Turns != nil {
		t.Fatalf("cut: %s", w.Body.String())
	}
}

// TestConversationChainLargeRun: 300 executions with 200 events each are
// counted without loading the event payloads (correct counts, task per row).
func TestConversationChainLargeRun(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto}
	g := models.Graph{Nodes: []models.Node{{ID: "a", Type: "agent", Caps: auto,
		Config: map[string]any{"prompt": "x {{nodes.a.outputs.content}} {{artifact(\"spec.md\")}}"}}}}
	h.db.Create(&models.Run{ID: "big", Status: "completed", WorkflowID: "wf", Graph: g, StartedAt: time.Now()})
	ev := make([]models.AcpEvent, 200)
	for i := range ev {
		ev[i] = models.AcpEvent{Kind: "tool_call", Text: string(make([]byte, 500))}
	}
	now := time.Now()
	for i := 1; i <= 300; i++ {
		h.db.Create(&models.StateRun{RunID: "big", NodeID: "a", NodeType: "agent", Iteration: i, Status: "completed",
			StartedAt: &now, Events: ev, Outputs: map[string]any{"content": fmt.Sprint(i)}})
	}
	start := time.Now()
	w := h.do("GET", "/api/runs/big/conversation-chain", nil)
	took := time.Since(start)
	var out chainResp
	decodeChain(t, w.Body.Bytes(), &out)
	t.Logf("300 segments x 200 events: code=%d bytes=%d took=%s", w.Code, w.Body.Len(), took)
	if len(out.Segments) != 300 {
		t.Fatalf("segments=%d", len(out.Segments))
	}
	for i, s := range out.Segments {
		if s.EventCount == nil || *s.EventCount != 200 {
			t.Fatalf("segment %d eventCount=%v", i, s.EventCount)
		}
		if want := fmt.Sprintf("x %d", i); i > 0 && (s.Task == nil || *s.Task != want) {
			t.Fatalf("segment %d task=%v want %q", i, s.Task, want)
		}
	}
}

func decodeChain(t *testing.T, b []byte, out *chainResp) {
	t.Helper()
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode: %v: %s", err, b)
	}
}

// GraspReview #850 repro, adapted to the gate-segment contract: gate-react
// turns (stored under the producer's (node, iter)) show on the gate segment;
// the producer keeps kind=agent/auto and its eventCount.
func TestReview850GateReactAttribution(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto}
	g := models.Graph{Nodes: []models.Node{
		{ID: "impl", Type: "agent", Label: "实现", Caps: auto, Config: map[string]any{"prompt": "p"}},
		{ID: "gate", Type: "human_gate", Label: "评审", Config: map[string]any{"body_template": "{{nodes.impl.outputs.page}}"}},
	}}
	h.db.Create(&models.Run{ID: "gr", Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: time.Now()})
	now := time.Now()
	later := now.Add(time.Second)
	at := later.Add(time.Second).Format(time.RFC3339)
	h.db.Create(&models.StateRun{RunID: "gr", NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now, Events: make([]models.AcpEvent, 9)})
	h.db.Create(&models.StateRun{RunID: "gr", NodeID: "gate", NodeType: "human_gate", Iteration: 1, Status: "waiting_human", StartedAt: &later})
	h.db.Create(&models.ReactConversation{RunID: "gr", NodeID: "impl", Iteration: 1, Messages: []models.ReactMessage{
		{Role: "agent", Text: "summary", At: at, GateNodeID: "gate"},
		{Role: "human", Text: "gate 里的修改意见", At: at, GateNodeID: "gate"},
	}})
	w := h.do("GET", "/api/runs/gr/conversation-chain", nil)
	var out struct {
		Segments []map[string]any `json:"segments"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Segments) != 2 {
		t.Fatalf("%d: %s", len(out.Segments), w.Body.String())
	}
	p, gt := out.Segments[0], out.Segments[1]
	if p["kind"] != "agent" || p["mode"] != "auto" || p["eventCount"] != float64(9) {
		t.Errorf("producer must keep agent/auto + eventCount: %v", p)
	}
	if _, ok := p["turns"]; ok {
		t.Errorf("producer must not carry the gate turns: %v", p)
	}
	turns, _ := gt["turns"].([]any)
	if gt["kind"] != "gate" || gt["mode"] != "manual" || gt["nodeId"] != "gate" || gt["iteration"] != float64(1) || len(turns) != 2 {
		t.Errorf("gate-react turns are not on the gate segment: %v", gt)
	}
	if _, ok := gt["task"]; ok {
		t.Errorf("gate segment has no task: %v", gt)
	}
}
