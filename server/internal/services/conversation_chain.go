package services

import (
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/models/nodereg"
)

// ChainTurn is one turn of a chain segment. It carries the text plus the
// structured detail the fold renders (questions, tools, parts, forms).
// Images and the raw ACP event log stay off this payload.
type ChainTurn struct {
	Role      string                 `json:"role"`
	Text      string                 `json:"text"`
	At        string                 `json:"at"`
	Questions []models.ReactQuestion `json:"questions,omitempty"`
	Tools     []models.ReactTool     `json:"tools,omitempty"`
	Parts     []models.AcpPart       `json:"parts,omitempty"`
	Forms     []models.ReactForm     `json:"forms,omitempty"`
}

// ChainSegment is one node execution (nodeId, iteration) of a run's
// conversation chain. Turns is set (possibly empty) for clarify / review /
// takeover / gate segments; EventCount (auto-phase ACP events of the visit)
// for agent / review / takeover segments; Decision for resolved gates with a
// standard action; Task by the handler (interpolated node prompt preview).
type ChainSegment struct {
	NodeID     string       `json:"nodeId"`
	Iteration  int          `json:"iteration"`
	Name       string       `json:"name"`
	Kind       string       `json:"kind"`
	Mode       string       `json:"mode"`
	Status     string       `json:"status"`
	StartedAt  *time.Time   `json:"startedAt,omitempty"`
	EndedAt    *time.Time   `json:"endedAt,omitempty"`
	Turns      *[]ChainTurn `json:"turns,omitempty"`
	EventCount *int         `json:"eventCount,omitempty"`
	Decision   string       `json:"decision,omitempty"`
	Task       string       `json:"task,omitempty"`
	// Handoff is the note this visit left for the next node (outputs.handoff);
	// HandoffSource is agent or platform (written for a silent upstream).
	Handoff       string `json:"handoff,omitempty"`
	HandoffSource string `json:"handoffSource,omitempty"`
	// Boot is the visit's sandbox start-up; nil when it has none (yet).
	Boot *ChainBoot `json:"boot,omitempty"`

	// StateRunID / NodeType are internal (task derivation in the handler).
	StateRunID uint   `json:"-"`
	NodeType   string `json:"-"`
}

// ChainBoot is one visit's sandbox start-up: Status is pulling / creating
// while it boots, ready (ReadyAt set) once the Agent is connected.
type ChainBoot struct {
	Status  string     `json:"status"`
	ReadyAt *time.Time `json:"readyAt,omitempty"`
}

// Chain boot statuses.
const (
	ChainBootReady = "ready"
)

// Gate decisions surfaced on gate segments.
const (
	ChainDecisionApproved = "approved"
	ChainDecisionRejected = "rejected"
)

// ErrChainUntilNotFound: ?until (/ iteration) names no segment of the chain.
var ErrChainUntilNotFound = errors.New("until segment not found")

// ClassifyVisit is the kind/mode of one node execution, with the same
// priority as currentNode (takeover > gate > clarify > review > agent).
// parkedForHuman is true when the visit is (or was) a human session: it is
// waiting_human now, or a conversation exists for that iteration.
func ClassifyVisit(node *models.Node, sr models.StateRun, parkedForHuman bool) (kind, mode string) {
	cn := CurrentNode{}
	classifyCurrentNode(&cn, node, sr, parkedForHuman)
	return cn.Kind, cn.Mode
}

func terminalVisit(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "skipped":
		return true
	}
	return false
}

// chainStateColumns is every StateRun column the chain needs. The heavy JSON
// columns (events, vars_snapshot, mcp_calls, usage) are left out; events are
// only counted (chainEventCounts).
var chainStateColumns = []string{
	"id", "run_id", "node_id", "node_type", "iteration", "status", "outputs",
	"vars_before", "started_at", "duration_sec", "mode", "taken_over_at", "sandbox_ready_at",
}

