package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// normalizeOutputConfig mirrors web migrateOutputConfig + cleanOutputConfigForSave:
// ensure results[], drop legacy result. Used so a client that cleaned on hydrate
// does not look like a graph change vs a DB head that still stores result.
func normalizeOutputConfig(cfg map[string]any) map[string]any {
	// Saturate capacity hint so len(cfg)+1 cannot overflow (CodeQL #23).
	hint := len(cfg)
	if hint < math.MaxInt {
		hint++
	}
	out := make(map[string]any, hint)
	for k, v := range cfg {
		out[k] = v
	}
	if _, ok := out["results"]; ok {
		if _, isArr := out["results"].([]any); !isArr {
			// JSON arrays sometimes decode as []string; coerce via remarshal path below.
			if raw, err := json.Marshal(out["results"]); err == nil {
				var arr []any
				if json.Unmarshal(raw, &arr) == nil {
					out["results"] = arr
				} else {
					out["results"] = []any{}
				}
			} else {
				out["results"] = []any{}
			}
		}
	} else {
		result := ""
		if r, ok := out["result"]; ok && r != nil {
			result = strings.TrimSpace(fmt.Sprint(r))
		}
		if result != "" {
			out["results"] = []any{result}
		} else {
			out["results"] = []any{}
		}
	}
	delete(out, "result")
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

// GraphsEqualIgnoringLayout is GraphsEqual without node positions: moving
// nodes on the canvas does not change what the workflow runs.
func GraphsEqualIgnoringLayout(a, b models.Graph) bool {
	na, nb := normalizeGraph(a), normalizeGraph(b)
	for i := range na.Nodes {
		na.Nodes[i].Position = models.Position{}
	}
	for i := range nb.Nodes {
		nb.Nodes[i].Position = models.Position{}
	}
	return graphJSONEqual(na, nb)
}

func graphJSONEqual(na, nb models.Graph) bool {
	ba, err1 := json.Marshal(na)
	bb, err2 := json.Marshal(nb)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(ba, bb)
}
