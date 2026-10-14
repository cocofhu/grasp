package mcp

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestNodeCompleteRequiresHandoffWithDownstream(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r-ho")
	h.SetActiveNode("r-ho", "impl", capsImplement)
	h.SetActiveDownstream("r-ho", "impl", []models.HandoffTarget{{NodeID: "test", Label: "测试评审", Outlet: "success"}})

	for _, status := range []string{"success", "failed"} {
		msg, isErr := h.runTool("r-ho", tok, "node_complete", map[string]any{"status": status, "summary": "x"})
		if !isErr || !strings.Contains(msg, "handoff") || !strings.Contains(msg, "测试评审") {
			t.Fatalf("%s without handoff: %q err=%v", status, msg, isErr)
		}
	}
	if h.HasOutcome("r-ho", "impl") {
		t.Fatal("a rejected call must not leave a mark")
	}

	long := strings.Repeat("长", models.HandoffMaxRunes+1)
	if msg, isErr := h.runTool("r-ho", tok, "node_complete", map[string]any{"status": "success", "handoff": long}); !isErr {
		t.Fatalf("over-long handoff accepted: %q", msg)
	}

	msg, isErr := h.runTool("r-ho", tok, "node_complete", map[string]any{
		"status": "failed", "error": "build broke", "handoff": "构建在 web 包失败,分支 feat/x 已推送,下游请先看 build.log",
	})
	if isErr {
		t.Fatalf("failed with handoff: %s", msg)
	}
	o, _ := h.TakeOutcome("r-ho", "impl")
	out := MergeOutcomeOutputs(nil, o)
	if out[models.OutputHandoff] == "" || out[models.OutputHandoffSource] != models.HandoffSourceAgent {
		t.Fatalf("merged outputs = %+v", out)
	}

	h.UnregisterRun("r-ho")
	if h.ActiveDownstream("r-ho", "impl") != nil {
		t.Fatal("UnregisterRun must drop downstream bookkeeping")
	}
}

func TestNodeCompleteWithoutDownstreamNeedsNoHandoff(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r-leaf")
	h.SetActiveNode("r-leaf", "last", capsImplement)
	if msg, isErr := h.runTool("r-leaf", tok, "node_complete", map[string]any{"status": "success"}); isErr {
		t.Fatalf("leaf node_complete: %s", msg)
	}
}
