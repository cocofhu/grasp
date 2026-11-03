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
	for _, tool := range []string{"ask_question", "ask_form", "node_complete", "list_run_history", "get_history_detail", "page_state", liveUpdateTool} {
		if names[tool] {
			t.Errorf("Solo advertises %s", tool)
		}
		if result, failed := h.runTool(soloRun, token, tool, nil); !failed || result != "unknown tool: "+tool {
			t.Errorf("Solo call %s = %q, failed=%v", tool, result, failed)
		}
	}
	for _, tool := range []string{"get_history", "write_artifact", "set_preview", "set_artifact_preview"} {
		if !names[tool] {
			t.Errorf("Solo omits %s", tool)
		}
	}

	const workflowRun = "run-workflow"
	h.SetActiveNode(workflowRun, "solo", caps)
	names = list(workflowRun)
	if !names["ask_question"] || !names["ask_form"] || !names["list_run_history"] || names["get_history"] {
		t.Fatalf("workflow with node ID solo has the wrong tools: %v", names)
	}
}
