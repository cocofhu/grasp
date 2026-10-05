package mcp

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// fakeHistory is an in-memory HistoryProvider for the run-history tools.
type fakeHistory struct {
	states   []models.StateRun
	run      models.Run
	feedback []models.FeedbackEvent
}

func (f *fakeHistory) States(runID string) []models.StateRun { return f.states }
func (f *fakeHistory) Get(runID string) (models.Run, bool)   { return f.run, true }
func (f *fakeHistory) FeedbackEvents(runID string) []models.FeedbackEvent {
	return f.feedback
}

// twoGateRun builds a pipeline: input → design → gate1(revise→design) →
// code → gate2(revise→code) → output, with one resolved feedback per gate.
func twoGateRun() *fakeHistory {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "design", Type: "agent", Label: "视觉设计"},
			{ID: "gate1", Type: "human_gate", Label: "设计门禁", Config: map[string]any{"title": "确认设计"}},
			{ID: "code", Type: "agent", Label: "编码"},
			{ID: "gate2", Type: "human_gate", Label: "评审门禁", Config: map[string]any{"title": "确认代码"}},
			{ID: "output", Type: "output", Label: "输出"},
		},
		Edges: []models.Edge{
			{Source: "input", Target: "design"},
			{Source: "design", Target: "gate1"},
			{Source: "gate1", Target: "design"}, // revise loop-back
			{Source: "gate1", Target: "code"},   // approve
			{Source: "code", Target: "gate2"},
			{Source: "gate2", Target: "code"},   // revise loop-back
			{Source: "gate2", Target: "output"}, // approve
		},
	}
	states := []models.StateRun{
		{RunID: "r", NodeID: "design", NodeType: "agent", Iteration: 1, Status: "completed", OutputMd: "初版设计"},
		{RunID: "r", NodeID: "gate1", NodeType: "human_gate", Iteration: 1, Status: "completed",
			Outputs: map[string]any{"action": "revise", "form": map[string]any{"comment": "改用直角"}}},
		{RunID: "r", NodeID: "design", NodeType: "agent", Iteration: 2, Status: "completed", OutputMd: "第二版"},
		{RunID: "r", NodeID: "code", NodeType: "agent", Iteration: 1, Status: "completed", OutputMd: "写代码"},
		{RunID: "r", NodeID: "gate2", NodeType: "human_gate", Iteration: 1, Status: "completed",
			Outputs: map[string]any{"action": "revise", "form": map[string]any{"comment": "补充测试"}}},
	}
	return &fakeHistory{states: states, run: models.Run{ID: "r", Graph: g}}
}

func TestRunHistoryIncludesArtifactEdit(t *testing.T) {
	fh := twoGateRun()
	fh.run.Trace = []models.TraceEntry{
		{NodeID: "gate1", Event: "artifact_edit", Detail: "人改产物 name=research.json kind=json size=12 reviewer=operator"},
	}
	h := NewHost(&memStore{})
	h.SetHistoryProvider(fh)
	out, err := h.RunHistory("r", "design", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "人改产物") || !strings.Contains(out, "research.json") {
		t.Fatalf("expected artifact_edit in history, got:\n%s", out)
	}
	// Scoped to unrelated stage should not include gate1 edits.
	outCode, _ := h.RunHistory("r", "code", false, false)
	if strings.Contains(outCode, "research.json") {
		t.Fatalf("code scope must not include gate1 artifact_edit, got:\n%s", outCode)
	}
}

func TestRunHistoryScopeAndIsolation(t *testing.T) {
	h := NewHost(&memStore{})
	h.SetHistoryProvider(twoGateRun())

	// Scoped to the design stage: sees its own executions + gate1 feedback
	// ("改用直角"), but NOT gate2's feedback (a different stage → no confusion).
	out, err := h.RunHistory("r", "design", false, false)
	if err != nil {
		t.Fatalf("RunHistory: %v", err)
	}
	if !strings.Contains(out, "改用直角") {
		t.Fatalf("design scope should include gate1 feedback, got:\n%s", out)
	}
	if strings.Contains(out, "补充测试") {
		t.Fatalf("design scope must NOT leak gate2 feedback, got:\n%s", out)
	}
	if !strings.Contains(out, "确认设计") || !strings.Contains(out, "审阶段") {
		t.Fatalf("feedback line should label the gate + reviewed stage, got:\n%s", out)
	}

	// Scoped to the code stage: sees gate2 feedback, not gate1's.
	outC, _ := h.RunHistory("r", "code", false, false)
	if !strings.Contains(outC, "补充测试") || strings.Contains(outC, "改用直角") {
		t.Fatalf("code scope isolation wrong, got:\n%s", outC)
	}

	// all=true drops the scope: both feedbacks present.
	outAll, _ := h.RunHistory("r", "design", true, false)
	if !strings.Contains(outAll, "改用直角") || !strings.Contains(outAll, "补充测试") {
		t.Fatalf("all=true should include every feedback, got:\n%s", outAll)
	}

	// only_feedback=true: no plain node executions.
	outFb, _ := h.RunHistory("r", "design", true, true)
	if strings.Contains(outFb, "初版设计") {
		t.Fatalf("only_feedback should exclude node executions, got:\n%s", outFb)
	}
}

