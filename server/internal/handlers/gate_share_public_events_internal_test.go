package handlers

import "testing"

func TestPublicDialogueFrameFilterIsPerLane(t *testing.T) {
	node := []byte(`{"type":"dialogue","runId":"r1","nodeId":"p1","rows":[]}`)
	laneA := []byte(`{"type":"dialogue","runId":"r1","nodeId":"p1","lane":"v:a","rows":[]}`)
	cases := []struct {
		raw        []byte
		node, lane string
		want       bool
	}{
		{node, "p1", "", true},
		{node, "p1", "v:a", false},
		{laneA, "p1", "", false},
		{laneA, "p1", "v:a", true},
		{laneA, "p1", "v:b", false},
		{laneA, "p2", "v:a", false},
	}
	for i, c := range cases {
		if got := isDialogueFrameFor(c.raw, c.node, c.lane); got != c.want {
			t.Fatalf("case %d: got %v want %v", i, got, c.want)
		}
	}
}
