package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func reviewCall(t *testing.T, h *Host, tok, tool string, args map[string]any) (string, bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	return toolText(t, call(t, h, "r1", tok, string(body)))
}

var reviewPlanArgs = map[string]any{"goals": []any{map[string]any{"title": "统一控件高度"}}}

func TestReviewPhaseKeepsDeclaredToolset(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "impl", capsImplement)

	for _, review := range []bool{false, true} {
		h.SetActiveReview("r1", review)
		if txt, isErr := reviewCall(t, h, tok, "set_plan", reviewPlanArgs); !isErr {
			t.Fatalf("review=%v: undeclared set_plan must stay closed: %q", review, txt)
		}
		if txt, isErr := reviewCall(t, h, tok, "set_preview", map[string]any{"url": "https://example.com"}); isErr {
			t.Fatalf("review=%v: granted set_preview should work: %q", review, txt)
		}
		for _, tool := range []string{"set_preflight", "ask_form", "set_root_cause", "set_test_result"} {
			if txt, isErr := reviewCall(t, h, tok, tool, map[string]any{}); !isErr || strings.HasPrefix(txt, "ok") {
				t.Errorf("review=%v: %s must stay closed: %q", review, tool, txt)
			}
		}
	}
	if got := h.ListPreviewPorts("r1", "impl"); len(got) != 1 || got[0].URL != "https://example.com" {
		t.Fatalf("preview not registered on the review node: %+v", got)
	}
}

func TestReviewPhaseNotForOtherNodeTypes(t *testing.T) {
	h := NewHost(&memStore{})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "t1", capsTestReview)
	h.SetActiveReview("r1", true)
	if txt, isErr := reviewCall(t, h, tok, "set_plan", reviewPlanArgs); !isErr {
		t.Fatalf("test node is not a review agent: %q", txt)
	}
}

func TestOwnProductWriteStaysOnActiveNode(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	tok := h.RegisterRun("r1")
	if _, err := store.Save("r1", "plan_old", PlanArtifactName, "json", `{"goals":[]}`); err != nil {
		t.Fatal(err)
	}
	h.SetActiveNode("r1", "plan_new", capsWriting(models.SchemaPlan))
	if txt, isErr := reviewCall(t, h, tok, "set_plan", reviewPlanArgs); isErr {
		t.Fatalf("set_plan: %q", txt)
	}
	if owner := store.node["r1|"+PlanArtifactName]; owner != "plan_new" {
		t.Fatalf("plan node writing its own product must own it, got %q", owner)
	}
}

func TestInReviewPhaseFallsBackToSource(t *testing.T) {
	h := NewHost(&memStore{})
	open := true
	h.SetReviewPhaseSource(func(runID string) bool {
		return runID == "r1" && open
	})
	if !h.InReviewPhase("r1") {
		t.Fatal("fallback should report the open review")
	}
	if h.InReviewPhase("r2") {
		t.Fatal("other run has no review")
	}
	open = false
	if h.InReviewPhase("r1") {
		t.Fatal("a review closed outside the engine must not stay cached")
	}
}
