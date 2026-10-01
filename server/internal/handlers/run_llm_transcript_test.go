package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

type inflightProvider struct{ fakeProvider }

func (inflightProvider) InflightPrompts(runID string) map[string]runtime.InflightPrompt {
	if runID != "run-tr" {
		return nil
	}
	return map[string]runtime.InflightPrompt{"n2": {Prompt: "still asking", At: "2026-10-01T00:00:10Z"}}
}

func (inflightProvider) RecentTurns(runID string) map[string][]models.AcpEvent {
	if runID != "run-tr" {
		return nil
	}
	return map[string][]models.AcpEvent{"n2": {
		{Kind: models.AcpKindPrompt, Text: "first ask", At: "2026-10-01T00:00:06Z"},
		{Kind: "message", Text: "first answer"},
		{Kind: models.AcpKindTurnEnd, At: "2026-10-01T00:00:08Z"},
	}}
}

func TestRunLlmTranscriptOrdersByStartAndReturnsFullPrompts(t *testing.T) {
	h := newHarness(t)
	old := h.h.Eng
	eng := engine.New(h.db, inflightProvider{}, h.host, h.h.Arts, 5)
	h.h.Eng = eng
	t.Cleanup(func() {
		eng.Close()
		h.h.Eng = old
	})

	long := strings.Repeat("问", 400)
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Second)
	h.db.Create(&models.Run{ID: "run-tr", Status: "running", StartedAt: t0})
	// n2 iteration 1 sorts first by iteration but started later than n1 iteration 2.
	h.db.Create(&models.StateRun{RunID: "run-tr", NodeID: "n2", Iteration: 1, Status: "running", StartedAt: &t1})
	h.db.Create(&models.StateRun{RunID: "run-tr", NodeID: "n1", Iteration: 2, Status: "completed", StartedAt: &t0,
		Usage:  &models.TokenUsage{InputTokens: 10, OutputTokens: 2},
		Events: []models.AcpEvent{{Kind: models.AcpKindPrompt, Text: long}, {Kind: "message", Text: "ok"}, {Kind: models.AcpKindTurnEnd}}})

	w := h.do(http.MethodGet, "/api/runs/run-tr/llm-transcript", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Executions []struct {
			NodeID string             `json:"nodeId"`
			Events []models.AcpEvent  `json:"events"`
			Usage  *models.TokenUsage `json:"usage"`
		} `json:"executions"`
		Inflight map[string]runtime.InflightPrompt `json:"inflight"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Executions) != 2 || body.Executions[0].NodeID != "n1" || body.Executions[1].NodeID != "n2" {
		t.Fatalf("want chronological n1,n2; got %+v", body.Executions)
	}
	if got := body.Executions[0].Events[0].Text; got != long {
		t.Fatalf("transcript must return the full prompt, got %d runes", len([]rune(got)))
	}
	if body.Executions[0].Usage == nil || body.Executions[0].Usage.InputTokens != 10 {
		t.Fatalf("usage missing: %+v", body.Executions[0].Usage)
	}
	if body.Inflight["n2"].Prompt != "still asking" {
		t.Fatalf("inflight missing: %+v", body.Inflight)
	}
	if ev := body.Executions[1].Events; len(ev) != 3 || ev[0].Text != "first ask" {
		t.Fatalf("running execution should include unpersisted finished turns: %+v", ev)
	}

	// Run detail and node events only carry a preview.
	for _, path := range []string{"/api/runs/run-tr", "/api/runs/run-tr/nodes/n1/events"} {
		w = h.do(http.MethodGet, path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), long) {
			t.Fatalf("%s leaked the full prompt", path)
		}
		if !strings.Contains(w.Body.String(), `"truncated":true`) {
			t.Fatalf("%s missing truncated flag", path)
		}
	}
}

func TestRunLlmTranscriptNotFound(t *testing.T) {
	h := newHarness(t)
	if w := h.do(http.MethodGet, "/api/runs/nope/llm-transcript", nil); w.Code != http.StatusNotFound {
		t.Fatalf("status %d", w.Code)
	}
}
