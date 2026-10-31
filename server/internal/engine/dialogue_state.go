package engine

import (
	"fmt"
	"hash/fnv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
)

// Row phases. A row keeps its id across all of them: sealing a reply is a
// phase change, never a new row.
const (
	rowRunning = "running"
	rowOrphan  = "orphan"
	rowSealed  = "sealed"
)

// Turn phases.
const (
	turnQueued  = "queued"
	turnRunning = "running"
	turnOrphan  = "orphan"
	turnEnded   = "ended"
)

// DialogueRow is one row of a node dialogue. Persisted rows and in-flight
// rows share this shape; Phase says which one it is.
type DialogueRow struct {
	models.ReactMessage
	ID    string `json:"id"`
	Phase string `json:"phase"`
	// Rev is the version at which this row's content last changed.
	Rev       uint64 `json:"rev"`
	Thought   string `json:"thought,omitempty"`
	Streaming bool   `json:"streaming,omitempty"`
}

// DialogueTurn is the platform's view of one turn, including turns that have
// no row yet (queued) or never will (dropped).
type DialogueTurn struct {
	TurnID            string         `json:"turnId"`
	ClientMessageID   string         `json:"clientMessageId,omitempty"`
	Phase             string         `json:"phase"`
	Position          *int           `json:"position,omitempty"`
	Outcome           string         `json:"outcome,omitempty"`
	Error             string         `json:"error,omitempty"`
	Code              string         `json:"code,omitempty"`
	AutoRetryOnResume bool           `json:"autoRetryOnResume,omitempty"`
	RunningOpID       string         `json:"runningOpId,omitempty"`
	Item              map[string]any `json:"item,omitempty"`
}

// DialogueOrphan is a sandbox turn still running after the platform FIFO went
// idle. It is only ever computed while the platform is idle.
type DialogueOrphan struct {
	RunningOpID string `json:"runningOpId"`
	Desynced    bool   `json:"desynced"`
}

// DialogueState is the authoritative view of one node dialogue (clarify,
// review, or an observed automatic visit). The same value is sent over the
// run WebSocket (type "dialogue") and returned by REST.
type DialogueState struct {
	Type         string          `json:"type"`
	RunID        string          `json:"runId"`
	NodeID       string          `json:"nodeId"`
	Lane         string          `json:"lane,omitempty"`
	Iteration    int             `json:"iteration"`
	Kind         string          `json:"kind,omitempty"`
	Epoch        int64           `json:"epoch"`
	Seq          uint64          `json:"seq"`
	Mode         string          `json:"mode"`
	PrefixLen    int             `json:"prefixLen,omitempty"`
	PrefixDigest string          `json:"prefixDigest,omitempty"`
	Rows         []DialogueRow   `json:"rows"`
	Turns        []DialogueTurn  `json:"turns"`
	Orphan       *DialogueOrphan `json:"orphan,omitempty"`
	Busy         bool            `json:"busy"`
	Done         bool            `json:"done"`
	Observe      bool            `json:"observe,omitempty"`
}

// Version is the state's (epoch, seq).
func (s DialogueState) Version() chatsession.DialogueVersion {
	return chatsession.DialogueVersion{Epoch: s.Epoch, Seq: s.Seq}
}

// dialogueFaults are test-only switches that re-introduce known classes of
// dialogue bugs, so the invariant tests can prove they catch them.
var dialogueFaults struct {
	// sandboxWhileBusy lets the bridge's busy flag end a turn the platform
	// is still running (the "本轮没有输出" root cause).
	sandboxWhileBusy atomic.Bool
	// skipVersion reuses the previous seq for a changed state.
	skipVersion atomic.Bool
	// resealRowID gives a reply a new row id when it is persisted.
	resealRowID atomic.Bool
}

// endedTurn remembers how a recent turn ended for turns that left no row.
type endedTurn struct {
	turnID, clientMessageID string
	code                    string
	autoRetry               bool
	errMsg                  string
}

const maxEndedTurns = 16

