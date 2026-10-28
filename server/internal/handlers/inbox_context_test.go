package handlers_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRunInboxContextGate(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "ic-gate", Status: "waiting_human", StartedAt: now, Graph: models.Graph{
		Nodes: []models.Node{
			{ID: "visual", Type: "agent", Caps: testReviewCaps, Config: map[string]any{}},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"body_template": "preview {{nodes.visual.outputs.page}}",
			}},
		},
	}})
	h.db.Create(&models.Gate{RunID: "ic-gate", NodeID: "gate", Iteration: 1, Resolved: false, RequestedAt: now})
	h.db.Create(&models.StateRun{
		RunID: "ic-gate", NodeID: "gate", Iteration: 1, Status: "waiting_human",
	})
	h.db.Create(&models.StateRun{
		RunID: "ic-gate", NodeID: "visual", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"page": "<html>v1</html>"},
		Events:  []models.AcpEvent{{Kind: "message", Text: "should-not-appear"}},
	})
	h.db.Create(&models.Artifact{
		ID: "ic-art", RunID: "ic-gate", NodeID: "visual", Name: "page.html", Kind: "html",
		Content: strings.Repeat("x", 5000), SizeBytes: 5000,
	})

	w := h.do("GET", "/api/runs/ic-gate/nodes/gate/context?iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("gate inbox-context: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{`"type":"gate"`, `"nodes"`, `"artifacts"`, `"nodeExecutions"`, "page.html", `\u003chtml\u003ev1\u003c/html\u003e`} {
		if !strings.Contains(body, want) {
			t.Errorf("gate response missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, strings.Repeat("x", 100)) {
		t.Error("gate inbox-context must not inline artifact content")
	}
	if strings.Contains(body, "should-not-appear") {
		t.Error("nodeExecutions must be slim (no events)")
	}
	// Idle (no in-memory hot session): reactSessions omitted; gate DTO present.
	if !strings.Contains(body, `"gate"`) {
		t.Error("gate inbox-context should include gate DTO subset")
	}
	if strings.Contains(body, `"reactSessions"`) {
		t.Error("idle gate inbox-context should omit empty reactSessions")
	}
}

func TestRunInboxContextClarify(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "ic-clarify", Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{
			Nodes: []models.Node{
				{ID: "research", Type: "agent", Caps: testReviewCaps, Label: "调研"},
				{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "需求澄清", Config: map[string]any{
					"prompt": "upstream {{nodes.research.outputs.research}}",
				}},
			},
			Edges: []models.Edge{{ID: "e-react-out", Source: "react", Target: "research"}},
		},
	})
	h.db.Create(&models.ReactConversation{
		RunID: "ic-clarify", NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "question?", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{RunID: "ic-clarify", NodeID: "react", Iteration: 1, Status: "waiting_human"})
	h.db.Create(&models.StateRun{
		RunID: "ic-clarify", NodeID: "research", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"research": `{"summary":"ok"}`},
		Events:  []models.AcpEvent{{Kind: "message", Text: "should-not-appear"}},
	})
	h.db.Create(&models.Artifact{
		ID: "ic-clarify-art", RunID: "ic-clarify", NodeID: "research", Name: "research.json", Kind: "json",
		Content: strings.Repeat("x", 5000), SizeBytes: 5000,
	})

	w := h.do("GET", "/api/runs/ic-clarify/nodes/react/context?iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("clarify inbox-context: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"type":"clarify"`, `"status"`, `"clarify"`, "question?", "需求澄清",
		`"nodes"`, `"edges"`, "e-react-out", `"artifacts"`, `"nodeExecutions"`, "research.json",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("clarify response missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, strings.Repeat("x", 100)) {
		t.Error("clarify inbox-context must not inline artifact content")
	}
	if strings.Contains(body, "should-not-appear") {
		t.Error("nodeExecutions must be slim (no events)")
	}
	// Idle (no in-memory session): reactSessions omitted — presence when busy is
	// covered by TestReactSessionsDTO + GatesInbox hard-load restore vitest.
	if strings.Contains(body, `"reactSessions"`) {
		t.Error("idle clarify inbox-context should omit empty reactSessions")
	}
}

func TestRunInboxContextNodeStatuses(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{
		ID: "ic-status", Status: "waiting_human", StartedAt: time.Now(),
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "开始"},
			{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "需求澄清"},
			{ID: "impl", Type: "agent", Caps: testReviewCaps, Label: "实现"},
		}},
	})
	h.db.Create(&models.ReactConversation{RunID: "ic-status", NodeID: "react", Iteration: 2,
		Messages: []models.ReactMessage{{Role: "agent", Text: "q", At: time.Now().Format(time.RFC3339)}}})
	h.db.Create(&models.StateRun{RunID: "ic-status", NodeID: "in", Iteration: 1, Status: "completed"})
	h.db.Create(&models.StateRun{RunID: "ic-status", NodeID: "react", Iteration: 1, Status: "completed"})
	h.db.Create(&models.StateRun{RunID: "ic-status", NodeID: "react", Iteration: 2, Status: "waiting_human"})

	w := h.do("GET", "/api/runs/ic-status/nodes/react/context?iteration=2", nil)
	if w.Code != 200 {
		t.Fatalf("inbox-context: %d %s", w.Code, w.Body)
	}
	var out struct {
		NodeRuns map[string]struct {
			Iteration int    `json:"iteration"`
			Status    string `json:"status"`
		} `json:"nodeRuns"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.NodeRuns["in"]; got.Status != "completed" {
		t.Errorf("input node: %+v", got)
	}
	if got := out.NodeRuns["react"]; got.Status != "waiting_human" || got.Iteration != 2 {
		t.Errorf("latest visit wins: %+v", got)
	}
	if _, ok := out.NodeRuns["impl"]; ok {
		t.Error("a node never entered has no status")
	}
}

func TestRunInboxContextClarifyResearchReview(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "ic-research", Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "research_1", Type: "agent", Caps: testReviewCaps, Label: "调研结论"},
		}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: "ic-research", NodeID: "research_1", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "请复审调研", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{
		RunID: "ic-research", NodeID: "research_1", Iteration: 1, Status: "waiting_human",
		Outputs: map[string]any{"research": `{"summary":"root cause"}`},
	})
	h.db.Create(&models.Artifact{
		ID: "ic-res-art", RunID: "ic-research", NodeID: "research_1", Name: "research.json", Kind: "json",
		Content: `{"summary":"root cause"}`, SizeBytes: 24,
	})

	w := h.do("GET", "/api/runs/ic-research/nodes/research_1/context?iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("research clarify inbox-context: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"type":"clarify"`, `"nodes"`, `"artifacts"`, `"nodeExecutions"`,
		"research_1", "research.json", "root cause",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("research clarify response missing %q in %s", want, body)
		}
	}
}

// The inbox product panel reads each execution's *_json. Stripping those
// fields made a later visit look empty even when that visit had written.
func TestRunInboxContextKeepsPerExecutionJSON(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "ic-iter-json", Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "test", Type: "agent", Caps: testReviewCaps, Label: "测试"},
		}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: "ic-iter-json", NodeID: "test", Iteration: 2, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "请看这次", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{
		RunID: "ic-iter-json", NodeID: "test", Iteration: 1, Status: "failed",
		Outputs: map[string]any{
			"test_result":      "md-1",
			"test_result_json": `{"summary":"ITER1"}`,
			"review_json":      `{"verdict":"reject"}`,
		},
		Events: []models.AcpEvent{{Kind: "message", Text: "should-not-appear"}},
	})
	h.db.Create(&models.StateRun{
		RunID: "ic-iter-json", NodeID: "test", Iteration: 2, Status: "waiting_human",
		Outputs: map[string]any{
			"test_result_json": `{"summary":"ITER2"}`,
		},
	})
	h.db.Create(&models.Artifact{
		ID: "ic-tr", RunID: "ic-iter-json", NodeID: "test", Name: "test_result.json", Kind: "json",
		Content: `{"summary":"LIVE"}`, SizeBytes: 18,
	})

	w := h.do("GET", "/api/runs/ic-iter-json/nodes/test/context?iteration=2", nil)
	if w.Code != 200 {
		t.Fatalf("inbox-context: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if strings.Contains(body, "should-not-appear") {
		t.Fatal("nodeExecutions must stay slim (no events)")
	}
	if strings.Contains(body, "LIVE") {
		t.Fatal("inbox-context must not inline the live artifact body")
	}
	var payload struct {
		Type           string `json:"type"`
		NodeExecutions map[string][]struct {
			Iteration int            `json:"iteration"`
			Outputs   map[string]any `json:"outputs"`
		} `json:"nodeExecutions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != "clarify" {
		t.Fatalf("type=%s", payload.Type)
	}
	execs := payload.NodeExecutions["test"]
	if len(execs) != 2 {
		t.Fatalf("executions=%d", len(execs))
	}
	if execs[0].Outputs["test_result_json"] != `{"summary":"ITER1"}` || execs[0].Outputs["review_json"] != `{"verdict":"reject"}` {
		t.Fatalf("iteration 1 snapshots: %+v", execs[0].Outputs)
	}
	if execs[1].Outputs["test_result_json"] != `{"summary":"ITER2"}` {
		t.Fatalf("iteration 2 snapshot: %+v", execs[1].Outputs)
	}
	if _, ok := execs[1].Outputs["review_json"]; ok {
		t.Fatal("iteration 2 must not carry iteration 1 review_json")
	}
}

