package services

import (
	"bytes"
	"encoding/json"

	"github.com/cocofhu/grasp/internal/models"
)

// normalizeOutputConfig treats a missing results list as empty so a client
// that seeds results:[] does not look like a graph change.
func normalizeOutputConfig(cfg map[string]any) map[string]any {
	if _, ok := cfg["results"]; ok {
		return cfg
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	out["results"] = []any{}
	return out
}

// normalizeGraph unifies nil vs empty slices/maps so DeepEqual / JSON compare
// does not treat DTO round-trips as spurious diffs.
func normalizeGraph(g models.Graph) models.Graph {
	out := models.Graph{
		Nodes:     make([]models.Node, 0, len(g.Nodes)),
		Edges:     make([]models.Edge, 0, len(g.Edges)),
		Variables: make([]models.Variable, 0, len(g.Variables)),
	}
	for _, n := range g.Nodes {
		cfg := n.Config
		if cfg == nil {
			cfg = map[string]any{}
		}
		if n.Type == "output" {
			cfg = normalizeOutputConfig(cfg)
		}
		out.Nodes = append(out.Nodes, models.Node{
			ID:         n.ID,
			Type:       n.Type,
			Label:      n.Label,
			Position:   n.Position,
			Config:     cfg,
			Checkpoint: n.Checkpoint,
		})
	}
	if g.Edges != nil {
		out.Edges = append(out.Edges, g.Edges...)
	}
	if g.Variables != nil {
		out.Variables = append(out.Variables, g.Variables...)
	}
	return out
}

// GraphsEqual reports whether two graphs are equivalent after normalizing
// nil/empty collections. Compare after LiftInputVariables so DTO↔Lift
// round-trips do not look like a graph change.
func GraphsEqual(a, b models.Graph) bool {
	return graphJSONEqual(normalizeGraph(a), normalizeGraph(b))
}

func graphJSONEqual(na, nb models.Graph) bool {
	ba, err1 := json.Marshal(na)
	bb, err2 := json.Marshal(nb)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(ba, bb)
}
