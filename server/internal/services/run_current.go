package services

import (
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// CurrentNode is the run list's view of where an active run is and who is
// driving it. Mode is auto|manual; State is running (auto agent working),
// stopping (taken over, auto call still unwinding), waiting_human (manual
// session idle / gate pending). The handler upgrades waiting_human to busy
// when a manual session's turn is running (it needs the engine for that).
//
// Kind is agent (auto Agent / other auto node) | gate | clarify | review |
// takeover. Mode is auto only for kind=agent; every human-driven kind is
// manual.
type CurrentNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Iteration int    `json:"iteration"`
	Kind      string `json:"kind"`
	Mode      string `json:"mode"`
	State     string `json:"state"`
	// Task is the node's task preview; only the run detail sets it (Agent
	// nodes with a non-blank prompt).
	Task string `json:"task,omitempty"`
}

// Current node kinds.
const (
	CurrentKindAgent    = "agent"
	CurrentKindGate     = "gate"
	CurrentKindClarify  = "clarify"
	CurrentKindReview   = "review"
	CurrentKindTakeover = "takeover"
)

// classifyCurrentNode fills Kind and Mode for a current visit.
func classifyCurrentNode(cn *CurrentNode, node *models.Node, sr models.StateRun, waiting bool) {
	switch {
	case sr.IsManual():
		cn.Kind = CurrentKindTakeover
	case node != nil && node.Type == "human_gate":
		cn.Kind = CurrentKindGate
	case node != nil && node.Caps.Clarify():
		cn.Kind = CurrentKindClarify
	case waiting:
		// A non-clarify Agent parked for a human is its post-run review.
		cn.Kind = CurrentKindReview
	default:
		cn.Kind = CurrentKindAgent
	}
	cn.Mode = models.NodeModeManual
	if cn.Kind == CurrentKindAgent {
		cn.Mode = models.NodeModeAuto
	}
}

// CurrentNodes returns the current node of every running / waiting_human run
// in runs, in a fixed number of queries (no per-run N+1).
func (s *RunService) CurrentNodes(runs []models.Run) map[string]CurrentNode {
	activeIDs := make([]string, 0)
	runByID := make(map[string]models.Run)
	for _, r := range runs {
		if r.Status == "running" || r.Status == "waiting_human" {
			activeIDs = append(activeIDs, r.ID)
			runByID[r.ID] = r
		}
	}
	if len(activeIDs) == 0 {
		return nil
	}
	var gates []models.Gate
	s.db.Where("run_id IN ? AND resolved = ?", activeIDs, false).Find(&gates)
	gateByRun := make(map[string]models.Gate, len(gates))
	for _, g := range gates {
		if existing, ok := gateByRun[g.RunID]; !ok || g.Iteration > existing.Iteration || (g.Iteration == existing.Iteration && g.ID > existing.ID) {
			gateByRun[g.RunID] = g
		}
	}
	var states []models.StateRun
	s.db.Select("id", "run_id", "node_id", "iteration", "status", "mode").
		Where("run_id IN ? AND status IN ?", activeIDs, []string{"running", "waiting_human"}).
		Order("id desc").Find(&states)
	running := make(map[string]models.StateRun)
	waiting := make(map[string]models.StateRun)
	for _, sr := range states {
		switch sr.Status {
		case "running":
			if _, ok := running[sr.RunID]; !ok {
				running[sr.RunID] = sr
			}
		case "waiting_human":
			if _, ok := waiting[sr.RunID]; !ok {
				waiting[sr.RunID] = sr
			}
		}
	}
	out := make(map[string]CurrentNode, len(activeIDs))
	for _, id := range activeIDs {
		r := runByID[id]
		var cn CurrentNode
		switch r.Status {
		case "waiting_human":
			if g, ok := gateByRun[id]; ok {
				cn = CurrentNode{ID: g.NodeID, Iteration: g.Iteration, Kind: CurrentKindGate, Mode: models.NodeModeManual, State: "waiting_human"}
			} else if sr, ok := waiting[id]; ok {
				cn = CurrentNode{ID: sr.NodeID, Iteration: sr.Iteration, State: "waiting_human"}
				classifyCurrentNode(&cn, r.Graph.FindNode(sr.NodeID), sr, true)
			}
		case "running":
			if sr, ok := running[id]; ok {
				cn = CurrentNode{ID: sr.NodeID, Iteration: sr.Iteration, State: "running"}
				classifyCurrentNode(&cn, r.Graph.FindNode(sr.NodeID), sr, false)
				if sr.IsManual() {
					cn.State = "stopping"
				}
			}
		}
		if cn.ID == "" {
			continue
		}
		cn.Name = GraphNodeLabel(r.Graph, cn.ID)
		out[id] = cn
	}
	return out
}

// RunLastActivity is the effective last-activity time of a run: the stamped
// column, else started_at (created_at while still queued).
func RunLastActivity(r models.Run) time.Time {
	if r.LastActivityAt != nil && !r.LastActivityAt.IsZero() {
		return *r.LastActivityAt
	}
	if r.Status == "queued" || r.StartedAt.IsZero() {
		return r.CreatedAt
	}
	return r.StartedAt
}
