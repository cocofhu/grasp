package handlers_test

import (
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

	w := h.do("GET", "/api/runs/ic-gate/inbox-context?nodeId=gate&iteration=1", nil)
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
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "research", Type: "agent", Caps: testReviewCaps, Label: "调研"},
			{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "需求澄清", Config: map[string]any{
				"prompt": "upstream {{nodes.research.outputs.research}}",
			}},
		}},
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

	w := h.do("GET", "/api/runs/ic-clarify/inbox-context?nodeId=react&iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("clarify inbox-context: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"type":"clarify"`, `"status"`, `"clarify"`, "question?", "需求澄清",
		`"nodes"`, `"artifacts"`, `"nodeExecutions"`, "research.json",
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

	w := h.do("GET", "/api/runs/ic-research/inbox-context?nodeId=research_1&iteration=1", nil)
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

// A booting approve node has no conversation row yet, but its loading card is
// already listed, so the context must resolve with an empty starting transcript.
func TestRunInboxContextStartingClarify(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "ic-starting", Status: "running", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "ap", Type: "agent", Caps: testClarifyCaps, Label: "开发前澄清"}}},
	})
	h.db.Create(&models.StateRun{
		RunID: "ic-starting", NodeID: "ap", NodeType: "agent", Iteration: 1, Status: "running",
	})

	w := h.do("GET", "/api/runs/ic-starting/inbox-context?nodeId=ap&iteration=1", nil)
	if w.Code != 200 {
		t.Fatalf("starting inbox-context: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"type":"clarify"`, `"starting":true`, `"turns":[]`, `"done":false`, "开发前澄清",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("starting response missing %q in %s", want, body)
		}
	}
}

func TestRunInboxContextNotFound(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "ic-none", Status: "waiting_human", StartedAt: time.Now(), Graph: models.Graph{}})

	if w := h.do("GET", "/api/runs/ic-none/inbox-context?nodeId=x&iteration=1", nil); w.Code != 404 {
		t.Fatalf("no pending: %d", w.Code)
	}
	if w := h.do("GET", "/api/runs/ic-none/inbox-context?nodeId=x", nil); w.Code != 400 {
		t.Fatalf("missing iteration: %d", w.Code)
	}
	if w := h.do("GET", "/api/runs/ic-none/inbox-context?iteration=1", nil); w.Code != 400 {
		t.Fatalf("missing nodeId: %d", w.Code)
	}
}

func TestRunInboxContextIterationMismatch(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "ic-iter", Status: "waiting_human", StartedAt: now, Graph: models.Graph{
		Nodes: []models.Node{{ID: "gate", Type: "human_gate"}},
	}})
	h.db.Create(&models.Gate{RunID: "ic-iter", NodeID: "gate", Iteration: 2, Resolved: false, RequestedAt: now})

	if w := h.do("GET", "/api/runs/ic-iter/inbox-context?nodeId=gate&iteration=1", nil); w.Code != 404 {
		t.Fatalf("iteration mismatch should 404: %d", w.Code)
	}
}
