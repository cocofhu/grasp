package mcp

import (
	"errors"
	"strings"
	"testing"
)

type referenceFake struct {
	conversationHistoryFake
	refID string
	err   error
	calls int
}

func (f *referenceFake) ReadReference(runID, refID string, req ConversationHistoryRequest) (ConversationHistoryPage, error) {
	f.calls++
	f.runID, f.refID, f.req = runID, refID, req
	return ConversationHistoryPage{ConversationID: "other", Ref: refID, Messages: []ConversationHistoryMessage{{ID: "m1", Role: "user", Content: "登录重构要求"}}}, f.err
}

func TestReadReferenceAuthorizesAndValidates(t *testing.T) {
	h := NewHost(&memStore{})
	token := h.RegisterRun("current")
	if text, failed := h.runTool("current", token, "read_reference", map[string]any{"ref": "ref-1"}); !failed || !strings.Contains(text, "不可用") {
		t.Fatalf("missing reader must fail: %s", text)
	}
	f := &referenceFake{}
	h.SetConversationHistoryProvider(f)
	for _, tc := range []struct {
		name  string
		token string
		args  map[string]any
	}{
		{"wrong token", "other", map[string]any{"ref": "ref-1"}},
		{"missing ref", token, nil},
		{"session selector", token, map[string]any{"ref": "ref-1", "sessionId": "x"}},
		{"unknown view", token, map[string]any{"ref": "ref-1", "view": "all"}},
		{"limit too large", token, map[string]any{"ref": "ref-1", "limit": 51}},
		{"negative limit", token, map[string]any{"ref": "ref-1", "limit": -2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if text, failed := h.runTool("current", tc.token, "read_reference", tc.args); !failed || !strings.Contains(text, "read_reference failed") {
				t.Fatalf("accepted: %s", text)
			}
		})
	}
	if f.calls != 0 {
		t.Fatalf("invalid input reached provider: %d", f.calls)
	}
	text, failed := h.runTool("current", token, "read_reference", map[string]any{"ref": "ref-1"})
	if failed || !strings.Contains(text, "登录重构要求") || !strings.Contains(text, `"ref":"ref-1"`) || f.req.View != "summary" || f.req.Limit != 20 || f.runID != "current" {
		t.Fatalf("default read: %s %+v", text, f.req)
	}
	if _, failed = h.runTool("current", token, "read_reference", map[string]any{"ref": "ref-1", "view": "messages", "cursor": "c", "limit": 5}); failed || f.req.Cursor != "c" || f.req.Limit != 5 {
		t.Fatalf("paged read: %+v", f.req)
	}
	f.err = &ReferenceHintError{Hint: "gh issue view 3 -R acme/web"}
	if text, failed = h.runTool("current", token, "read_reference", map[string]any{"ref": "ref-2"}); failed || text != f.err.Error() {
		t.Fatalf("hint must be a normal answer: %q %v", text, failed)
	}
	f.err = errors.New("引用的会话已被删除或不可用")
	if text, failed = h.runTool("current", token, "read_reference", map[string]any{"ref": "ref-3"}); !failed || !strings.Contains(text, f.err.Error()) {
		t.Fatalf("provider error lost: %s", text)
	}
}

func TestReadReferenceAlwaysListedForSolo(t *testing.T) {
	h := NewHost(&memStore{})
	h.RegisterRun("current")
	h.SetActiveNode("current", "solo", nil)
	listed := func() bool {
		for _, tool := range h.listedTools("current") {
			if tool["name"] == "read_reference" {
				return true
			}
		}
		return false
	}
	h.SetConversationHistoryProvider(&conversationHistoryFake{})
	if listed() {
		t.Fatal("history-only provider cannot read references")
	}
	h.SetConversationHistoryProvider(&referenceFake{})
	if !listed() {
		t.Fatal("read_reference must be listed before the first @")
	}
	h.SetActiveNode("current", "workflow-node", nil)
	if listed() {
		t.Fatal("read_reference advertised outside Solo")
	}
	if got := FormatMCPAuditSummary("read_reference", map[string]any{"ref": "ref-1"}, "secret text", false); !strings.Contains(got, "读取引用的会话") || !strings.Contains(got, "ref-1") || strings.Contains(got, "secret") {
		t.Fatalf("audit summary: %q", got)
	}
}
