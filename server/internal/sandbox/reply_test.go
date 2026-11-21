package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplyReadsWrappedAndBareFrames(t *testing.T) {
	var p Reply
	for _, raw := range []string{
		`not json`,
		`{"op":"queue_state","busy":true}`,
		`{"op":"event","opId":"g-1","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Plan:"}}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"hm"}}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agentMessageChunk","content":[{"text":"a"},{"text":"b"}]}}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"t","title":"npm test","status":"in_progress"}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"tool_call_update","toolCallId":"t","status":"completed","content":[{"type":"content","content":{"type":"text","text":"42 passed"}},{"type":"content","content":{"type":"image","data":"binary"}}]}}`,
		`{"op":"event","data":{"type":"user_steer","text":"also"}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"done"}}}}`,
		`{"op":"event","data":{"type":"prompt_done","stopReason":"end_turn"}}`,
	} {
		p.Add(json.RawMessage(raw))
	}
	if want := "Plan:\n\nab\n\ndone"; p.Text() != want {
		t.Fatalf("text = %q, want %q", p.Text(), want)
	}
	tools := p.Tools()
	if len(tools) != 1 || tools[0].ID != "t" || tools[0].Title != "npm test" || tools[0].Status != "completed" {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].Output != "42 passed" || strings.Contains(tools[0].Output, "binary") {
		t.Fatalf("tool output = %q", tools[0].Output)
	}
}

func TestDecodeFrame(t *testing.T) {
	if f := decodeFrame(json.RawMessage(`[1]`)); f.Op != "" {
		t.Fatalf("malformed = %+v", f)
	}
	f := decodeFrame(json.RawMessage(`{"op":"cancel_ack","opId":"g-1","status":"removed"}`))
	if f.Op != "cancel_ack" || f.OpID != "g-1" || f.Status != "removed" || f.ev.Type != "" {
		t.Fatalf("control frame = %+v", f)
	}
	f = decodeFrame(json.RawMessage(`{"type":"prompt_begin","opId":"g-2"}`))
	if f.Op != "event" || f.ev.Type != "prompt_begin" || f.ev.OpID != "g-2" || string(f.Data) != `{"type":"prompt_begin","opId":"g-2"}` {
		t.Fatalf("bare event = %+v", f)
	}
}
