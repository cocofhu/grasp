package handlers_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func takeoverHandlerGraph() models.Graph {
	auto := &models.AgentCapabilities{Interaction: models.InteractionAuto, Reads: []string{"*"}}
	return models.Graph{Nodes: []models.Node{
		{ID: "impl", Type: "agent", Label: "实现", Caps: auto, Config: map[string]any{"agent_profile": "dev"}},
		{ID: "gate", Type: "human_gate", Label: "评审"},
	}}
}

// TestTakeoverEndpointCodes covers the HTTP contract: 404 / 409 bodies with
// codes and current, 200 already_manual, 400 on a bad body.
func TestTakeoverEndpointCodes(t *testing.T) {
	h := newHarness(t)
	g := takeoverHandlerGraph()
	h.db.Create(&models.Run{ID: "r-run", Status: "running", WorkflowID: "wf", Graph: g})
	h.db.Create(&models.StateRun{RunID: "r-run", NodeID: "impl", NodeType: "agent", Iteration: 2, Status: "running"})
	h.db.Create(&models.Run{ID: "r-done", Status: "completed", WorkflowID: "wf", Graph: g})
	h.db.Create(&models.Run{ID: "r-manual", Status: "waiting_human", WorkflowID: "wf", Graph: g})
	h.db.Create(&models.StateRun{RunID: "r-manual", NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "waiting_human", Mode: "manual"})

	type resp struct {
		code int
		body map[string]any
	}
	call := func(run, node string, body any) resp {
		t.Helper()
		w := h.do("POST", "/api/runs/"+run+"/nodes/"+node+"/takeover", body)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return resp{w.Code, m}
	}
	expect := func(r resp, code int, errCode string) {
		t.Helper()
		if r.code != code || (errCode != "" && r.body["error"] != errCode) {
			t.Fatalf("got %d %v, want %d %s", r.code, r.body, code, errCode)
		}
	}
	expect(call("nope", "impl", nil), 404, "run_not_found")
	expect(call("r-run", "nope", nil), 404, "node_not_found")
	expect(call("r-run", "gate", nil), 409, "not_auto_node")
	expect(call("r-done", "impl", nil), 409, "run_not_running")
	stale := call("r-run", "impl", map[string]any{"iteration": 1})
	expect(stale, 409, "node_not_current")
	cur, _ := stale.body["current"].(map[string]any)
	if cur["runStatus"] != "running" || cur["iteration"] != float64(2) || cur["mode"] != "auto" || stale.body["message"] == "" {
		t.Fatalf("409 body must carry message + current: %v", stale.body)
	}
	// Running visit with no in-flight auto call (driver between steps).
	expect(call("r-run", "impl", nil), 409, "node_finishing")
	ok := call("r-manual", "impl", map[string]any{})
	expect(ok, 200, "")
	if ok.body["status"] != "already_manual" || ok.body["state"] != "waiting_human" || ok.body["mode"] != "manual" || ok.body["iteration"] != float64(1) {
		t.Fatalf("idempotent 200 body: %v", ok.body)
	}
	expect(call("r-run", "impl", map[string]any{"iteration": -1}), 400, "")

	// Run detail exposes the visit mode and the takeover conversation flag.
	h.db.Create(&models.ReactConversation{RunID: "r-manual", NodeID: "impl", Iteration: 1, Takeover: true,
		Messages: []models.ReactMessage{{Role: "agent", Text: "已中断"}}})
	w := h.do("GET", "/api/runs/r-manual", nil)
	var detail map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &detail)
	nr, _ := detail["nodeRuns"].(map[string]any)
	impl, _ := nr["impl"].(map[string]any)
	if impl["mode"] != "manual" {
		t.Fatalf("nodeRuns.impl.mode = %v", impl["mode"])
	}
	// /api/pending-nodes lists the takeover as a review entry with takeover=true.
	gw := h.do("GET", "/api/pending-nodes", nil)
	if gw.Code != 200 || !strings.Contains(gw.Body.String(), `"takeover":true`) || !strings.Contains(gw.Body.String(), `"kind":"review"`) {
		t.Fatalf("/api/pending-nodes: %d %s", gw.Code, gw.Body)
	}
	cb, _ := detail["clarifyByNode"].(map[string]any)
	conv, _ := cb["impl"].(map[string]any)
	if conv["takeover"] != true {
		t.Fatalf("clarifyByNode.impl.takeover = %v", conv["takeover"])
	}
}