// buildDialogue computes the node dialogue from the persisted conversation,
// the platform session and (only while the platform is idle) the sandbox
// bridge. ok is false when the node has no dialogue to show. Callers hold
// entry.mu.
func (e *Engine) buildDialogue(entry *dialogueEntry, runID, nodeID string) (DialogueState, bool) {
	st := DialogueState{Type: "dialogue", RunID: runID, NodeID: nodeID, Mode: "full",
		Rows: []DialogueRow{}, Turns: []DialogueTurn{}}
	if entry.lane != "" {
		return e.buildLaneDialogue(entry, st), true
	}

	var conv *models.ReactConversation
	var c models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&c).Error; err == nil {
		conv = &c
	}
	var sr *models.StateRun
	var r models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("id desc").First(&r).Error; err == nil {
		sr = &r
	}

	var view chatsession.View[*reviewQueueItem]
	sess := e.laneSession(runID, nodeID, "")
	if sess != nil {
		view = sess.View()
		st.Kind = string(sess.kind)
	}
	platformBusy := view.HasActive || len(view.Pending) > 0

	// A finished automatic visit stays observed only for viewers who watched
	// it run; its transcript is otherwise the node's own history.
	observe := !platformBusy && sr != nil && (conv == nil || conv.Iteration < sr.Iteration) &&
		(sr.Status == "running" || entry.observed == sr.Iteration) && e.nodeIsAgent(runID, nodeID)
	if conv == nil && !observe {
		if !platformBusy {
			return st, false
		}
	}
	if observe {
		e.buildObserved(entry, &st, sr)
		return st, true
	}

	if conv != nil {
		st.Iteration = conv.Iteration
		st.Done = conv.Done
		if st.Kind == "" {
			st.Kind = string(sessionKindReview)
			if !conv.Takeover && e.nodeIsClarify(runID, nodeID) {
				st.Kind = string(sessionKindClarify)
			}
		}
		if entry.iteration != conv.Iteration {
			entry.iteration = conv.Iteration
			entry.ended = nil
			entry.started = map[string]string{}
		}
	}
	var persisted []models.ReactMessage
	if conv != nil {
		persisted = conv.Messages
	}

	var active *reviewQueueItem
	if view.HasActive {
		active = view.Active
	}
	// A retry covers the trailing empty/failed reply of the request it reruns;
	// that request is this turn's human row until the turn restamps it.
	retryHuman := -1
	if active != nil && active.RetryLast && !hasTurnHuman(persisted, active.ID) {
		n := len(persisted)
		if n > 0 && models.RetryableReply(persisted[n-1]) {
			persisted = persisted[:n-1]
			n--
		}
		if n > 0 && persisted[n-1].Role == "human" {
			retryHuman = n - 1
		}
	}

	agentIdx := map[string]int{}
	for i, m := range persisted {
		turnID := m.TurnID
		if i == retryHuman {
			turnID = active.ID
		}
		row := DialogueRow{ReactMessage: m, Phase: rowSealed}
		row.TurnID = turnID
		row.ID = dialogueRowID(conv.Iteration, i, turnID, m.Role, agentIdx)
		if m.Role == "agent" {
			row.Outcome = models.ReplyOutcome(m)
			if dialogueFaults.resealRowID.Load() && turnID != "" {
				row.ID += ":sealed"
			}
		}
		st.Rows = append(st.Rows, row)
	}

	if dialogueFaults.sandboxWhileBusy.Load() && platformBusy {
		e.faultProbeSandbox(entry, runID, nodeID)
	}
	if active != nil {
		e.appendActiveTurn(entry, &st, active, view.LiveEvents, persisted, agentIdx)
	}
	appendQueuedTurns(&st, view.Pending)
	st.Busy = platformBusy

	if conv != nil && !conv.Done {
		e.attachOrphan(entry, &st, runID, nodeID, platformBusy, agentIdx)
	}
	for _, et := range entry.ended {
		st.Turns = append(st.Turns, endedDialogueTurn(et, st.Rows))
	}
	return st, true
}

