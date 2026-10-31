package engine

import (
	"strings"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

// PublicDialogueRow is a DialogueRow as a share-link viewer sees it: the
// public turn fields (gateshare.SanitizeTurnsFrom) plus the row identity.
type PublicDialogueRow struct {
	gateshare.PreviewTurn
	ID              string `json:"id"`
	TurnID          string `json:"turnId,omitempty"`
	ClientMessageID string `json:"clientMessageId,omitempty"`
	Phase           string `json:"phase"`
	Rev             uint64 `json:"rev"`
	Outcome         string `json:"outcome,omitempty"`
	Handoff         bool   `json:"handoff,omitempty"`
	Thought         string `json:"thought,omitempty"`
	Streaming       bool   `json:"streaming,omitempty"`
}

// PublicDialogueTurn drops error details and sandbox op ids.
type PublicDialogueTurn struct {
	TurnID            string                       `json:"turnId"`
	ClientMessageID   string                       `json:"clientMessageId,omitempty"`
	Phase             string                       `json:"phase"`
	Position          *int                         `json:"position,omitempty"`
	Outcome           string                       `json:"outcome,omitempty"`
	Code              string                       `json:"code,omitempty"`
	AutoRetryOnResume bool                         `json:"autoRetryOnResume,omitempty"`
	Item              *gateshare.PreviewActiveItem `json:"item,omitempty"`
}

// PublicDialogueOrphan says a sandbox turn is still running, nothing more.
type PublicDialogueOrphan struct {
	Desynced bool `json:"desynced"`
}

// PublicDialogueState is the share-link form of DialogueState: always full,
// no run id, the public node id, sanitized rows. Prefix digests are over row
// ids and revisions, which are unchanged, so tail logic still works.
type PublicDialogueState struct {
	Type      string                `json:"type"`
	NodeID    string                `json:"nodeId"`
	Iteration int                   `json:"iteration"`
	Epoch     int64                 `json:"epoch"`
	Seq       uint64                `json:"seq"`
	Mode      string                `json:"mode"`
	Rows      []PublicDialogueRow   `json:"rows"`
	Turns     []PublicDialogueTurn  `json:"turns"`
	Orphan    *PublicDialogueOrphan `json:"orphan,omitempty"`
	Busy      bool                  `json:"busy"`
	Done      bool                  `json:"done"`
	Observe   bool                  `json:"observe,omitempty"`
}

// PublicDialogue redacts st for share-link viewers. Opaque image indexes run
// over rows, then queued items, the order DialogueImageCatalog uses.
func PublicDialogue(st DialogueState) PublicDialogueState {
	out := PublicDialogueState{Type: "dialogue", NodeID: gateshare.PublicDialogueNodeID, Iteration: st.Iteration,
		Epoch: st.Epoch, Seq: st.Seq, Mode: "full", Rows: []PublicDialogueRow{}, Turns: []PublicDialogueTurn{},
		Busy: st.Busy, Done: st.Done, Observe: st.Observe}
	idx := 0
	for _, r := range st.Rows {
		var turns []gateshare.PreviewTurn
		turns, idx = gateshare.SanitizeTurnsFrom([]models.ReactMessage{r.ReactMessage}, idx)
		pt := gateshare.PreviewTurn{Role: r.Role, At: r.At}
		if len(turns) == 1 {
			pt = turns[0]
		}
		out.Rows = append(out.Rows, PublicDialogueRow{PreviewTurn: pt, ID: publicOrphanID(r.ID), TurnID: publicOrphanID(r.TurnID),
			ClientMessageID: r.ClientMessageID, Phase: r.Phase, Rev: r.Rev, Outcome: r.Outcome,
			Handoff: r.Handoff, Thought: r.Thought, Streaming: r.Streaming})
	}
	for _, t := range st.Turns {
		pt := PublicDialogueTurn{TurnID: publicOrphanID(t.TurnID), ClientMessageID: t.ClientMessageID, Phase: t.Phase,
			Position: t.Position, Outcome: t.Outcome, Code: t.Code, AutoRetryOnResume: t.AutoRetryOnResume}
		if t.Phase == turnQueued && t.Item != nil {
			pt.Item, idx = gateshare.SanitizeActiveItemFrom(t.Item, idx)
		}
		out.Turns = append(out.Turns, pt)
	}
	if st.Orphan != nil {
		out.Orphan = &PublicDialogueOrphan{Desynced: st.Orphan.Desynced}
	}
	return out
}

// publicOrphanID drops the sandbox op id from an orphan turn or row id
// ("orphan:<op>[:aK]" → "orphan[:aK]"). Orphan rows are never part of a
// sealed prefix, so this cannot break a prefix digest.
func publicOrphanID(id string) string {
	rest, ok := strings.CutPrefix(id, "orphan:")
	if !ok {
		return id
	}
	if i := strings.LastIndex(rest, ":a"); i >= 0 {
		return "orphan" + rest[i:]
	}
	return "orphan"
}

// PublicDialogueSnapshot is the node's own dialogue for share-link viewers.
func (e *Engine) PublicDialogueSnapshot(runID, nodeID string) (PublicDialogueState, bool) {
	st, ok := e.DialogueSnapshot(runID, nodeID)
	if !ok {
		return PublicDialogueState{}, false
	}
	return PublicDialogue(st), true
}

// PublicLaneDialogueSnapshot is a share visitor lane's dialogue; lane ""
// is the node's own.
func (e *Engine) PublicLaneDialogueSnapshot(runID, nodeID, lane, linkID string) (PublicDialogueState, bool) {
	if lane == "" {
		return e.PublicDialogueSnapshot(runID, nodeID)
	}
	st, ok := e.LaneDialogueSnapshot(runID, nodeID, lane, linkID)
	if !ok {
		return PublicDialogueState{}, false
	}
	return PublicDialogue(st), true
}
