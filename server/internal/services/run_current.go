package services

import (
	"math"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// CurrentNode is the run list's view of where an active run is and who is
// driving it. Mode is auto|manual; State is running (auto agent working),
// stopping (taken over, auto call still unwinding), waiting_human (manual
// session idle / gate pending), entering (a confirm already settled on one
// automatic successor, and that visit has not started). The handler upgrades
// waiting_human to busy when a manual session's turn is running (it needs
// the engine for that).
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

// PendingAutoCheckpoint is written when a confirm has already settled on one
// automatic successor, before that successor's visit is running. The run list
// reads it so a refresh can group the row. It is not the control-plane
// admission cursor.
const PendingAutoCheckpoint = "__pending_auto__"

// CurrentStateEntering is currentNode.state for that successor while its
// visit has not started. The sidebar counts it as an automatic process. The
// live page does not: only state=running is observable.
const CurrentStateEntering = "entering"

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

// currentFromVisit is one active StateRun as a CurrentNode. A manual visit
// that is still running is stopping (takeover unwind), not a live auto agent.
func currentFromVisit(r models.Run, sr models.StateRun, state string, waiting bool) CurrentNode {
	cn := CurrentNode{ID: sr.NodeID, Iteration: sr.Iteration, State: state}
	classifyCurrentNode(&cn, r.Graph.FindNode(sr.NodeID), sr, waiting)
	if state == "running" && sr.IsManual() {
		cn.State = "stopping"
	}
	return cn
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
		srRun, hasRun := running[id]
		srWait, hasWait := waiting[id]
		var cn CurrentNode
		switch r.Status {
		case "waiting_human":
			// Confirm returns before the run status leaves waiting_human.
			// resumeAdmitted may already have opened a later running visit;
			// that visit is current, and the older waiting_human row is the
			// node just left. A running visit that is not newer stays hidden
			// so a gate or a still-waiting human node remains current.
			if hasRun && hasWait && srRun.ID > srWait.ID {
				cn = currentFromVisit(r, srRun, "running", false)
				break
			}
			if g, ok := gateByRun[id]; ok {
				cn = CurrentNode{ID: g.NodeID, Iteration: g.Iteration, Kind: CurrentKindGate, Mode: models.NodeModeManual, State: "waiting_human"}
				break
			}
			// The successor's visit is already running and the human visit
			// has closed, but the run status has not caught up. Any other
			// running row stays hidden, same as before this mark existed.
			if hasRun && !hasWait {
				if mark, ok := pendingAutoTarget(r); ok && srRun.NodeID == mark.nodeID {
					cn = currentFromVisit(r, srRun, "running", false)
					break
				}
			}
			if entered, ok := enteringAutoSuccessor(r, hasWait, srWait); ok {
				cn = entered
				break
			}
			if hasWait {
				cn = currentFromVisit(r, srWait, "waiting_human", true)
			}
		case "running":
			if hasRun {
				cn = currentFromVisit(r, srRun, "running", false)
				break
			}
			// Early Done can leave the run "running" with only the human
			// visit still waiting, until the successor's row exists.
			if entered, ok := enteringAutoSuccessor(r, hasWait, srWait); ok {
				cn = entered
				break
			}
			// A confirm can close the conversation before the opening driver
			// pauses the run. Until its successor is settled or starts, the
			// waiting visit remains current even while the run is running.
			if hasWait {
				cn = currentFromVisit(r, srWait, "waiting_human", true)
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

// pendingAuto is the successor a confirm wrote before its visit existed.
type pendingAuto struct {
	nodeID  string
	fromID  string
	visitID uint
}

// pendingAutoTarget reads PendingAutoCheckpoint. ok is false when the mark
// is missing, names a node that is not an automatic agent, or the visit id
// does not decode. An unresolved guard is never stored here.
func pendingAutoTarget(r models.Run) (pendingAuto, bool) {
	if r.Checkpoints == nil {
		return pendingAuto{}, false
	}
	cp, ok := r.Checkpoints[PendingAutoCheckpoint]
	if !ok || cp == nil {
		return pendingAuto{}, false
	}
	nodeID := strings.TrimSpace(strCheckpoint(cp["node"]))
	fromID := strings.TrimSpace(strCheckpoint(cp["from"]))
	visitID, visitOK := uintCheckpoint(cp["visit"])
	if nodeID == "" || fromID == "" || !visitOK || nodeID == fromID {
		return pendingAuto{}, false
	}
	n := r.Graph.FindNode(nodeID)
	if n == nil || n.Type != "agent" || n.Caps == nil || n.Caps.Interaction != models.InteractionAuto || n.Caps.Clarify() {
		return pendingAuto{}, false
	}
	return pendingAuto{nodeID: nodeID, fromID: fromID, visitID: visitID}, true
}

func strCheckpoint(v any) string {
	s, _ := v.(string)
	return s
}

func uintCheckpoint(v any) (uint, bool) {
	switch n := v.(type) {
	case float64:
		if n < 1 || n != math.Trunc(n) || n > float64(^uint(0)>>1) {
			return 0, false
		}
		return uint(n), true
	case int:
		if n < 1 {
			return 0, false
		}
		return uint(n), true
	case int64:
		if n < 1 {
			return 0, false
		}
		return uint(n), true
	case uint:
		if n == 0 {
			return 0, false
		}
		return n, true
	case uint64:
		if n == 0 || n > uint64(^uint(0)>>1) {
			return 0, false
		}
		return uint(n), true
	default:
		return 0, false
	}
}

// enteringAutoSuccessor is the list row for a confirm that already settled
// on one automatic agent, while that visit is not running yet. Callers must
// already have preferred a newer running visit. A different human visit, or
// a new visit of the node that was confirmed, does not keep the mark. No
// waiting visit means the human row has closed and the successor has not
// started: the mark still names the current node.
func enteringAutoSuccessor(r models.Run, hasWait bool, wait models.StateRun) (CurrentNode, bool) {
	mark, ok := pendingAutoTarget(r)
	if !ok {
		return CurrentNode{}, false
	}
	if hasWait {
		if wait.ID != mark.visitID || wait.NodeID != mark.fromID || wait.NodeID == mark.nodeID {
			return CurrentNode{}, false
		}
	}
	return CurrentNode{
		ID:    mark.nodeID,
		Kind:  CurrentKindAgent,
		Mode:  models.NodeModeAuto,
		State: CurrentStateEntering,
	}, true
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
