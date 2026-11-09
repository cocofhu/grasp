package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperationReplyRequiresExactBoundedCompleteOriginalTurn(t *testing.T) {
	op := OperationIdentity{ID: "g-original", Attempt: 1, Resource: "provider-resource", SessionID: "stable-native-session", Workspace: "/workspace", WorkspaceFingerprint: strings.Repeat("f", 64), Authorization: "grant"}
	client := NewACPClient("localhost", 1).WithAttachOperation(op)
	base := operationReply{Op: "operation_attached", ID: op.ID, Session: "vendor-session-may-change", NativeSession: op.SessionID, Workspace: op.Workspace, Fingerprint: op.WorkspaceFingerprint, Complete: true, Events: []json.RawMessage{json.RawMessage(`{"type":"prompt_begin","opId":"g-original"}`), json.RawMessage(`{"type":"session_update","opId":"g-original","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"original"}}}`), json.RawMessage(`{"type":"prompt_done","opId":"g-original"}`)}}
	raw, _ := json.Marshal(base)
	if _, err := client.checkOperationReply(raw, true); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"op", "native-session", "workspace", "fingerprint", "incomplete", "missing-begin", "duplicate-begin", "missing-update-identity", "duplicate-done", "too-many-events"} {
		t.Run(kind, func(t *testing.T) {
			reply := base
			reply.Events = append([]json.RawMessage(nil), base.Events...)
			switch kind {
			case "op":
				reply.ID = "g-other"
			case "native-session":
				reply.NativeSession = "new-native-session"
			case "workspace":
				reply.Workspace = "/different"
			case "fingerprint":
				reply.Fingerprint = strings.Repeat("a", 64)
			case "incomplete":
				reply.Complete = false
			case "missing-begin":
				reply.Events = reply.Events[1:]
			case "duplicate-begin":
				reply.Events[1] = reply.Events[0]
			case "missing-update-identity":
				reply.Events[1] = json.RawMessage(`{"type":"session_update"}`)
			case "duplicate-done":
				reply.Events = append(reply.Events, reply.Events[2])
			case "too-many-events":
				reply.Events = make([]json.RawMessage, 100001)
			}
			raw, _ := json.Marshal(reply)
			if _, err := client.checkOperationReply(raw, true); err == nil {
				t.Fatal("unverified operation accepted")
			}
		})
	}
}