// buildLaneDialogue is a share visitor's own chat: its transcript (the node's
// dialogue until the visitor first speaks) and its lane session. A lane has no
// iterations, observed visits or orphan sandbox turns.
func (e *Engine) buildLaneDialogue(entry *dialogueEntry, st DialogueState) DialogueState {
	st.Lane = entry.lane
	st.Kind = string(sessionKindReview)
	var view chatsession.View[*reviewQueueItem]
	if s := e.laneSession(entry.runID, entry.nodeID, entry.lane); s != nil {
		view = s.View()
		st.Kind = string(s.kind)
		if s.linkID != "" {
			entry.linkID = s.linkID
		}
	}
	persisted := e.VisitorTurns(entry.linkID, entry.lane, entry.runID, entry.nodeID)
	agentIdx := map[string]int{}
	for i, m := range persisted {
		row := DialogueRow{ReactMessage: m, Phase: rowSealed}
		row.ID = dialogueRowID(0, i, m.TurnID, m.Role, agentIdx)
		if m.Role == "agent" {
			row.Outcome = models.ReplyOutcome(m)
		}
		st.Rows = append(st.Rows, row)
	}
	if view.HasActive {
		e.appendActiveTurn(entry, &st, view.Active, view.LiveEvents, persisted, agentIdx)
	}
	appendQueuedTurns(&st, view.Pending)
	st.Busy = view.HasActive || len(view.Pending) > 0
	for _, et := range entry.ended {
		st.Turns = append(st.Turns, endedDialogueTurn(et, st.Rows))
	}
	return st
}

func appendQueuedTurns(st *DialogueState, pending []*reviewQueueItem) {
	for i, it := range pending {
		pos := i
		v := reviewItemView(it)
		st.Turns = append(st.Turns, DialogueTurn{TurnID: it.ID, ClientMessageID: it.ClientMessageID,
			Phase: turnQueued, Position: &pos, Item: v.Frame()})
	}
}

// appendActiveTurn adds the running turn: its human row until the turn has
// persisted one, and its reply rows from the live stream until it has
// persisted those.
func (e *Engine) appendActiveTurn(entry *dialogueEntry, st *DialogueState, active *reviewQueueItem, live []models.AcpEvent, persisted []models.ReactMessage, agentIdx map[string]int) {
	started := entry.started[active.ID]
	if started == "" {
		started = time.Now().Format(time.RFC3339)
		if entry.started == nil {
			entry.started = map[string]string{}
		}
		entry.started[active.ID] = started
	}
	v := reviewItemView(active)
	turn := DialogueTurn{TurnID: active.ID, ClientMessageID: active.ClientMessageID, Phase: turnRunning, Item: v.Frame()}

	if !hasTurnHuman(persisted, active.ID) && !rowsHaveTurnHuman(st.Rows, active.ID) {
		st.Rows = append(st.Rows, DialogueRow{
			ReactMessage: models.ReactMessage{Role: "human", Text: active.Text, At: started, TurnID: active.ID,
				ClientMessageID: active.ClientMessageID, Images: active.Images, Annotations: active.Annotations,
				Live: active.Live, GateNodeID: gateTagOf(active)},
			ID: active.ID + ":h", Phase: rowSealed,
		})
	}
	if turnHasReply(persisted, active.ID) {
		st.Turns = append(st.Turns, turn)
		return
	}
	if dialogueFaults.sandboxWhileBusy.Load() && entry.sandboxDropped {
		// Fault: the bridge's idle flag closed the platform's turn.
		turn.Phase = turnEnded
		turn.Outcome = models.OutcomeEmpty
		st.Rows = append(st.Rows, DialogueRow{
			ReactMessage: models.ReactMessage{Role: "agent", At: started, TurnID: active.ID,
				ClientMessageID: active.ClientMessageID, Outcome: models.OutcomeEmpty},
			ID: active.ID + ":a" + fmt.Sprint(agentIdx[active.ID]), Phase: rowSealed,
		})
		st.Turns = append(st.Turns, turn)
		return
	}
	for _, r := range models.LiveReplyRows(live) {
		k := agentIdx[active.ID]
		agentIdx[active.ID] = k + 1
		row := DialogueRow{
			ReactMessage: models.ReactMessage{Role: "agent", Text: r.Text, At: started, TurnID: active.ID,
				ClientMessageID: active.ClientMessageID, Handoff: r.Handoff, Tools: r.Tools, Parts: r.Parts,
				GateNodeID: gateTagOf(active)},
			ID: fmt.Sprintf("%s:a%d", active.ID, k), Phase: rowRunning, Thought: r.Thought, Streaming: !r.Handoff,
		}
		st.Rows = append(st.Rows, row)
	}
	st.Turns = append(st.Turns, turn)
}