// A booting approve node has no conversation row yet, but its loading card is
// already listed, so the context must resolve with an empty starting transcript.
func TestRunInboxContextStartingClarify(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "ic-starting", Status: "running", StartedAt: now,
		Graph:        models.Graph{Nodes: []models.Node{{ID: "ap", Type: "agent", Caps: testClarifyCaps, Label: "开发前澄清"}}},
		FirstMessage: &models.CompositeText{Text: "  帮我做导出  "},
	})
	h.db.Create(&models.StateRun{
		RunID: "ic-starting", NodeID: "ap", NodeType: "agent", Iteration: 1, Status: "running",
	})

	w := h.do("GET", "/api/runs/ic-starting/nodes/ap/context?iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("starting inbox-context: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"type":"clarify"`, `"starting":true`, `"turns":[]`, `"done":false`, "开发前澄清",
		`"firstMessage":"帮我做导出"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("starting response missing %q in %s", want, body)
		}
	}
}

func TestRunInboxContextNotFound(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "ic-none", Status: "waiting_human", StartedAt: time.Now(), Graph: models.Graph{}})

	w := h.do("GET", "/api/runs/ic-none/nodes/x/context?iteration=1", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pending":false`) {
		t.Fatalf("no pending should leave: %d %s", w.Code, w.Body.String())
	}
	if w := h.do("GET", "/api/runs/missing-run/nodes/x/context?iteration=1", nil); w.Code != 404 {
		t.Fatalf("missing run: %d %s", w.Code, w.Body.String())
	}
	if w := h.do("GET", "/api/runs/ic-none/nodes/x/context", nil); w.Code != 400 {
		t.Fatalf("missing iteration: %d", w.Code)
	}
	if w := h.do("GET", "/api/runs/ic-none/inbox-context?iteration=1", nil); w.Code != 404 {
		t.Fatalf("retired inbox-context: %d", w.Code)
	}
	if w := h.do("GET", "/api/runs/ic-none/nodes/x/context?iteration=0", nil); w.Code != 400 {
		t.Fatalf("iteration 0: %d", w.Code)
	}
	if w := h.do("GET", "/api/runs/ic-none/nodes/x/context?iteration=abc", nil); w.Code != 400 {
		t.Fatalf("bad iteration: %d", w.Code)
	}
}