// chainEventCounts returns len(events) per StateRun id of the run, computed
// in the database so the event payloads are never loaded. On a dialect
// without a JSON length function, or a row whose events are not valid JSON,
// it falls back to loading just (id, events).
func (s *RunService) chainEventCounts(runID string) map[uint]int {
	type idCount struct {
		ID uint
		N  int
	}
	fn := ""
	switch s.db.Name() {
	case "sqlite":
		fn = "json_array_length"
	case "mysql":
		fn = "JSON_LENGTH"
	}
	out := map[uint]int{}
	if fn != "" {
		var rows []idCount
		expr := "CASE WHEN events IS NULL OR events = '' OR events = 'null' THEN 0 ELSE " + fn + "(events) END AS n"
		if err := s.db.Model(&models.StateRun{}).Select("id, "+expr).
			Where("run_id = ?", runID).Scan(&rows).Error; err == nil {
			for _, r := range rows {
				out[r.ID] = r.N
			}
			return out
		}
	}
	var full []models.StateRun
	s.db.Select("id", "events").Where("run_id = ?", runID).Find(&full)
	for _, sr := range full {
		out[sr.ID] = len(sr.Events)
	}
	return out
}

// ConversationChain returns the run's executions in StateRun order (id asc:
// every (re-)entry appends a row), one segment per (nodeId, iteration), each
// clarify / review / takeover segment carrying the conversation of THAT
// iteration. until / untilIter cut the chain after the matching segment
// (inclusive); untilIter 0 means the node's last execution. It also returns
// the StateRun rows of the kept segments (same order, without events) for
// callers that derive more (task previews). Read-only, four queries.
//
// Gate hot-revise (gate-ReAct) turns are stored on the upstream producer's
// conversation; they are moved to the gate visit they happened in
// (attributeGateTurns), and the producer keeps its own kind / mode.
func (s *RunService) ConversationChain(run models.Run, until string, untilIter int) ([]ChainSegment, []models.StateRun, error) {
	var states []models.StateRun
	if err := s.db.Select(chainStateColumns).Where("run_id = ?", run.ID).Order("id asc").Find(&states).Error; err != nil {
		return nil, nil, err
	}
	counts := s.chainEventCounts(run.ID)
	var convs []models.ReactConversation
	if err := s.db.Where("run_id = ?", run.ID).Order("id asc").Find(&convs).Error; err != nil {
		return nil, nil, err
	}
	convByVisit := make(map[visitKey]models.ReactConversation, len(convs))
	for _, c := range convs {
		it := c.Iteration
		if it < 1 {
			it = 1 // rows written before iterations were tracked
		}
		convByVisit[visitKey{c.NodeID, it}] = c // id asc: the latest row wins
	}

	// Collapse duplicate rows of one visit (defensive): keep the latest.
	seen := make(map[visitKey]int, len(states))
	rows := make([]models.StateRun, 0, len(states))
	for _, sr := range states {
		k := visitKey{sr.NodeID, sr.Iteration}
		if i, ok := seen[k]; ok {
			rows[i] = sr
			continue
		}
		seen[k] = len(rows)
		rows = append(rows, sr)
	}

	booting := s.bootingSandboxes(run.ID)

	// Attribute gate turns over the whole chain, before any cut.
	ownTurns, gateTurns := attributeGateTurns(run.Graph, rows, convByVisit)
	dropCarriedTurns(rows, ownTurns)

	if until != "" {
		cut := -1
		for i, sr := range rows {
			if sr.NodeID == until && (untilIter == 0 || sr.Iteration == untilIter) {
				cut = i
				if untilIter != 0 {
					break
				}
			}
		}
		if cut < 0 {
			return nil, nil, ErrChainUntilNotFound
		}
		rows = rows[:cut+1]
	}

	segs := make([]ChainSegment, 0, len(rows))
	for _, sr := range rows {
		node := run.Graph.FindNode(sr.NodeID)
		k := visitKey{sr.NodeID, sr.Iteration}
		msgs, hasConv := ownTurns[k]
		parked := sr.Status == "waiting_human" ||
			(hasConv && node != nil && node.Type == "agent")
		kind, mode := ClassifyVisit(node, sr, parked)
		seg := ChainSegment{
			NodeID: sr.NodeID, Iteration: sr.Iteration, Name: GraphNodeLabel(run.Graph, sr.NodeID),
			Kind: kind, Mode: mode, Status: sr.Status, StartedAt: sr.StartedAt,
			StateRunID: sr.ID, NodeType: sr.NodeType,
		}
		if node != nil {
			seg.NodeType = node.Type
		}
		if end, ok := visitEnd(sr); ok {
			seg.EndedAt = &end
		}
		if h, _ := sr.Outputs[models.OutputHandoff].(string); strings.TrimSpace(h) != "" {
			seg.Handoff = strings.TrimSpace(h)
			seg.HandoffSource, _ = sr.Outputs[models.OutputHandoffSource].(string)
		}
		seg.Boot = visitBoot(sr, booting)
		switch kind {
		case CurrentKindClarify, CurrentKindReview, CurrentKindTakeover:
			seg.Turns = chainTurns(msgs)
		case CurrentKindGate:
			seg.Turns = chainTurns(gateTurns[k])
			seg.Decision = gateDecision(sr)
		}
		switch kind {
		case CurrentKindAgent, CurrentKindReview, CurrentKindTakeover:
			n := counts[sr.ID]
			seg.EventCount = &n
		}
		segs = append(segs, seg)
	}
	return segs, rows, nil
}

