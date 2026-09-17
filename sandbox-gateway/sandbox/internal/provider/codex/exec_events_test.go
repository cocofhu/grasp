package codex

import (
	"backend/internal/provider/oneshot"
	"testing"
)

func TestCurrentExecEvents(t *testing.T) {
	c := codec{}
	if got := c.ParseLine([]byte(`{"type":"thread.started","thread_id":"thread-1"}`)); got.SessionID != "thread-1" {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		line string
		kind oneshot.MsgKind
		text string
	}{
		{`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"changed"}}`, oneshot.KindText, "changed"},
		{`{"type":"item.started","item":{"id":"i2","type":"command_execution","command":"go test"}}`, oneshot.KindToolUse, ""},
		{`{"type":"item.completed","item":{"id":"i2","type":"command_execution","aggregated_output":"PASS"}}`, oneshot.KindToolResult, "PASS"},
		{`{"type":"turn.failed","error":{"message":"failed"}}`, oneshot.KindError, "failed"},
	} {
		r := c.ParseLine([]byte(tc.line))
		if len(r.Msgs) != 1 || r.Msgs[0].Kind != tc.kind || r.Msgs[0].Text != tc.text {
			t.Fatalf("%+v", r)
		}
	}
	r := c.ParseLine([]byte(`{"type":"turn.completed","usage":{"input_tokens":12,"cached_input_tokens":4,"output_tokens":3}}`))
	if r.StopReason != "end_turn" || r.Usage["default"].InputTokens != 12 || r.Usage["default"].CacheReadTokens != 4 {
		t.Fatal(r)
	}
}
