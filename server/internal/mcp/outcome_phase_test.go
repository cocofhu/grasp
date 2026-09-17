package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func toolNamesFromList(t *testing.T, h *Host, runID, tok string) []string {
	t.Helper()
	list := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools, _ := list["result"].(map[string]any)["tools"].([]any)
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		m, _ := raw.(map[string]any)
		if n, _ := m["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	return names
}

func hasTool(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestGraspPhase1HidesNodeComplete locks g2.1/g2.3: before confirm, tools/list
// omits node_complete and tools/call fails as unknown without teaching.
func TestGraspPhase1HidesNodeComplete(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r-grasp")
	h.SetActiveNode("r-grasp", "predev", "approve")

	names := toolNamesFromList(t, h, "r-grasp", tok)
	if hasTool(names, "node_complete") {
		t.Fatal("Phase1 tools/list must omit node_complete")
	}

	msg, isErr := h.runTool("r-grasp", tok, "node_complete", map[string]any{"status": "success"})
	if !isErr || msg != "unknown tool: node_complete" {
		t.Fatalf("Phase1 call want unknown tool, got %q err=%v", msg, isErr)
	}
	if strings.Contains(msg, "确认") || strings.Contains(msg, "流转") {
		t.Fatalf("Phase1 error must not teach confirm flow: %q", msg)
	}
	if h.HasOutcome("r-grasp", "predev") {
		t.Fatal("Phase1 call must not leave an outcome mark")
	}
}

// TestGraspPhase2ExposesNodeComplete locks g2.2: after SetOutcomeAllowed,
// tools/list includes the tool, generation bumps, and call succeeds.
func TestGraspPhase2ExposesNodeComplete(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r-grasp2")
	h.SetActiveNode("r-grasp2", "predev", "grasp")

	gen0 := h.ToolsListGeneration("r-grasp2")
	h.SetOutcomeAllowed("r-grasp2", true)
	if got := h.ToolsListGeneration("r-grasp2"); got <= gen0 {
		t.Fatalf("tools list gen must bump on allow, gen0=%d got=%d", gen0, got)
	}

	names := toolNamesFromList(t, h, "r-grasp2", tok)
	if !hasTool(names, "node_complete") {
		t.Fatal("Phase2 tools/list must include node_complete")
	}

	msg, isErr := h.runTool("r-grasp2", tok, "node_complete", map[string]any{
		"status": "success", "summary": "confirmed",
	})
	if isErr {
		t.Fatalf("Phase2 node_complete: %s", msg)
	}
	if !h.HasOutcome("r-grasp2", "predev") {
		t.Fatal("Phase2 call must record outcome")
	}
}

// TestNonGraspAlwaysListsNodeComplete locks g3.3: implement/research keep the tool.
func TestNonGraspAlwaysListsNodeComplete(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r-impl")
	h.SetActiveNode("r-impl", "n1", "implement")

	names := toolNamesFromList(t, h, "r-impl", tok)
	if !hasTool(names, "node_complete") {
		t.Fatal("non-Grasp tools/list must include node_complete")
	}
	msg, isErr := h.runTool("r-impl", tok, "node_complete", map[string]any{"status": "success"})
	if isErr {
		t.Fatalf("implement node_complete: %s", msg)
	}
}

func TestListedToolsJSONRoundTrip(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r-json")
	h.SetActiveNode("r-json", "n", "approve")
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 9, "method": "tools/list"})
	st, resp := h.ServeRPC("r-json", tok, body)
	if st != 200 {
		t.Fatalf("status=%d resp=%s", st, resp)
	}
	if strings.Contains(string(resp), `"name":"node_complete"`) {
		t.Fatal("Phase1 ServeRPC tools/list must not serialize node_complete")
	}
}