// TestListRunsLastActivityAndCurrentNode: sort=last_activity orders by the
// effective activity time, and each active run carries currentNode +
// lastActivityAt.
func TestListRunsLastActivityAndCurrentNode(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	g := takeoverHandlerGraph()
	base := time.Date(2026, 10, 21, 4, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := base.Add(d); return &v }
	h.db.Create(&models.Run{ID: "r-old-active", Status: "running", WorkflowID: "wf", Graph: g, StartedAt: base, LastActivityAt: at(50 * time.Minute)})
	h.db.Create(&models.Run{ID: "r-new-idle", Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: base.Add(30 * time.Minute), LastActivityAt: at(31 * time.Minute)})
	h.db.Create(&models.Run{ID: "r-stopping", Status: "running", WorkflowID: "wf", Graph: g, StartedAt: base.Add(5 * time.Minute), LastActivityAt: at(40 * time.Minute)})
	h.db.Create(&models.Run{ID: "r-legacy", Status: "completed", WorkflowID: "wf", Graph: g, StartedAt: base.Add(35 * time.Minute)})
	h.db.Create(&models.StateRun{RunID: "r-old-active", NodeID: "impl", Iteration: 1, Status: "running"})
	h.db.Create(&models.StateRun{RunID: "r-new-idle", NodeID: "impl", Iteration: 3, Status: "waiting_human", Mode: "manual"})
	h.db.Create(&models.StateRun{RunID: "r-stopping", NodeID: "impl", Iteration: 1, Status: "running", Mode: "manual"})

	w := h.do("GET", "/api/runs?sort=last_activity&order=desc", nil)
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	var rows []map[string]any
	if err := decodePageItems(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	var order []string
	byID := map[string]map[string]any{}
	for _, r := range rows {
		order = append(order, r["id"].(string))
		byID[r["id"].(string)] = r
	}
	want := []string{"r-old-active", "r-stopping", "r-legacy", "r-new-idle"}
	if len(order) != len(want) {
		t.Fatalf("order=%v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v, want %v", order, want)
		}
	}
	if ts, _ := byID["r-legacy"]["lastActivityAt"].(string); ts == "" {
		t.Fatal("legacy row must fall back to startedAt for lastActivityAt")
	}
	node := func(id string) map[string]any {
		cn, _ := byID[id]["currentNode"].(map[string]any)
		return cn
	}
	if cn := node("r-old-active"); cn["id"] != "impl" || cn["name"] != "实现" || cn["kind"] != "agent" || cn["mode"] != "auto" || cn["state"] != "running" || cn["iteration"] != float64(1) {
		t.Fatalf("auto running currentNode: %v", cn)
	}
	if cn := node("r-stopping"); cn["kind"] != "takeover" || cn["mode"] != "manual" || cn["state"] != "stopping" {
		t.Fatalf("stopping currentNode: %v", cn)
	}
	if cn := node("r-new-idle"); cn["kind"] != "takeover" || cn["mode"] != "manual" || cn["state"] != "waiting_human" || cn["iteration"] != float64(3) {
		t.Fatalf("manual idle currentNode: %v", cn)
	}
	if node("r-legacy") != nil {
		t.Fatal("terminal runs carry no currentNode")
	}
	if byID["r-old-active"]["currentNodeLabel"] != "实现" {
		t.Fatal("currentNodeLabel must be kept for compatibility")
	}

	// Ascending is whitelisted too.
	w = h.do("GET", "/api/runs?sort=last_activity&order=asc", nil)
	if w.Code != 200 {
		t.Fatalf("asc: %d", w.Code)
	}
	rows = nil
	if err := decodePageItems(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if got := rows[i]["id"]; got != want[len(want)-1-i] {
			t.Fatalf("asc order[%d]=%v, want %s", i, got, want[len(want)-1-i])
		}
	}

	// An unknown sort still falls back to the default order.
	if w := h.do("GET", "/api/runs?sort=bogus&order=desc", nil); w.Code != 200 {
		t.Fatalf("bogus sort: %d", w.Code)
	}
}

