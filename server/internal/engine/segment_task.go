package engine

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// TaskPreviewRunes caps a segment task preview (runes, before the ellipsis).
const TaskPreviewRunes = 280

// SegmentTasks returns, per StateRun ID, the interpolated and truncated
// node.config.prompt of each execution in states (StateRun id order) for
// which want(state) is true and the prompt is non-empty.
//
// Read-only: it never calls loadCtx (which re-registers MCP tokens) and never
// writes. Each execution is interpolated the way the engine did when it
// entered the node: vars are that execution's VarsBefore (falling back to the
// run's current variables for rows without it), and node outputs are the
// latest outputs of every execution that precedes it in states.
func (e *Engine) SegmentTasks(run models.Run, states []models.StateRun, want func(models.StateRun) bool) map[uint]string {
	out := map[uint]string{}
	var current map[string]any
	outputs := map[string]map[string]any{}
	for _, sr := range states {
		if want(sr) {
			node := run.Graph.FindNode(sr.NodeID)
			var prompt string
			if node != nil {
				prompt, _ = node.Config["prompt"].(string)
			}
			if strings.TrimSpace(prompt) != "" {
				vars := sr.VarsBefore
				if vars == nil {
					if current == nil {
						current = e.runVariables(run.ID)
					}
					vars = current
				}
				nodes := make(map[string]map[string]any, len(outputs))
				for k, v := range outputs {
					nodes[k] = v
				}
				c := &execCtx{run: &run, graph: run.Graph, vars: vars, nodeOutputs: nodes}
				if t := TaskPreview(e.interpolate(c, prompt)); t != "" {
					out[sr.ID] = t
				}
			}
		}
		if sr.Outputs != nil {
			outputs[sr.NodeID] = sr.Outputs
		}
	}
	return out
}

func (e *Engine) runVariables(runID string) map[string]any {
	vars := map[string]any{}
	var rows []models.RunVariable
	if e.db.Where("run_id = ?", runID).Find(&rows).Error != nil {
		return vars
	}
	for _, v := range rows {
		vars[v.Name] = v.Value
	}
	return vars
}

// TaskPreview trims s and truncates it to TaskPreviewRunes runes plus "…".
func TaskPreview(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= TaskPreviewRunes {
		return s
	}
	return strings.TrimRightFunc(string(r[:TaskPreviewRunes]), isSpaceRune) + "…"
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == '\r' }
