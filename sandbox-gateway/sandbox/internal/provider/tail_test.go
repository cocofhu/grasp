package provider

import "testing"

func TestOutputTailKeepsTheEnd(t *testing.T) {
	tail := &OutputTail{Max: 8, LogPrefix: "test"}
	_, _ = tail.Write([]byte("first line\n"))
	_, _ = tail.Write([]byte("  last\n"))
	if got := tail.String(); got != "last" {
		t.Fatalf("tail %q", got)
	}
}
