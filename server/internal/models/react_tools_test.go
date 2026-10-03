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

func TestPartsForReplyUsesLastTimeline(t *testing.T) {
	events := []AcpEvent{
		{Kind: AcpKindTimeline, Parts: []AcpPart{{Kind: "message", Text: "old"}}},
		{Kind: "message", Text: "hi there"},
		{Kind: AcpKindTimeline, Parts: []AcpPart{{Kind: "thought", Text: "t"}, {Kind: "tool", Title: "Read"}, {Kind: "message", Text: "hi "}, {Kind: "message", Text: "there"}}},
	}
	got := PartsForReply(events, " hi there\n")
	if len(got) != 4 || got[0].Kind != "thought" || got[3].Text != "there" {
		t.Fatalf("parts kept as streamed: %+v", got)
	}
	if PartsForReply([]AcpEvent{{Kind: "message", Text: "x"}}, "x") != nil {
		t.Fatal("no timeline -> nil")
	}
}

func TestPartsForReplyReplacesCleanedNarration(t *testing.T) {
	events := []AcpEvent{{Kind: AcpKindTimeline, Parts: []AcpPart{
		{Kind: "message", Text: "draft <!--internal-->"}, {Kind: "tool", Title: "Shell"}, {Kind: "message", Text: "done"},
	}}}
	got := PartsForReply(events, "draft done")
	if len(got) != 2 || got[0].Kind != "tool" || got[1] != (AcpPart{Kind: "message", Text: "draft done"}) {
		t.Fatalf("cleaned reply must replace message steps: %+v", got)
	}
	got = PartsForReply(events, "")
	if len(got) != 1 || got[0].Kind != "tool" {
		t.Fatalf("empty reply keeps only non-message steps: %+v", got)
	}
}
