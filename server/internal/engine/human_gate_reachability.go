package engine

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// hasRemainingHumanGate reports whether a node with Type=="human_gate" is
// reachable forward from fromNodeID (inclusive) on the given graph snapshot.
//
// Reachability follows OutEdges targets. Only human_gate counts — ReAct review waits and platform
// auto gates do not. Missing/empty graph, missing from node, or unresolvable
// structure returns false (conservative: avoid falsely prioritizing).
// Cycles terminate via a visited set.
func hasRemainingHumanGate(graph *models.Graph, fromNodeID string) bool {
	if graph == nil || fromNodeID == "" {
		return false
	}
	start := graph.FindNode(fromNodeID)
	if start == nil {
		return false
	}
	if start.Type == "human_gate" {
		return true
	}
	visited := map[string]bool{fromNodeID: true}
	queue := []string{fromNodeID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		node := graph.FindNode(cur)
		if node == nil {
			continue
		}
		for _, ed := range graph.OutEdges(node.ID) {
			next := ed.Target
			if visited[next] {
				continue
			}
			visited[next] = true
			gn := graph.FindNode(next)
			if gn != nil && gn.Type == "human_gate" {
				return true
			}
			queue = append(queue, next)
		}
	}
	return false
}

// continueFromNodeID resolves the admission continue point for a queued run:
// Checkpoints[__admission_from__].node when present, else Graph.StartNode().
// Returns ("", false) when neither is available.
func continueFromNodeID(run models.Run) (string, bool) {
	if run.Checkpoints != nil {
		if cp, ok := run.Checkpoints[admissionFromCheckpoint]; ok {
			if node, _ := cp["node"].(string); strings.TrimSpace(node) != "" {
				return node, true
			}
		}
	}
	if start := run.Graph.StartNode(); start != nil {
		return start.ID, true
	}
	return "", false
}