// bootingSandboxes maps node id → status of the run's node sandboxes still
// starting (pulling / creating).
func (s *RunService) bootingSandboxes(runID string) map[string]string {
	var rows []models.Sandbox
	s.db.Select("node_id", "status").
		Where("run_id = ? AND purpose = ? AND status IN ?", runID, "run", []string{"pulling", "creating"}).
		Find(&rows)
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.NodeID] = r.Status
	}
	return out
}

func visitBoot(sr models.StateRun, booting map[string]string) *ChainBoot {
	if sr.SandboxReadyAt != nil {
		return &ChainBoot{Status: ChainBootReady, ReadyAt: sr.SandboxReadyAt}
	}
	if sr.Status != "running" && sr.Status != "waiting_human" {
		return nil
	}
	if st, ok := booting[sr.NodeID]; ok {
		return &ChainBoot{Status: st}
	}
	return nil
}

// dropCarriedTurns empties an interrupted visit's turns when the visit that
// continued it from the failure position carried them on: they show once,
// in the resumed visit.
func dropCarriedTurns(rows []models.StateRun, own map[visitKey][]models.ReactMessage) {
	for i, sr := range rows {
		if sr.Status != "failed" && sr.Status != "cancelled" {
			continue
		}
		k := visitKey{sr.NodeID, sr.Iteration}
		old := own[k]
		if len(old) == 0 {
			continue
		}
		for _, next := range rows[i+1:] {
			if next.NodeID != sr.NodeID {
				continue
			}
			cur := own[visitKey{next.NodeID, next.Iteration}]
			if len(cur) >= len(old) && sameMessage(cur[len(old)-1], old[len(old)-1]) && sameMessage(cur[0], old[0]) {
				own[k] = []models.ReactMessage{}
			}
			break
		}
	}
}

func sameMessage(a, b models.ReactMessage) bool {
	return a.Role == b.Role && a.Text == b.Text && a.At == b.At
}

type visitKey struct {
	node string
	iter int
}

func visitEnd(sr models.StateRun) (time.Time, bool) {
	if sr.StartedAt == nil || !terminalVisit(sr.Status) {
		return time.Time{}, false
	}
	return sr.StartedAt.Add(time.Duration(sr.DurationSec) * time.Second), true
}

func chainTurns(msgs []models.ReactMessage) *[]ChainTurn {
	turns := make([]ChainTurn, 0, len(msgs))
	for _, m := range msgs {
		turns = append(turns, ChainTurn{
			Role: m.Role, Text: m.Text, At: m.At,
			Questions: m.Questions, Tools: m.Tools, Parts: m.Parts, Forms: m.Forms,
		})
	}
	return &turns
}

