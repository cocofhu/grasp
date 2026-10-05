package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSupportsPreviewAndChanges(t *testing.T) {
	var nilC *Capabilities
	if nilC.supportsPreview() || nilC.supportsChanges() {
		t.Fatal("nil caps should be false")
	}
	c := &Capabilities{}
	if c.supportsPreview() || c.supportsChanges() {
		t.Fatal("empty caps should be false")
	}
	c.Preview.VNC = true
	c.Changes.Endpoint = "/api/changes"
	if !c.supportsPreview() || !c.supportsChanges() {
		t.Fatal("expected supports true")
	}
}

func TestAggregateFrames(t *testing.T) {
	frames := []json.RawMessage{
		json.RawMessage(`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"hello"}}}}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"think"}}}`),
		json.RawMessage(`not-json`),
	}
	ev := AggregateFrames(frames)
	if len(ev) == 0 {
		t.Fatal("expected aggregated events")
	}
}

// TestAggregateLastTurnFrames_MultiTurnSeed covers the hard-refresh bug:
// two turns of message/thought must seed only the last turn's rails.
func TestAggregateLastTurnFrames_MultiTurnSeed(t *testing.T) {
	frames := []json.RawMessage{
		json.RawMessage(`{"type":"prompt_begin","promptText":"q1"}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"think-1"}}}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"answer-1"}}}`),
		json.RawMessage(`{"type":"prompt_begin","promptText":"q2"}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"think-2"}}}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"answer-2-partial"}}}`),
	}

	full := AggregateFrames(frames)
	var fullMsg, fullThought string
	for _, e := range full {
		if e.Kind == "message" {
			fullMsg = e.Text
		}
		if e.Kind == "thought" {
			fullThought = e.Text
		}
	}
	if fullMsg != "answer-1answer-2-partial" || fullThought != "think-1think-2" {
		t.Fatalf("full aggregate sanity: msg=%q thought=%q", fullMsg, fullThought)
	}

	last := AggregateLastTurnFrames(frames)
	var msg, thought string
	for _, e := range last {
		if e.Kind == "message" {
			msg = e.Text
		}
		if e.Kind == "thought" {
			thought = e.Text
		}
	}
	if msg != "answer-2-partial" {
		t.Fatalf("last-turn message = %q, want answer-2-partial (no turn1 stitch)", msg)
	}
	if thought != "think-2" {
		t.Fatalf("last-turn thought = %q, want think-2 (no turn1 stitch)", thought)
	}
	if strings.Contains(msg, "answer-1") || strings.Contains(thought, "think-1") {
		t.Fatal("last-turn seed must not contain first-turn text")
	}
}

func TestFramesAfterLastPromptBegin_WrappedAndBare(t *testing.T) {
	frames := []json.RawMessage{
		json.RawMessage(`{"op":"event","data":{"type":"prompt_begin","promptText":"old"}}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"old-msg"}}}`),
		json.RawMessage(`{"op":"event","data":{"type":"prompt_begin","promptText":"new"}}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"new-msg"}}}`),
	}
	cut := FramesAfterLastPromptBegin(frames)
	if len(cut) != 2 {
		t.Fatalf("want 2 frames after last prompt_begin, got %d", len(cut))
	}
	ev := AggregateFrames(cut)
	var msg string
	for _, e := range ev {
		if e.Kind == "message" {
			msg = e.Text
		}
	}
	if msg != "new-msg" {
		t.Fatalf("msg=%q", msg)
	}
}

func TestSplitFramesByPromptBegin(t *testing.T) {
	setup := json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"available_commands_update"}}`)
	pb := json.RawMessage(`{"op":"event","data":{"type":"prompt_begin"}}`)
	msg := json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"x"}}}`)

	turns := SplitFramesByPromptBegin([]json.RawMessage{setup, pb, msg, pb, msg, msg})
	if len(turns) != 2 || len(turns[0]) != 3 || len(turns[1]) != 3 {
		t.Fatalf("leading frames should stay with turn 1: %v", turns)
	}
	if got := SplitFramesByPromptBegin([]json.RawMessage{msg, msg}); len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("no prompt_begin should be one chunk: %v", got)
	}
	if got := SplitFramesByPromptBegin(nil); len(got) != 0 {
		t.Fatalf("empty log: %v", got)
	}
}

func TestFramesAfterLastPromptBegin_NoBeginKeepsAll(t *testing.T) {
	frames := []json.RawMessage{
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"only"}}}`),
	}
	cut := FramesAfterLastPromptBegin(frames)
	if len(cut) != 1 {
		t.Fatalf("want unchanged, got %d", len(cut))
	}
}
