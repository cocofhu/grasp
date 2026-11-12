package mcp

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloToolSurfaceExcludesWorkflowActions(t *testing.T) {
	h := NewHost(&memStore{})
	caps := &models.AgentCapabilities{
		Interaction: models.InteractionClarify,
		Tools:       []string{models.ToolAskQuestion, models.ToolAskForm, models.ToolSetPreview, models.ToolSetArtifactPreview},
	}
	h.SetConversationHistoryProvider(&conversationHistoryFake{})
	list := func(runID string) map[string]bool {
		names := map[string]bool{}
		for _, tool := range h.listedTools(runID) {
			names[tool["name"].(string)] = true
		}
		return names
	}

	const soloRun = "solo-session"
	token := h.RegisterRun(soloRun)
	h.SetActiveNode(soloRun, "solo", caps)
	names := list(soloRun)
	for _, tool := range []string{"node_complete", "list_run_history", "get_history_detail", "page_state", liveUpdateTool} {
		if names[tool] {
			t.Errorf("Solo advertises %s", tool)
		}
		if result, failed := h.runTool(soloRun, token, tool, nil); !failed || result != "unknown tool: "+tool {
			t.Errorf("Solo call %s = %q, failed=%v", tool, result, failed)
		}
	}
	for _, tool := range []string{"get_history", "write_artifact", "set_preview", "set_artifact_preview", "ask_question", "ask_form"} {
		if !names[tool] {
			t.Errorf("Solo omits %s", tool)
		}
	}
	question := map[string]any{"questions": []any{map[string]any{"id": "q1", "prompt": "Which?", "options": []any{
		map[string]any{"id": "a", "label": "A"}, map[string]any{"id": "b", "label": "B"}}}}}
	if result, failed := h.runTool(soloRun, token, "ask_question", question); failed {
		t.Fatalf("Solo ask_question failed: %s", result)
	}
	form := map[string]any{"fields": []any{map[string]any{"name": "url", "label": "URL"}}}
	if result, failed := h.runTool(soloRun, token, "ask_form", form); failed {
		t.Fatalf("Solo ask_form failed: %s", result)
	}
	if qs := h.TakePendingQuestions(soloRun, "solo"); len(qs) != 1 || qs[0].Prompt != "Which?" {
		t.Fatalf("Solo pending questions = %+v", qs)
	}
	if forms := h.TakePendingForms(soloRun, "solo"); len(forms) != 1 || len(forms[0].Fields) != 1 {
		t.Fatalf("Solo pending forms = %+v", forms)
	}

	const workflowRun = "run-workflow"
	h.SetActiveNode(workflowRun, "solo", caps)
	names = list(workflowRun)
	if !names["ask_question"] || !names["ask_form"] || !names["list_run_history"] || names["get_history"] {
		t.Fatalf("workflow with node ID solo has the wrong tools: %v", names)
	}
}
