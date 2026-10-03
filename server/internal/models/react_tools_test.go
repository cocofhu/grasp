package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolsFromEventsKeepsOrderAndSkipsOtherKinds(t *testing.T) {
	got := ToolsFromEvents([]AcpEvent{
		{Kind: "thought", Text: "hmm"},
		{Kind: "tool_call", Title: "read_file", Status: "completed"},
		{Kind: "tool_call", Title: "", Status: "completed"},
		{Kind: "plan", Title: "plan"},
		{Kind: "tool_call", Title: "Shell", Status: "failed"},
		{Kind: "tool_call", Title: "read_file", Status: "running"},
		{Kind: "message", Text: "done"},
	})
	want := []ReactTool{{"read_file", "completed"}, {"Shell", "failed"}, {"read_file", "running"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool %d = %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestToolsFromEventsNilWithoutToolsAndCapped(t *testing.T) {
	if got := ToolsFromEvents([]AcpEvent{{Kind: "message", Text: "x"}}); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
	var ev []AcpEvent
	for i := 0; i < MaxReactTools+10; i++ {
		ev = append(ev, AcpEvent{Kind: "tool_call", Title: "t"})
	}
	if got := ToolsFromEvents(ev); len(got) != MaxReactTools {
		t.Fatalf("len=%d", len(got))
	}
}

func TestReactMessageToolsJSONOmittedWhenEmpty(t *testing.T) {
	b, _ := json.Marshal(ReactMessage{Role: "agent", Text: "hi"})
	if strings.Contains(string(b), "tools") {
		t.Fatalf("tools must be omitted: %s", b)
	}
	b, _ = json.Marshal(ReactMessage{Role: "agent", Tools: []ReactTool{{Title: "Shell", Status: "completed"}}})
	if !strings.Contains(string(b), `"tools":[{"title":"Shell","status":"completed"}]`) {
		t.Fatalf("tools json: %s", b)
	}
}
