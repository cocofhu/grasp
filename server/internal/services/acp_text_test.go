package services

import (
	"context"
	"encoding/json"
	"testing"
)

func TestReplyText(t *testing.T) {
	cases := []struct {
		raw  string
		text string
		brk  bool
	}{
		{`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"hi"}}}`, "hi", false},
		{`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agentMessageChunk","content":[{"text":"a"},{"text":"b"}]}}}`, "ab", false},
		{`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"t"}}`, "", true},
		{`{"type":"session_update","update":{"sessionUpdate":"tool_call_update","toolCallId":"t"}}`, "", true},
		{`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"x"}}}`, "", true},
		{`{"op":"event","data":{"type":"user_steer","text":"more"}}`, "", true},
		{`{"type":"session_update","update":{"sessionUpdate":"usage_update"}}`, "", false},
		{`{"type":"prompt_done"}`, "", false},
		{`not json`, "", false},
	}
	for _, c := range cases {
		text, brk := replyText(json.RawMessage(c.raw))
		if text != c.text || brk != c.brk {
			t.Errorf("%s = %q %v, want %q %v", c.raw, text, brk, c.text, c.brk)
		}
	}
}

func TestSoloReplyParagraphs(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, _ := newSoloFixture(t, port, 3)
	gate := newGateChat(true)
	solo.SetChatterForTest(gate)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = solo.Close(context.Background(), view.ID, "alice") }()
	if _, err := solo.SendWithReferences(view.ID, "alice", "go", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "active turn", func() bool { return gate.count() == 1 })
	chat := solo.lookup(view.ID)
	for _, raw := range []string{
		`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"t0"}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"I'll run it.\n"}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"usage_update"}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Now."}}}`,
		`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"t1"}}`,
		`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Done"}}}`,
	} {
		chat.onAcp(json.RawMessage(raw))
	}
	live, _ := solo.PreviewLiveReply(view.ID)
	if want := "I'll run it.\nNow.\n\nDone"; live == nil || live.Content != want {
		t.Fatalf("live reply = %+v, want %q", live, want)
	}
}
