package engine

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// TaskPreviewRunes caps a segment task preview (runes, before the ellipsis).
const TaskPreviewRunes = 280

// SecretMask replaces the value of a secret variable in a task preview.
const SecretMask = "•••"

// SegmentTasks returns, per StateRun ID, the interpolated and truncated
// node.config.prompt of each execution in states (StateRun id order) for
// which want(state) is true and the prompt is non-empty.
//
// Read-only: it never calls loadCtx (which re-registers MCP tokens) and never
// writes. Each execution is interpolated the way the engine did when it
// entered the node: vars are that execution's VarsBefore (falling back to the
// run's current variables for rows without it), and node outputs are the
// latest outputs of every execution that precedes it in states.
//
// Variables flagged Secret on the run (seeded from a secret workspace
// variable) render as SecretMask. artifact()/json()/exists() read the
// artifact's CURRENT content (the store keeps no per-visit snapshot), memoized
// for the whole call so each artifact is read at most once. Cost is linear in
// len(states): the outputs map is shared, not copied per execution.
func (e *Engine) SegmentTasks(run models.Run, states []models.StateRun, want func(models.StateRun) bool) map[uint]string {
	out := map[uint]string{}
	var current map[string]any
	var secrets map[string]bool
	secretsLoaded := false
	outputs := map[string]map[string]any{}
	type artifactHit struct {
		content string
		ok      bool
	}
	artifacts := map[string]artifactHit{}
	artifact := func(name string) (string, bool) {
		if hit, ok := artifacts[name]; ok {
			return hit.content, hit.ok
		}
		content, ok := e.store.Get(run.ID, name)
		artifacts[name] = artifactHit{content, ok}
		return content, ok
	}
	for _, sr := range states {
		if want(sr) {
			node := run.Graph.FindNode(sr.NodeID)
			var prompt string
			if node != nil {
				prompt, _ = node.Config["prompt"].(string)
			}
			if strings.TrimSpace(prompt) != "" {
				if !secretsLoaded {
					secrets = e.secretRunVars(run.ID)
					secretsLoaded = true
				}
				vars := sr.VarsBefore
				if vars == nil {
					if current == nil {
						current = e.runVariables(run.ID)
					}
					vars = current
				}
				vars = maskSecretVars(vars, secrets)
				// outputs is only read during this synchronous interpolation,
				// so it is passed as-is and updated after.
				ec := EvalContext{Vars: vars, Nodes: outputs, Artifact: artifact}
				if t := TaskPreview(interpolateWith(ec, prompt)); t != "" {
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

// maskSecretVars returns vars with every secret name's value replaced by
// SecretMask (a copy only when something is masked).
func maskSecretVars(vars map[string]any, secrets map[string]bool) map[string]any {
	masked := vars
	copied := false
	for name := range secrets {
		if _, ok := vars[name]; !ok {
			continue
		}
		if !copied {
			masked = make(map[string]any, len(vars))
			for k, v := range vars {
				masked[k] = v
			}
			copied = true
		}
		masked[name] = SecretMask
	}
	return masked
}

func (e *Engine) secretRunVars(runID string) map[string]bool {
	var names []string
	if e.db.Model(&models.RunVariable{}).Where("run_id = ? AND secret = ?", runID, true).
		Pluck("name", &names).Error != nil {
		return nil
	}
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
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
