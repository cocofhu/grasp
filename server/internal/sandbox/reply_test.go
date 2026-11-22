package sandbox

import (
	"encoding/json"
	"fmt"
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

func foldFrames(frames ...string) *Reply {
	var p Reply
	for _, raw := range frames {
		p.Add(json.RawMessage(raw))
	}
	return &p
}

func updateFrame(u string) string {
	return `{"type":"session_update","update":` + u + `}`
}

func TestReplyParagraphFollowsEventOrder(t *testing.T) {
	for name, frames := range map[string][]string{
		"tool updated between texts": {
			updateFrame(`{"sessionUpdate":"tool_call","toolCallId":"a","title":"A","status":"in_progress"}`),
			updateFrame(`{"sessionUpdate":"agent_message_chunk","content":{"text":"Working."}}`),
			updateFrame(`{"sessionUpdate":"tool_call_update","toolCallId":"a","status":"completed"}`),
			updateFrame(`{"sessionUpdate":"agent_message_chunk","content":{"text":"Done."}}`),
		},
		"tool started between texts": {
			updateFrame(`{"sessionUpdate":"agent_message_chunk","content":{"text":"Working."}}`),
			updateFrame(`{"sessionUpdate":"tool_call","toolCallId":"a","title":"A","status":"completed"}`),
			updateFrame(`{"sessionUpdate":"agent_message_chunk","content":{"text":"Done."}}`),
		},
	} {
		p := foldFrames(frames...)
		if p.Text() != "Working.\n\nDone." {
			t.Fatalf("%s: text = %q", name, p.Text())
		}
		var messages []string
		for _, part := range p.r.AcpEvents()[len(p.r.AcpEvents())-1].Parts {
			if part.Kind == "message" {
				messages = append(messages, part.Text)
			}
		}
		if strings.Join(messages, "|") != "Working.|Done." {
			t.Fatalf("%s: timeline messages = %q", name, messages)
		}
	}
}

func TestReplyTextSpansContinuations(t *testing.T) {
	p := foldFrames(
		updateFrame(`{"sessionUpdate":"agent_message_chunk","content":{"text":"first"}}`),
		`{"type":"turn_segment"}`,
		updateFrame(`{"sessionUpdate":"agent_message_chunk","content":{"text":"second"}}`),
	)
	if p.Text() != "first\n\nsecond" {
		t.Fatalf("text = %q", p.Text())
	}
}

func TestReplyKeepsEverySteer(t *testing.T) {
	frames := []string{`{"type":"user_steer","text":"use React"}`}
	for i := range MaxTimelineSteps + 5 {
		frames = append(frames, updateFrame(fmt.Sprintf(`{"sessionUpdate":"tool_call","toolCallId":"t%d","title":"ls","status":"completed"}`, i)))
	}
	frames = append(frames, `{"type":"turn_segment"}`, `{"type":"user_steer","text":"and Vite"}`)
	if got := foldFrames(frames...).Steers(); strings.Join(got, "|") != "use React|and Vite" {
		t.Fatalf("steers = %q", got)
	}
}

func TestReplyReplaysALoggedDiagnostic(t *testing.T) {
	var r ChatResult
	for _, raw := range []string{
		`{"op":"raw","type":"error_text","text":"model quota exhausted"}`,
		`{"type":"prompt_done","stopReason":"failed"}`,
	} {
		dispatchFrame(json.RawMessage(raw), &r)
	}
	if !r.Failed || r.ErrorText != "model quota exhausted" {
		t.Fatalf("failed=%v error=%q", r.Failed, r.ErrorText)
	}
}

func TestReplyToolOutputOmitsMediaBytes(t *testing.T) {
	p := foldFrames(updateFrame(`{"sessionUpdate":"tool_call","toolCallId":"s","title":"screenshot","status":"completed","content":[{"type":"content","content":{"type":"image","mimeType":"image/png","data":"iVBORw0KGgoAAAANSUhEUg"}}]}`))
	out := p.Tools()[0].Output
	if strings.Contains(out, "iVBORw0KGgo") || !strings.Contains(out, "image/png") {
		t.Fatalf("output = %q", out)
	}
}
