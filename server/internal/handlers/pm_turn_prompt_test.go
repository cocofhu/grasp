package handlers

import "testing"

func TestPmTurnPrompt(t *testing.T) {
	if got := pmTurnPrompt("", "问"); got != "问" {
		t.Fatalf("got %q", got)
	}
	if got := pmTurnPrompt("背景", ""); got != "背景\n\n用户问题：（见附件）" {
		t.Fatalf("got %q", got)
	}
}