// attachOrphan merges the sandbox bridge's view. While the platform runs or
// queues a turn the bridge is not consulted at all: its busy flag flips
// between a turn's own sandbox calls and must never decide that turn's phase.
func (e *Engine) attachOrphan(entry *dialogueEntry, st *DialogueState, runID, nodeID string, platformBusy bool, agentIdx map[string]int) {
	if platformBusy {
		entry.idleEvents = nil
		return
	}
	entry.sandboxSeenBusy, entry.sandboxDropped = false, false
	bs, ok := e.SandboxBridgeState(runID, nodeID)
	if !ok {
		entry.idleEvents = nil
		return
	}
	bs = e.dropCompletedSandboxEcho(runID, nodeID, bs)
	if !(bs.Busy || bs.Desynced) {
		entry.idleEvents = nil
		return
	}
	op := strings.TrimSpace(bs.RunningOpID)
	st.Orphan = &DialogueOrphan{RunningOpID: op, Desynced: bs.Desynced}
	turnID := "orphan:" + op
	st.Turns = append(st.Turns, DialogueTurn{TurnID: turnID, Phase: turnOrphan, RunningOpID: op})
	if len(entry.idleEvents) == 0 {
		return
	}
	cmid := ""
	for i := len(st.Rows) - 1; i >= 0; i-- {
		if st.Rows[i].Role == "human" {
			cmid = st.Rows[i].ClientMessageID
			break
		}
	}
	for _, r := range models.LiveReplyRows(entry.idleEvents) {
		k := agentIdx[turnID]
		agentIdx[turnID] = k + 1
		st.Rows = append(st.Rows, DialogueRow{
			ReactMessage: models.ReactMessage{Role: "agent", Text: r.Text, TurnID: turnID, ClientMessageID: cmid,
				Handoff: r.Handoff, Tools: r.Tools, Parts: r.Parts},
			ID: fmt.Sprintf("%s:a%d", turnID, k), Phase: rowOrphan, Thought: r.Thought, Streaming: !r.Handoff,
		})
	}
}

// faultProbeSandbox is the root-cause fault: it records the bridge flag while
// the platform is busy, so a busy→idle flip can close the running turn.
func (e *Engine) faultProbeSandbox(entry *dialogueEntry, runID, nodeID string) {
	bs, ok := e.SandboxBridgeState(runID, nodeID)
	if ok && (bs.Busy || bs.Desynced) {
		entry.sandboxSeenBusy = true
	} else if entry.sandboxSeenBusy {
		entry.sandboxDropped = true
	}
}

// buildObserved is an automatic visit nobody has taken over: one reply row
// fed by the node's stream while it runs, sealed from its saved events after.
func (e *Engine) buildObserved(entry *dialogueEntry, st *DialogueState, sr *models.StateRun) {
	st.Observe = true
	st.Iteration = sr.Iteration
	if entry.observed != sr.Iteration {
		entry.observed = sr.Iteration
		entry.iteration = sr.Iteration
		entry.ended = nil
		entry.idleEvents = nil
	}
	turnID := fmt.Sprintf("auto:%d", sr.Iteration)
	running := sr.Status == "running"
	events := entry.idleEvents
	if !running && len(sr.Events) > 0 {
		events = sr.Events
	}
	turn := DialogueTurn{TurnID: turnID, Phase: turnRunning}
	outcome := ""
	if !running {
		turn.Phase = turnEnded
		switch sr.Status {
		case "failed":
			outcome = models.OutcomeFailed
			turn.Error = sr.Error
		case "cancelled", "interrupted":
			outcome = models.OutcomeInterrupted
		default:
			outcome = models.OutcomeOK
		}
		turn.Outcome = outcome
	}
	st.Busy = running
	at := ""
	if sr.StartedAt != nil {
		at = sr.StartedAt.Format(time.RFC3339)
	}
	rows := models.LiveReplyRows(events)
	for k, r := range rows {
		row := DialogueRow{
			ReactMessage: models.ReactMessage{Role: "agent", Text: r.Text, At: at, TurnID: turnID,
				Handoff: r.Handoff, Tools: r.Tools, Parts: r.Parts},
			ID: fmt.Sprintf("%s:a%d", turnID, k), Phase: rowRunning, Thought: r.Thought, Streaming: running && !r.Handoff,
		}
		if !running {
			row.Phase = rowSealed
			row.Outcome = models.OutcomeOK
			if k == len(rows)-1 {
				row.Outcome = outcome
				row.Error = turn.Error
			}
		}
		st.Rows = append(st.Rows, row)
	}
	st.Turns = append(st.Turns, turn)
}

