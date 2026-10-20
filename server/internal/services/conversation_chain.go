package services

import (
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// ChainTurn is one text-only turn of a chain segment (the clarifyByNode turn
// shape without images / questions / forms / events).
type ChainTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// ChainSegment is one node execution (nodeId, iteration) of a run's
// conversation chain. Turns is set (possibly empty) for clarify / review /
// takeover segments, EventCount for auto (kind=agent) segments, Task by the
// handler (interpolated node prompt preview).
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
	Task       string       `json:"task,omitempty"`

	// StateRunID / NodeType are internal (task derivation in the handler).
	StateRunID uint   `json:"-"`
	NodeType   string `json:"-"`
}

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

// ConversationChain returns the run's executions in StateRun order (id asc:
// every (re-)entry appends a row), one segment per (nodeId, iteration), each
// clarify / review / takeover segment carrying the conversation of THAT
// iteration. until / untilIter cut the chain after the matching segment
// (inclusive); untilIter 0 means the node's last execution. It also returns
// the full StateRun rows of the kept segments (same order) for callers that
// derive more (task previews). Read-only.
func (s *RunService) ConversationChain(run models.Run, until string, untilIter int) ([]ChainSegment, []models.StateRun, error) {
	var states []models.StateRun
	if err := s.db.Where("run_id = ?", run.ID).Order("id asc").Find(&states).Error; err != nil {
		return nil, nil, err
	}
	var convs []models.ReactConversation
	if err := s.db.Where("run_id = ?", run.ID).Order("id asc").Find(&convs).Error; err != nil {
		return nil, nil, err
	}
	type visitKey struct {
		node string
		iter int
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
		conv, hasConv := convByVisit[visitKey{sr.NodeID, sr.Iteration}]
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
		if sr.StartedAt != nil && terminalVisit(sr.Status) {
			end := sr.StartedAt.Add(time.Duration(sr.DurationSec) * time.Second)
			seg.EndedAt = &end
		}
		switch kind {
		case CurrentKindClarify, CurrentKindReview, CurrentKindTakeover:
			turns := make([]ChainTurn, 0, len(conv.Messages))
			for _, m := range conv.Messages {
				turns = append(turns, ChainTurn{Role: m.Role, Text: m.Text, At: m.At})
			}
			seg.Turns = &turns
		case CurrentKindAgent:
			n := len(sr.Events)
			seg.EventCount = &n
		}
		segs = append(segs, seg)
	}
	return segs, rows, nil
}
