package services

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSoloReplyTextPreservesChunksAndSeparatesMessageRuns(t *testing.T) {
	var text strings.Builder
	var state soloReplyTextState
	frames := []string{
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Checking "}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"the setup."}}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"one"}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":""}}}`,
		`{"type":"usage","input_tokens":10}`,
		`{"type":"session_update","update":{"session_update":"agentMessageChunk","content":{"text":"Server is up."}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"Check the screenshot"}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"![Homepage](/tmp/full.png)"}}}`,
	}
	for _, frame := range frames {
		appendSoloReplyText(&text, &state, json.RawMessage(frame))
	}
	if got, want := text.String(), "Checking the setup.\n\nServer is up.\n\n![Homepage](/tmp/full.png)"; got != want {
		t.Fatalf("reply=%q want=%q", got, want)
	}
}

func TestSoloReplyTextToolUpdatesDoNotInventMessageBoundaries(t *testing.T) {
	var text strings.Builder
	var state soloReplyTextState
	for _, frame := range []string{
		`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"one"}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Server "}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"one","status":"completed"}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"is up."}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"tool_call_update","toolCall":{"id":"two"},"status":"completed"}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Screenshot ready."}}}`,
	} {
		appendSoloReplyText(&text, &state, json.RawMessage(frame))
	}
	if got, want := text.String(), "Server is up.\n\nScreenshot ready."; got != want {
		t.Fatalf("reply=%q want=%q", got, want)
	}
}