// gateDecision maps a resolved gate's action to approved / rejected; custom
// actions (and open gates) have none.
func gateDecision(sr models.StateRun) string {
	if sr.Status != "completed" {
		return ""
	}
	action, _ := sr.Outputs["action"].(string)
	switch action {
	case "approve", "pass":
		return ChainDecisionApproved
	case "revise", "fail":
		return ChainDecisionRejected
	}
	return ""
}

// attributeGateTurns splits every conversation into the turns that belong to
// its own visit (own, keyed by the producer visit; a visit whose turns all
// moved to gates has no entry) and the gate hot-revise turns, keyed by the
// gate visit they happened in. Only human_gate visits after the producer
// visit and before the producer's next visit are candidates:
//   - a turn tagged with a gate (ReactMessage.GateNodeID) goes to the latest
//     visit of that gate that started no later than the turn (the first one
//     when none did);
//   - an untagged turn (rows written before the tag existed) goes to a visit
//     of a gate whose primary upstream is the producer when it falls inside
//     that visit's window: in a later second than the gate's start (At has
//     second precision, so a turn in the same second, typically the confirm
//     that opened the gate, stays with the producer) and, for a finished
//     gate, not after its end.
func attributeGateTurns(g models.Graph, rows []models.StateRun, convs map[visitKey]models.ReactConversation) (own, gates map[visitKey][]models.ReactMessage) {
	own = make(map[visitKey][]models.ReactMessage, len(convs))
	gates = map[visitKey][]models.ReactMessage{}
	upstreamOf := map[string]string{}
	for i := range g.Nodes {
		if n := &g.Nodes[i]; n.Type == "human_gate" {
			upstreamOf[n.ID] = nodereg.GatePrimaryUpstreamNodeID(n)
		}
	}
	pos := make(map[visitKey]int, len(rows))
	for i, sr := range rows {
		pos[visitKey{sr.NodeID, sr.Iteration}] = i
	}
	for k, conv := range convs {
		var cands []models.StateRun
		if p, inChain := pos[k]; inChain && len(upstreamOf) > 0 {
			for _, sr := range rows[p+1:] {
				if sr.NodeID == k.node {
					break // the producer's next visit has its own conversation
				}
				if _, isGate := upstreamOf[sr.NodeID]; isGate && sr.StartedAt != nil {
					cands = append(cands, sr)
				}
			}
		}
		var mine []models.ReactMessage
		for _, m := range conv.Messages {
			if gk, ok := gateVisitFor(m, k.node, cands, upstreamOf); ok {
				gates[gk] = append(gates[gk], m)
				continue
			}
			mine = append(mine, m)
		}
		if len(mine) > 0 || len(conv.Messages) == 0 {
			own[k] = mine
		}
	}
	return own, gates
}

func gateVisitFor(m models.ReactMessage, producer string, cands []models.StateRun, upstreamOf map[string]string) (visitKey, bool) {
	if len(cands) == 0 {
		return visitKey{}, false
	}
	at, atErr := time.Parse(time.RFC3339, m.At)
	if m.GateNodeID != "" {
		var pick *models.StateRun
		for i := range cands {
			sr := &cands[i]
			if sr.NodeID != m.GateNodeID {
				continue
			}
			if pick == nil || (atErr == nil && !sr.StartedAt.After(at.Add(time.Second))) {
				pick = sr
			}
		}
		if pick == nil {
			return visitKey{}, false
		}
		return visitKey{pick.NodeID, pick.Iteration}, true
	}
	if atErr != nil {
		return visitKey{}, false
	}
	for _, sr := range cands {
		if upstreamOf[sr.NodeID] != producer || !at.After(sr.StartedAt.Truncate(time.Second)) {
			continue
		}
		if end, ok := visitEnd(sr); ok && at.After(end.Add(time.Second)) {
			continue
		}
		return visitKey{sr.NodeID, sr.Iteration}, true
	}
	return visitKey{}, false
}
