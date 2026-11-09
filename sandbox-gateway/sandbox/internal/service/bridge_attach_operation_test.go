package service

import (
	"backend/internal/provider"
	"encoding/json"
	"testing"
)

func attachFrame(id, kind string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"type": kind, "opId": id})
	return raw
}

func TestAttachOperationReadsExactCompletedNativeTurnWithoutEnsureOrPrompt(t *testing.T) {
	b := newTestBridge(&stubSess{id: "original-session"})
	session := b.NativeSessionID()
	b.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		t.Fatal("AttachOnly created Agent")
		return nil, nil
	}
	for _, id := range []string{"previous-visit", "original-operation"} {
		b.evLog.append(attachFrame(id, "prompt_begin"))
		b.evLog.append(attachFrame(id, "prompt_done"))
	}
	reply, err := b.AttachOperation("original-operation", session, "/tmp", "")
	if err != nil {
		t.Fatal(err)
	}
	frames := reply["events"].([]json.RawMessage)
	if reply["complete"] != true || len(frames) != 2 {
		t.Fatal(reply)
	}
	for _, raw := range frames {
		var f struct {
			ID string `json:"opId"`
		}
		_ = json.Unmarshal(raw, &f)
		if f.ID != "original-operation" {
			t.Fatal(string(raw))
		}
	}
	if _, err := b.AttachOperation("missing", session, "/tmp", ""); err == nil {
		t.Fatal("missing operation adopted")
	}
	if _, err := b.AttachOperation("original-operation", "new-session", "/tmp", ""); err == nil {
		t.Fatal("foreign session adopted")
	}
	if _, err := b.AttachOperation("original-operation", session, "/other", ""); err == nil {
		t.Fatal("foreign workspace adopted")
	}
	b.sess = nil
	if _, err := b.AttachOperation("original-operation", session, "/tmp", ""); err == nil {
		t.Fatal("lost session recreated")
	}
}

func TestNativeOperationLogRejectsAmbiguousOrIncompleteProvenance(t *testing.T) {
	for name, frames := range map[string][]json.RawMessage{
		"missing":         nil,
		"duplicate":       {attachFrame("original", "prompt_begin"), attachFrame("original", "prompt_done"), attachFrame("original", "prompt_begin")},
		"interleaved":     {attachFrame("original", "prompt_begin"), attachFrame("other", "session_update"), attachFrame("original", "prompt_done")},
		"untagged-update": {attachFrame("original", "prompt_begin"), json.RawMessage(`{"type":"session_update"}`), attachFrame("original", "prompt_done")},
		"missing-begin":   {attachFrame("original", "prompt_done")},
	} {
		t.Run(name, func(t *testing.T) {
			var log eventLog
			for _, f := range frames {
				log.append(f)
			}
			if _, _, err := log.operation("original"); err == nil {
				t.Fatal("unprovable operation accepted")
			}
		})
	}
}

func TestNativeBroadcastPersistsOperationIdentityOnUpdates(t *testing.T) {
	b := NewBridge()
	b.tryRecordBroadcast([]byte(`{"op":"event","opId":"original","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk"}}}`))
	var frame struct {
		ID string `json:"opId"`
	}
	if err := json.Unmarshal(b.evLog.snapshot()[0], &frame); err != nil || frame.ID != "original" {
		t.Fatal(frame, err)
	}
}
