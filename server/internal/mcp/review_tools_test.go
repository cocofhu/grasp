package mcp

import (
	"encoding/json"
	"strings"
	"testing"
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

func TestReviewPhaseWidensToolset(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "impl", "implement")

	if txt, isErr := reviewCall(t, h, tok, "set_plan", reviewPlanArgs); !isErr || !strings.Contains(txt, "复审阶段") {
		t.Fatalf("implement outside review must not set_plan: %q", txt)
	}
	if txt, isErr := reviewCall(t, h, tok, "set_preview", map[string]any{"url": "https://example.com"}); !isErr {
		t.Fatalf("implement outside review must not set_preview: %q", txt)
	}

	h.SetActiveReview("r1", true)
	if txt, isErr := reviewCall(t, h, tok, "set_plan", reviewPlanArgs); isErr {
		t.Fatalf("implement in review should set_plan: %q", txt)
	}
	if txt, isErr := reviewCall(t, h, tok, "set_preview", map[string]any{"url": "https://example.com"}); isErr {
		t.Fatalf("implement in review should set_preview: %q", txt)
	}
	if got := h.ListPreviewPorts("r1", "impl"); len(got) != 1 || got[0].URL != "https://example.com" {
		t.Fatalf("preview not registered on the review node: %+v", got)
	}
	for _, tool := range []string{"set_preflight", "ask_form", "set_root_cause", "set_test_result"} {
		if txt, isErr := reviewCall(t, h, tok, tool, map[string]any{}); !isErr || strings.HasPrefix(txt, "ok") {
			t.Errorf("%s must stay closed in review: %q", tool, txt)
		}
	}
}

func TestReviewPhaseNotForOtherNodeTypes(t *testing.T) {
	h := NewHost(&memStore{})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "t1", "test")
	h.SetActiveReview("r1", true)
	if txt, isErr := reviewCall(t, h, tok, "set_plan", reviewPlanArgs); !isErr {
		t.Fatalf("test node is not a review agent: %q", txt)
	}
}

func TestReviewCrossNodeWriteKeepsOwner(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	tok := h.RegisterRun("r1")
	if _, err := store.Save("r1", "plan_1", PlanArtifactName, "json", `{"goals":[]}`); err != nil {
		t.Fatal(err)
	}
	h.SetActiveNode("r1", "impl", "implement")
	h.SetActiveReview("r1", true)

	if txt, isErr := reviewCall(t, h, tok, "set_plan", reviewPlanArgs); isErr {
		t.Fatalf("set_plan: %q", txt)
	}
	if owner := store.node["r1|"+PlanArtifactName]; owner != "plan_1" {
		t.Fatalf("plan.json owner=%q, want plan_1", owner)
	}
	if body, _ := store.Get("r1", PlanArtifactName); !strings.Contains(body, "统一控件高度") {
		t.Fatalf("plan content not rewritten: %s", body)
	}

	if txt, isErr := reviewCall(t, h, tok, "set_research", map[string]any{
		"summary": "s", "findings": []any{map[string]any{"title": "f", "detail": "d"}},
	}); isErr {
		t.Fatalf("set_research: %q", txt)
	}
	if owner := store.node["r1|research.json"]; owner != "impl" {
		t.Fatalf("new cross-node product owner=%q, want the review node", owner)
	}

	if _, err := store.Save("r1", "visual_1", "page.html", "html", "<p>old</p>"); err != nil {
		t.Fatal(err)
	}
	if txt, isErr := reviewCall(t, h, tok, "write_artifact", map[string]any{"name": "page.html", "content": "<p>new</p>", "kind": "html"}); isErr {
		t.Fatalf("write_artifact: %q", txt)
	}
	if owner := store.node["r1|page.html"]; owner != "visual_1" {
		t.Fatalf("page.html owner=%q, want visual_1", owner)
	}
}

func TestOwnProductWriteStaysOnActiveNode(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	tok := h.RegisterRun("r1")
	if _, err := store.Save("r1", "plan_old", PlanArtifactName, "json", `{"goals":[]}`); err != nil {
		t.Fatal(err)
	}
	h.SetActiveNode("r1", "plan_new", "plan")
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
