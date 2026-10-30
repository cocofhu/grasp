package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestToolsFromEventsScopesSnapshotToCurrentReply(t *testing.T) {
	tool := func(title string) AcpEvent { return AcpEvent{Kind: "tool_call", Title: title, Status: "completed"} }
	timeline := AcpEvent{Kind: AcpKindTimeline}
	prompt := AcpEvent{Kind: AcpKindPrompt}
	end := AcpEvent{Kind: AcpKindTurnEnd}
	for _, tc := range []struct {
		name   string
		events []AcpEvent
		want   []ReactTool
	}{
		{"snapshot history", []AcpEvent{tool("old"), timeline, prompt, tool("current"), timeline, end}, []ReactTool{{"current", "completed"}}},
		{"reply without tools", []AcpEvent{tool("old"), timeline, prompt, {Kind: "message", Text: "done"}, timeline, end}, nil},
		{"hidden continuation belongs to same reply", []AcpEvent{tool("old"), timeline, prompt, tool("read"), timeline, end, prompt, tool("write"), timeline, end}, []ReactTool{{"read", "completed"}, {"write", "completed"}}},
		{"silent wrap-up combined with full reply snapshot", []AcpEvent{prompt, tool("git_push"), timeline, end, tool("old_read"), timeline, tool("git_push"), timeline, prompt, tool("write"), timeline, end}, []ReactTool{{"git_push", "completed"}, {"write", "completed"}}},
		{"legacy snapshot timelines", []AcpEvent{tool("old"), timeline, tool("current"), timeline}, []ReactTool{{"current", "completed"}}},
		{"legacy last timeline without tools", []AcpEvent{tool("old"), timeline, {Kind: "message", Text: "done"}, timeline}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]AcpEvent(nil), tc.events...)
			if got := ToolsFromEvents(tc.events); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tools = %+v, want %+v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.events, before) {
				t.Fatal("tool projection changed the full transcript")
			}
		})
	}
	// Historical tools must not use up the current reply's storage cap.
	history := make([]AcpEvent, MaxReactTools)
	for i := range history {
		history[i] = tool("old")
	}
	events := append(history, prompt, tool("current"), end)
	if got := ToolsFromEvents(events); !reflect.DeepEqual(got, []ReactTool{{"current", "completed"}}) {
		t.Fatalf("history exhausted the current reply cap: %+v", got)
	}
}

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

func TestPartsForReplyDoesNotRestoreSnapshotHistoryOnEmptyCurrentReply(t *testing.T) {
	events := []AcpEvent{
		{Kind: "tool_call", Title: "old tool"},
		{Kind: AcpKindTimeline, Parts: []AcpPart{{Kind: "tool", Title: "old tool"}, {Kind: "message", Text: "old answer"}}},
		{Kind: AcpKindPrompt, Text: "current request"},
		{Kind: "message", Text: "current answer without a timeline"},
		{Kind: AcpKindTurnEnd},
	}
	before := append([]AcpEvent(nil), events...)
	if got := PartsForReply(events, "current answer without a timeline"); got != nil {
		t.Fatalf("current reply borrowed old timeline: %+v", got)
	}
	if !reflect.DeepEqual(events, before) {
		t.Fatal("reply projection changed the full transcript")
	}
}

func TestPartsForReplySkipsEmbeddedSnapshotBetweenCurrentPrompts(t *testing.T) {
	events := []AcpEvent{
		{Kind: AcpKindPrompt, Text: "silent wrap-up"},
		{Kind: AcpKindTimeline, Parts: []AcpPart{{Kind: "tool", Title: "git_push"}}},
		{Kind: AcpKindTurnEnd},
		{Kind: AcpKindTimeline, Parts: []AcpPart{{Kind: "tool", Title: "old_read"}, {Kind: "message", Text: "old answer"}}},
		{Kind: AcpKindPrompt, Text: "main reply"},
		{Kind: "message", Text: "done"},
		{Kind: AcpKindTurnEnd},
	}
	before := append([]AcpEvent(nil), events...)
	want := []AcpPart{{Kind: "tool", Title: "git_push"}, {Kind: "message", Text: "done"}}
	if got := PartsForReply(events, "done"); !reflect.DeepEqual(got, want) {
		t.Fatalf("reply borrowed embedded historical timeline: %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(events, before) {
		t.Fatal("reply projection changed the full transcript")
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
