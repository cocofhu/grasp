package mcp

import (
	"errors"
	"strings"
	"testing"
)

type conversationHistoryFake struct {
	runID string
	req   ConversationHistoryRequest
	calls int
	err   error
}

func (f *conversationHistoryFake) ReadConversationHistory(runID string, req ConversationHistoryRequest) (ConversationHistoryPage, error) {
	f.calls++
	f.runID, f.req = runID, req
	return ConversationHistoryPage{ConversationID: "current-thread", HistoryVersion: "snapshot", Messages: []ConversationHistoryMessage{{ID: "original", Role: "user", Content: "保留圆角要求"}}}, f.err
}

func TestGetHistoryAuthorizesScopeAndValidatesInput(t *testing.T) {
	h := NewHost(&memStore{})
	token := h.RegisterRun("current")
	f := &conversationHistoryFake{}
	h.SetConversationHistoryProvider(f)
	for _, tc := range []struct {
		name  string
		token string
		args  map[string]any
	}{
		{"wrong token", "other", nil},
		{"foreign session selector", token, map[string]any{"conversationId": "other"}},
		{"unknown view", token, map[string]any{"view": "everything"}},
		{"negative limit", token, map[string]any{"limit": -1}},
		{"excessive limit", token, map[string]any{"limit": 51}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, failed := h.runTool("current", tc.token, "get_history", tc.args)
			if !failed || !strings.Contains(text, "get_history failed") {
				t.Fatalf("invalid request accepted: %s", text)
			}
		})
	}
	if f.calls != 0 {
		t.Fatalf("unauthorized input reached provider: %d", f.calls)
	}
	text, failed := h.runTool("current", token, "get_history", nil)
	if failed || !strings.Contains(text, "保留圆角要求") || f.runID != "current" || f.req.View != "summary" || f.req.Limit != 20 {
		t.Fatalf("default request: %s %+v", text, f)
	}
	text, failed = h.runTool("current", token, "get_history", map[string]any{"view": "messages", "cursor": "opaque", "limit": 3})
	if failed || f.req.Cursor != "opaque" || f.req.Limit != 3 || f.req.View != "messages" {
		t.Fatalf("paging request: %s %+v", text, f.req)
	}
	f.err = errors.New("current sandbox expired")
	if text, failed = h.runTool("current", token, "get_history", nil); !failed || !strings.Contains(text, f.err.Error()) {
		t.Fatalf("provider error lost: %s", text)
	}
	h.SetConversationHistoryProvider(nil)
	if _, failed := h.runTool("current", token, "get_history", nil); !failed {
		t.Fatal("missing provider must fail")
	}
}

func TestGetHistoryIsListedOnlyForSoloWithProvider(t *testing.T) {
	h := NewHost(&memStore{})
	h.RegisterRun("current")
	h.SetActiveNode("current", "solo", nil)
	listed := func() bool {
		for _, tool := range h.listedTools("current") {
			if tool["name"] == "get_history" {
				annotations, _ := tool["annotations"].(map[string]any)
				if annotations["readOnlyHint"] != true || annotations["destructiveHint"] != false || annotations["idempotentHint"] != true || annotations["openWorldHint"] != false {
					t.Error("current conversation history must advertise its truthful read-only, closed-world semantics")
				}
				return true
			}
		}
		return false
	}
	if listed() {
		t.Fatal("unavailable history advertised")
	}
	h.SetConversationHistoryProvider(&conversationHistoryFake{})
	if !listed() {
		t.Fatal("Solo cannot discover history")
	}
	h.SetActiveNode("current", "workflow-node", nil)
	if listed() {
		t.Fatal("Solo-only history advertised to workflow")
	}
	if got := FormatMCPAuditSummary("get_history", nil, "private conversation", false); got != "读取当前会话历史" {
		t.Fatalf("audit summary: %q", got)
	}
}
