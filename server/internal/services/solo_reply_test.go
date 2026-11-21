package services

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSoloReplyFoldsFramesLikeTheTurn(t *testing.T) {
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
		`{"op":"queue_state","busy":true}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"t0"}}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"I'll run it.\n"}}}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"usage_update"}}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Now."}}}}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"t1"}}}`,
		`{"op":"error","message":"not reply text"}`,
		`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Done"}}}}`,
	} {
		chat.onAcp(json.RawMessage(raw))
	}
	live, _ := solo.PreviewLiveReply(view.ID)
	if want := "I'll run it.\nNow.\n\nDone"; live == nil || live.Content != want {
		t.Fatalf("live reply = %+v, want %q", live, want)
	}
}
