package models

import (
	"reflect"
	"testing"
)

func TestReplyOutcome(t *testing.T) {
	cases := []struct {
		name string
		m    ReactMessage
		want string
	}{
		{"human", ReactMessage{Role: "human"}, ""},
		{"blank", ReactMessage{Role: "agent", Text: "  "}, OutcomeEmpty},
		{"text", ReactMessage{Role: "agent", Text: "好的"}, OutcomeOK},
		{"questions", ReactMessage{Role: "agent", Questions: []ReactQuestion{{ID: "q"}}}, OutcomeOK},
		{"handoff", ReactMessage{Role: "agent", Handoff: true}, OutcomeOK},
		{"interrupted", ReactMessage{Role: "agent", Interrupted: true}, OutcomeInterrupted},
		{"banner", ReactMessage{Role: "agent", Text: "(澄清回复失败:boom)"}, OutcomeFailed},
		{"review banner", ReactMessage{Role: "agent", Text: "复审修改失败:x"}, OutcomeFailed},
		{"generic banner", ReactMessage{Role: "agent", Text: "(Live 初始化失败: x)"}, OutcomeFailed},
		{"failure word in prose", ReactMessage{Role: "agent", Text: "上次登录失败的原因是…"}, OutcomeOK},
		{"stored failed", ReactMessage{Role: "agent", Text: "部分输出", Outcome: OutcomeFailed}, OutcomeFailed},
		{"stored ok but blank", ReactMessage{Role: "agent", Outcome: OutcomeOK}, OutcomeEmpty},
	}
	for _, c := range cases {
		if got := ReplyOutcome(c.m); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	if !RetryableReply(ReactMessage{Role: "agent"}) || RetryableReply(ReactMessage{Role: "agent", Interrupted: true}) {
		t.Fatal("only empty and failed replies are retryable")
	}
}

// The last in-flight row and the persisted reply are the same projection of
// the same events, so sealing a reply never reshapes it.
func TestLiveReplyRowsMatchPersistedReply(t *testing.T) {
	events := []AcpEvent{
		{Kind: "thought", Text: "先看代码"},
		{Kind: "tool_call", Title: "Read a.go", Status: "completed"},
		{Kind: "message", Text: "第一段"},
		{Kind: "segment"},
		{Kind: "thought", Text: "再想想"},
		{Kind: "tool_call", Title: "Edit b.go", Status: "completed"},
		{Kind: "message", Text: "第二段"},
	}
	rows := LiveReplyRows(events)
	if len(rows) != 2 || !rows[0].Handoff || rows[0].Text != "第一段" || rows[1].Handoff || rows[1].Text != "第二段" {
		t.Fatalf("rows=%+v", rows)
	}
	body := ReplyRow(events, "第二段")
	if !reflect.DeepEqual(rows[1].Tools, body.Tools) || !reflect.DeepEqual(rows[1].Parts, body.Parts) {
		t.Fatalf("live %+v / %+v\npersisted %+v / %+v", rows[1].Tools, rows[1].Parts, body.Tools, body.Parts)
	}
}

func TestLiveReplyRowsKeepsTheOpenRowAndSkipsFailureEchoes(t *testing.T) {
	rows := LiveReplyRows([]AcpEvent{{Kind: "message", Text: "第一段"}, {Kind: "segment"}})
	if len(rows) != 2 || rows[1].Text != "" || rows[1].Handoff {
		t.Fatalf("open row after a sealed segment: %+v", rows)
	}
	rows = LiveReplyRows([]AcpEvent{{Kind: "message", Text: "react reply chat failed: eof"}})
	if len(rows) != 1 || rows[0].Text != "" {
		t.Fatalf("failure echo shown as reply: %+v", rows)
	}
	if rows := LiveReplyRows(nil); len(rows) != 1 {
		t.Fatalf("no events still has the open row: %+v", rows)
	}
}

func TestWithReplyOutcomesFillsAgentRows(t *testing.T) {
	in := []ReactMessage{{Role: "human", Text: "q"}, {Role: "agent"}, {Role: "agent", Text: "(澄清回复失败:boom)"}, {Role: "agent", Text: "ok"}}
	out := WithReplyOutcomes(in)
	got := []string{out[0].Outcome, out[1].Outcome, out[2].Outcome, out[3].Outcome}
	want := []string{"", OutcomeEmpty, OutcomeFailed, OutcomeOK}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d outcome %q, want %q", i, got[i], want[i])
		}
	}
	if in[1].Outcome != "" {
		t.Fatal("input changed")
	}
}