func TestExecutionDetail(t *testing.T) {
	h := NewHost(&memStore{})
	h.SetHistoryProvider(twoGateRun())

	// Gate detail surfaces the human decision + form verbatim.
	d, err := h.ExecutionDetail("r", "gate1", 1, false)
	if err != nil {
		t.Fatalf("ExecutionDetail: %v", err)
	}
	if !strings.Contains(d, "改用直角") || !strings.Contains(d, "确认设计") {
		t.Fatalf("gate detail missing feedback/title, got:\n%s", d)
	}

	// Node detail (latest iteration) shows the output summary.
	dn, _ := h.ExecutionDetail("r", "design", 0, false)
	if !strings.Contains(dn, "第二版") {
		t.Fatalf("latest design detail should be v2, got:\n%s", dn)
	}

	// Unknown node errors.
	if _, err := h.ExecutionDetail("r", "nope", 0, false); err == nil {
		t.Fatalf("expected error for unknown node")
	}
}

// TestMcpCallTrace verifies built-in tool calls are recorded (name + truncated
// in/out) against the active node and drained destructively.
func TestMcpCallTrace(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-trace"
	tok := h.RegisterRun(runID)
	h.SetActiveNode(runID, "n1", capsPlain)

	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"page.html","content":"<h1>hi</h1>"}}}`)
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_artifact","arguments":{"name":"page.html"}}}`)

	calls := h.TakeMcpCalls(runID, "n1")
	if len(calls) != 2 {
		t.Fatalf("expected 2 recorded calls, got %d (%+v)", len(calls), calls)
	}
	if calls[0].Tool != "write_artifact" || calls[0].IsError {
		t.Fatalf("first call wrong: %+v", calls[0])
	}
	if !strings.Contains(calls[0].Args, "page.html") {
		t.Fatalf("args not captured: %+v", calls[0])
	}
	if calls[1].Tool != "read_artifact" || calls[1].Result == "" {
		t.Fatalf("second call result not captured: %+v", calls[1])
	}
	// Draining is destructive.
	if len(h.TakeMcpCalls(runID, "n1")) != 0 {
		t.Fatalf("calls should be cleared after take")
	}

	// A failed call is flagged IsError.
	h.SetActiveNode(runID, "n2", capsPlain)
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_artifact","arguments":{"name":"missing.md"}}}`)
	c2 := h.TakeMcpCalls(runID, "n2")
	if len(c2) != 1 || !c2[0].IsError {
		t.Fatalf("failed call should be flagged IsError: %+v", c2)
	}
}

// TestPeekMcpCallsAndHostHooks covers PeekMcpCalls (non-destructive) and the
// rarely-wired host setters that previously sat at 0% in the coverage gate.
func TestPeekMcpCallsAndHostHooks(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-peek"
	tok := h.RegisterRun(runID)
	h.SetActiveNode(runID, "n1", capsPlain)

	if peek := h.PeekMcpCalls(runID, "n1"); peek != nil {
		t.Fatalf("empty peek should be nil, got %+v", peek)
	}
	if peek := h.PeekMcpCalls("missing", "n1"); peek != nil {
		t.Fatalf("missing run peek should be nil, got %+v", peek)
	}

	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"note.md","content":"hi"}}}`)
	peeked := h.PeekMcpCalls(runID, "n1")
	if len(peeked) != 1 || peeked[0].Tool != "write_artifact" {
		t.Fatalf("peek expected 1 write_artifact, got %+v", peeked)
	}
	// Peek must not drain.
	if again := h.PeekMcpCalls(runID, "n1"); len(again) != 1 {
		t.Fatalf("peek should be non-destructive, got %+v", again)
	}
	if taken := h.TakeMcpCalls(runID, "n1"); len(taken) != 1 {
		t.Fatalf("take after peek: %+v", taken)
	}

	called := false
	h.SetAfterWriteArtifact(func(runID, nodeID, name, content, kind string) { called = true })
	h.SetProjectAuditHook(func(runID, nodeID, tool string, args map[string]any, resultText string, isError bool) {})
	h.SetArtifactPreviewHook(func(runID, nodeID, name string) error { return nil })
	if h.afterWrite == nil || h.projectAudit == nil || h.artifactPreview == nil {
		t.Fatal("host hooks should be wired")
	}
	_ = called
}

func TestTrunc(t *testing.T) {
	if got := trunc("abc", 10); got != "abc" {
		t.Fatalf("short string should be unchanged, got %q", got)
	}
	long := strings.Repeat("x", 50)
	got := trunc(long, 10)
	if !strings.HasPrefix(got, strings.Repeat("x", 10)) || !strings.Contains(got, "共50字") {
		t.Fatalf("trunc marker wrong: %q", got)
	}
}