// nodeIsAgent reads only the run graph to tell whether nodeID is an agent
// node (the only kind whose automatic visit has a transcript to observe).
func (e *Engine) nodeIsAgent(runID, nodeID string) bool {
	var run models.Run
	if err := e.db.Select("id", "graph").First(&run, "id = ?", runID).Error; err != nil {
		return false
	}
	n := run.Graph.FindNode(nodeID)
	return n != nil && n.Type == "agent"
}

func endedDialogueTurn(et endedTurn, rows []DialogueRow) DialogueTurn {
	t := DialogueTurn{TurnID: et.turnID, ClientMessageID: et.clientMessageID, Phase: turnEnded,
		Code: et.code, AutoRetryOnResume: et.autoRetry}
	if et.code == clarifyVisitEndedCode {
		t.Outcome = models.OutcomeDropped
		return t
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].TurnID == et.turnID && rows[i].Role == "agent" {
			t.Outcome = rows[i].Outcome
			t.Error = rows[i].Error
			return t
		}
	}
	if et.errMsg != "" {
		t.Outcome = models.OutcomeFailed
		t.Error = et.errMsg
	} else {
		t.Outcome = models.OutcomeInterrupted
	}
	return t
}

// dialogueRowID is a row's stable id: turn id plus role (and reply index)
// for rows that know their turn, the transcript position otherwise.
func dialogueRowID(iteration, index int, turnID, role string, agentIdx map[string]int) string {
	if turnID == "" {
		return fmt.Sprintf("i%d:%d", iteration, index)
	}
	if role == "human" {
		return turnID + ":h"
	}
	k := agentIdx[turnID]
	agentIdx[turnID] = k + 1
	return fmt.Sprintf("%s:a%d", turnID, k)
}

func hasTurnHuman(msgs []models.ReactMessage, turnID string) bool {
	for _, m := range msgs {
		if m.Role == "human" && m.TurnID == turnID {
			return true
		}
	}
	return false
}

func rowsHaveTurnHuman(rows []DialogueRow, turnID string) bool {
	for _, r := range rows {
		if r.Role == "human" && r.TurnID == turnID {
			return true
		}
	}
	return false
}

func turnHasReply(msgs []models.ReactMessage, turnID string) bool {
	for _, m := range msgs {
		if m.Role == "agent" && m.TurnID == turnID && !m.Handoff {
			return true
		}
	}
	return false
}

func gateTagOf(it *reviewQueueItem) string {
	if it.Source == "gate" {
		return it.GateNodeID
	}
	return ""
}

// dialoguePrefixDigest names a row prefix by its ids and revisions (FNV-1a
// 32 over "id@rev|id@rev…"); the web client computes the same digest.
func dialoguePrefixDigest(rows []DialogueRow) string {
	h := fnv.New32a()
	for i, r := range rows {
		if i > 0 {
			_, _ = h.Write([]byte("|"))
		}
		_, _ = fmt.Fprintf(h, "%s@%d", r.ID, r.Rev)
	}
	return fmt.Sprintf("%d:%08x", len(rows), h.Sum32())
}
