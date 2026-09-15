package runtime

import "testing"

func TestNodeNeedsOutcomeIncludesPreflight(t *testing.T) {
	if !nodeNeedsOutcome("preflight") {
		t.Fatal("preflight must require node_complete")
	}
	if !nodeNeedsOutcome("react") {
		t.Fatal("react still needs outcome")
	}
	if nodeNeedsOutcome("input") {
		t.Fatal("input must not need outcome")
	}
}
