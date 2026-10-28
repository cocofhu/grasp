package handlers

import (
	"encoding/json"
	"testing"
)

func TestReviewRequestRejectionPreservesControlFailureWithoutTerminalEvent(t *testing.T) {
	h := &Handlers{}
	for _, requestType := range []string{"review_chat", "review_cancel"} {
		t.Run(requestType, func(t *testing.T) {
			var got map[string]any
			h.replyReviewWSRejection(func(raw []byte) error { return json.Unmarshal(raw, &got) }, "run", "node", map[string]any{
				"type": "ignored", "event": "error", "message": "rejected", "requestType": requestType,
				"code": "clarify_visit_ended", "clientMessageId": "sender", "autoRetryOnResume": true,
				"dropped": map[string]any{"text": "unsent"},
			})
			if got["type"] != "review" || got["event"] != "request_rejected" || got["requestType"] != requestType || got["nodeId"] != "node" || got["runId"] != "run" {
				t.Fatalf("rejection turned into a terminal event: %v", got)
			}
			if got["clientMessageId"] != "sender" || got["code"] != "clarify_visit_ended" || got["autoRetryOnResume"] != true || got["dropped"].(map[string]any)["text"] != "unsent" {
				t.Fatalf("rejection lost recovery details: %v", got)
			}
		})
	}
}