// TestRunDetailCurrentNode: GET /runs/:id carries the same currentNode
// {id,name,iteration,kind,mode,state} and lastActivityAt as the run list, and
// the takeover conversation's opening turn exposes the auto-phase timeline.
func TestRunDetailCurrentNode(t *testing.T) {
	h := newHarness(t)
	h.h.Eng.Halt()
	g := takeoverHandlerGraph()
	now := time.Now()
	h.db.Create(&models.Run{ID: "d-auto", Status: "running", WorkflowID: "wf", Graph: g, StartedAt: now, LastActivityAt: &now})
	h.db.Create(&models.StateRun{RunID: "d-auto", NodeID: "impl", Iteration: 1, Status: "running"})
	h.db.Create(&models.Run{ID: "d-manual", Status: "waiting_human", WorkflowID: "wf", Graph: g, StartedAt: now})
	h.db.Create(&models.StateRun{RunID: "d-manual", NodeID: "impl", Iteration: 2, Status: "waiting_human", Mode: "manual", TakenOverAt: &now})
	h.db.Create(&models.ReactConversation{RunID: "d-manual", NodeID: "impl", Iteration: 2, Takeover: true,
		Messages: []models.ReactMessage{{Role: "agent", Text: "已中断自动执行", At: now.Format(time.RFC3339),
			Events: []models.AcpEvent{{Kind: "thought", Text: "t"}, {Kind: "tool_call", Title: "edit"}}}}})
	h.db.Create(&models.Run{ID: "d-done", Status: "completed", WorkflowID: "wf", Graph: g, StartedAt: now})

	get := func(id string) map[string]any {
		w := h.do("GET", "/api/runs/"+id, nil)
		if w.Code != 200 {
			t.Fatalf("get %s: %d %s", id, w.Code, w.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := get("d-auto")
	cn, _ := d["currentNode"].(map[string]any)
	if cn["id"] != "impl" || cn["name"] != "实现" || cn["iteration"] != float64(1) || cn["kind"] != "agent" || cn["mode"] != "auto" || cn["state"] != "running" {
		t.Fatalf("auto detail currentNode: %v", cn)
	}
	if ts, _ := d["lastActivityAt"].(string); ts == "" {
		t.Fatal("detail must carry lastActivityAt")
	}
	d = get("d-manual")
	cn, _ = d["currentNode"].(map[string]any)
	if cn["kind"] != "takeover" || cn["mode"] != "manual" || cn["state"] != "waiting_human" || cn["iteration"] != float64(2) {
		t.Fatalf("manual detail currentNode: %v", cn)
	}
	byNode, _ := d["clarifyByNode"].(map[string]any)
	conv, _ := byNode["impl"].(map[string]any)
	if conv["takeover"] != true {
		t.Fatalf("clarifyByNode.impl.takeover: %v", conv)
	}
	turns, _ := conv["turns"].([]any)
	if len(turns) != 1 {
		t.Fatalf("turns: %v", conv["turns"])
	}
	evs, _ := turns[0].(map[string]any)["events"].([]any)
	if len(evs) != 2 || evs[1].(map[string]any)["kind"] != "tool_call" {
		t.Fatalf("opening turn events: %v", turns[0])
	}
	if _, ok := get("d-done")["currentNode"]; ok {
		t.Fatal("terminal run carries no currentNode")
	}
}