func TestRunInboxContextIterationMismatch(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "ic-iter", Status: "waiting_human", StartedAt: now, Graph: models.Graph{
		Nodes: []models.Node{{ID: "gate", Type: "human_gate"}},
	}})
	h.db.Create(&models.Gate{RunID: "ic-iter", NodeID: "gate", Iteration: 2, Resolved: false, RequestedAt: now})

	w := h.do("GET", "/api/runs/ic-iter/nodes/gate/context?iteration=1", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pending":false`) {
		t.Fatalf("iteration mismatch should leave: %d %s", w.Code, w.Body.String())
	}
}

func TestRunInboxContextClarifyCarriesVisitModel(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "ic-model", Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "需求澄清"}}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: "ic-model", NodeID: "react", Iteration: 1, Done: false,
		CurrentModelSet: true, CurrentConfigurationID: "group-kimi", CurrentModelID: "kimi-k2.5", CurrentReasoningEffort: "high",
		Messages: []models.ReactMessage{{Role: "agent", Text: "q", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{RunID: "ic-model", NodeID: "react", Iteration: 1, Status: "waiting_human"})

	w := h.do("GET", "/api/runs/ic-model/nodes/react/context?iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("clarify inbox-context: %d %s", w.Code, w.Body)
	}
	var out struct {
		Clarify struct {
			CurrentModelSet        bool   `json:"currentModelSet"`
			CurrentModelID         string `json:"currentModelId"`
			CurrentConfigurationID string `json:"currentConfigurationId"`
			ReasoningEffort        string `json:"reasoningEffort"`
		} `json:"clarify"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Clarify.CurrentModelSet || out.Clarify.CurrentModelID != "kimi-k2.5" || out.Clarify.CurrentConfigurationID != "group-kimi" || out.Clarify.ReasoningEffort != "high" {
		t.Fatalf("visit model = %+v", out.Clarify)
	}
}
