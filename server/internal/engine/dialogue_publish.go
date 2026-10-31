package engine

import (
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
)

// dialogueThrottle bounds content-only frames (a streaming reply) per
// dialogue. Phase changes are published at once.
const dialogueThrottle = 150 * time.Millisecond

// dialogueIdleTTL drops a dialogue entry nobody built or read for this long
// while no session runs it. The next read rebuilds it under a newer version.
const dialogueIdleTTL = 30 * time.Minute

type dialogueHub struct {
	mu        sync.Mutex
	entries   map[string]*dialogueEntry
	lastSweep time.Time
}

type rowStamp struct {
	hash [32]byte
	rev  uint64
}

// dialogueEntry is one node dialogue's versioning and publish state.
type dialogueEntry struct {
	mu     sync.Mutex
	runID  string
	nodeID string
	// lane is a share visitor's own chat ("" for the node dialogue); linkID
	// is the share link that lane belongs to.
	lane   string
	linkID string
	// used is when the entry was last looked up (guarded by dialogueHub.mu).
	used time.Time
	// evicted: the hub no longer holds this entry; it never publishes again.
	evicted bool

	cur       *DialogueState
	curHash   [32]byte
	stamps    map[string]rowStamp
	sent      *DialogueState
	lastSent  time.Time
	timer     *time.Timer
	iteration int
	// observed is the automatic visit this dialogue last showed (0: none).
	observed int
	// started is when each running turn was first seen (stable row times).
	started map[string]string
	// idleEvents is the node's stream while the platform tracks no turn: an
	// observed automatic visit, or an orphan sandbox turn.
	idleEvents []models.AcpEvent
	ended      []endedTurn
}

func dialogueKey(runID, nodeID, lane string) string { return runID + "|" + nodeID + "|" + lane }

const dialogueEpochSetting = "dialogue.epoch"

type settingEpochStore struct{ db *gorm.DB }

func (s settingEpochStore) LoadEpoch() (int64, bool) {
	var row models.Setting
	if err := s.db.First(&row, "key = ?", dialogueEpochSetting).Error; err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(row.Value, 10, 64)
	return v, err == nil
}

func (s settingEpochStore) SaveEpoch(v int64) error {
	return s.db.Save(&models.Setting{Key: dialogueEpochSetting, Value: strconv.FormatInt(v, 10), UpdatedAt: time.Now()}).Error
}

// InitDialogueEpoch fixes this process's dialogue version epoch above every
// earlier boot's. Call once at startup, before runs are served.
func (e *Engine) InitDialogueEpoch() error {
	return chatsession.InitDialogueEpoch(settingEpochStore{db: e.db})
}

func (e *Engine) dialogueHub() *dialogueHub {
	e.dialogueOnce.Do(func() { e.dialogues = &dialogueHub{entries: map[string]*dialogueEntry{}} })
	return e.dialogues
}

func (e *Engine) dialogueEntry(runID, nodeID string) *dialogueEntry {
	return e.laneDialogueEntry(runID, nodeID, "", "")
}

func (e *Engine) laneDialogueEntry(runID, nodeID, lane, linkID string) *dialogueEntry {
	h := e.dialogueHub()
	now := time.Now()
	h.mu.Lock()
	if now.Sub(h.lastSweep) > dialogueIdleTTL/6 {
		h.lastSweep = now
		e.sweepDialoguesLocked(h, now)
	}
	k := dialogueKey(runID, nodeID, lane)
	en := h.entries[k]
	if en == nil {
		en = &dialogueEntry{runID: runID, nodeID: nodeID, lane: lane, started: map[string]string{}}
		h.entries[k] = en
	}
	en.used = now
	h.mu.Unlock()
	if linkID != "" {
		en.mu.Lock()
		en.linkID = linkID
		en.mu.Unlock()
	}
	return en
}

// sweepDialoguesLocked drops entries idle past dialogueIdleTTL whose session
// is gone. Callers hold h.mu.
func (e *Engine) sweepDialoguesLocked(h *dialogueHub, now time.Time) {
	for k, en := range h.entries {
		if !(en.used.IsZero() || now.Sub(en.used) > dialogueIdleTTL) || e.laneSession(en.runID, en.nodeID, en.lane) != nil {
			continue
		}
		// An entry being built or published right now is in use.
		if en.mu.TryLock() {
			en.evictLocked()
			en.mu.Unlock()
			delete(h.entries, k)
		}
	}
}

// evictLocked retires en before the hub forgets it, so a frame it was about
// to send cannot follow a newer one from its replacement. Callers hold en.mu.
func (en *dialogueEntry) evictLocked() {
	en.evicted = true
	if en.timer != nil {
		en.timer.Stop()
		en.timer = nil
	}
}

// forgetDialogues drops every dialogue entry of runID (lane "" too when lane
// is "*", else only that lane). Versions stay monotonic: a rebuilt entry is
// stamped from the process-wide counter. Ended turns without a persisted row
// (dropped, visit_ended) live only in the entry, so a rebuilt state no longer
// lists them. Viewers act on an ended turn when it first appears, and entries
// are only dropped long after that.
func (e *Engine) forgetDialogues(runID, nodeID, lane string) {
	h := e.dialogueHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, en := range h.entries {
		if en.runID == runID && (nodeID == "" || en.nodeID == nodeID) && (lane == "*" || en.lane == lane) {
			en.mu.Lock()
			en.evictLocked()
			en.mu.Unlock()
			delete(h.entries, k)
		}
	}
}

// expireDialogues lets the next sweep drop runID's entries (a finished run)
// unless they are looked up again first.
func (e *Engine) expireDialogues(runID string) {
	h := e.dialogueHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, en := range h.entries {
		if en.runID == runID {
			en.used = time.Time{}
		}
	}
}

// knownDialogueNodes lists nodes of runID whose dialogue was built before.
func (e *Engine) knownDialogueNodes(runID string) []string {
	h := e.dialogueHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, en := range h.entries {
		if en.runID == runID && en.lane == "" {
			out = append(out, en.nodeID)
		}
	}
	return out
}

// stampDialogue assigns st a version: the current one when nothing changed,
// a new one otherwise. Rows keep the revision of their last content change.
func stampDialogue(en *dialogueEntry, st DialogueState) DialogueState {
	normalizeDialogueQuestions(st.Rows)
	hashes := make([][32]byte, len(st.Rows))
	for i := range st.Rows {
		st.Rows[i].Rev = 0
		b, _ := json.Marshal(st.Rows[i])
		hashes[i] = sha256.Sum256(b)
	}
	st.Epoch, st.Seq, st.Mode, st.PrefixLen, st.PrefixDigest = 0, 0, "full", 0, ""
	whole, _ := json.Marshal(st)
	h := sha256.Sum256(whole)
	if en.cur != nil && h == en.curHash {
		return *en.cur
	}
	v := chatsession.NextDialogueVersion()
	if h := dialogueStampHook.Load(); h != nil {
		v = (*h)(en, v)
	}
	st.Epoch, st.Seq = v.Epoch, v.Seq
	next := make(map[string]rowStamp, len(st.Rows))
	for i := range st.Rows {
		rev := v.Seq
		if old, ok := en.stamps[st.Rows[i].ID]; ok && old.hash == hashes[i] {
			rev = old.rev
		}
		st.Rows[i].Rev = rev
		next[st.Rows[i].ID] = rowStamp{hash: hashes[i], rev: rev}
	}
	en.stamps = next
	en.curHash = h
	cp := st
	en.cur = &cp
	return st
}

// normalizeDialogueQuestions sends a question without options as an empty
// list, never null: the chat reads question.options as an array.
func normalizeDialogueQuestions(rows []DialogueRow) {
	for i := range rows {
		qs := rows[i].Questions
		for j := range qs {
			if qs[j].Options != nil {
				continue
			}
			cp := append([]models.ReactQuestion(nil), qs...)
			for k := range cp {
				if cp[k].Options == nil {
					cp[k].Options = []models.ReactOption{}
				}
			}
			rows[i].Questions = cp
			break
		}
	}
}

// DialogueSnapshot is the node's current full dialogue (REST and reconnect).
func (e *Engine) DialogueSnapshot(runID, nodeID string) (DialogueState, bool) {
	return e.snapshotDialogue(e.dialogueEntry(runID, nodeID))
}

// LaneDialogueSnapshot is a share visitor lane's current full dialogue.
func (e *Engine) LaneDialogueSnapshot(runID, nodeID, lane, linkID string) (DialogueState, bool) {
	return e.snapshotDialogue(e.laneDialogueEntry(runID, nodeID, lane, linkID))
}

func (e *Engine) snapshotDialogue(en *dialogueEntry) (DialogueState, bool) {
	en.mu.Lock()
	st, ok := e.buildDialogue(en, en.runID, en.nodeID)
	if !ok {
		en.mu.Unlock()
		return DialogueState{}, false
	}
	st = stampDialogue(en, st)
	behind := en.sent == nil || en.sent.Seq != st.Seq || en.sent.Epoch != st.Epoch
	en.mu.Unlock()
	if behind {
		// A REST read noticed a change first; viewers get it as well.
		go e.publishDialogue(en)
	}
	return st, true
}

// dialogueCandidates is every node of runID that may have a dialogue: nodes
// with a conversation and agent nodes running right now.
func (e *Engine) dialogueCandidates(runID string) []string {
	var ids []string
	e.db.Model(&models.ReactConversation{}).Where("run_id = ?", runID).Distinct().Pluck("node_id", &ids)
	var running []string
	e.db.Model(&models.StateRun{}).Where("run_id = ? AND status = ? AND node_type = ?", runID, "running", "agent").
		Distinct().Pluck("node_id", &running)
	return append(ids, running...)
}

// DialoguesForRun is every node dialogue of runID.
func (e *Engine) DialoguesForRun(runID string) map[string]DialogueState {
	seen := map[string]bool{}
	var ids []string
	for _, id := range append(e.dialogueCandidates(runID), e.knownDialogueNodes(runID)...) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	e.reviewSess.Range(func(_ string, s *reviewSession) bool {
		if s != nil && s.runID == runID && s.lane == "" && !seen[s.producerID] {
			seen[s.producerID] = true
			ids = append(ids, s.producerID)
		}
		return true
	})
	sort.Strings(ids)
	out := map[string]DialogueState{}
	for _, id := range ids {
		if st, ok := e.DialogueSnapshot(runID, id); ok {
			out[id] = st
		}
	}
	return out
}

// SendDialogues writes a full dialogue frame per node of runID to one new
// connection, so a reconnect needs no other source to rebuild the chat.
func (e *Engine) SendDialogues(runID string, write func([]byte) error) error {
	all := e.DialoguesForRun(runID)
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b, err := json.Marshal(all[id])
		if err != nil {
			return err
		}
		if err := write(b); err != nil {
			return err
		}
	}
	return nil
}

// dialogueChanged schedules a publish of the node dialogue. urgent is for a
// phase change (enqueue, start, end, orphan); content-only changes coalesce.
func (e *Engine) dialogueChanged(runID, nodeID string, urgent bool) {
	e.dialogueChangedLane(runID, nodeID, "", urgent)
}

func (e *Engine) dialogueChangedLane(runID, nodeID, lane string, urgent bool) {
	if runID == "" || nodeID == "" {
		return
	}
	en := e.laneDialogueEntry(runID, nodeID, lane, "")
	if urgent {
		e.publishDialogue(en)
		return
	}
	en.mu.Lock()
	wait := dialogueThrottle - time.Since(en.lastSent)
	if wait <= 0 && en.timer == nil {
		en.mu.Unlock()
		e.publishDialogue(en)
		return
	}
	if en.timer == nil {
		if wait < 0 {
			wait = 0
		}
		en.timer = time.AfterFunc(wait, func() {
			en.mu.Lock()
			en.timer = nil
			en.mu.Unlock()
			e.publishDialogue(en)
		})
	}
	en.mu.Unlock()
}

// publishDialogue builds the dialogue and broadcasts it when its version is
// newer than the last broadcast: a tail when only rows after an unchanged
// sealed prefix moved, else the full state.
func (e *Engine) publishDialogue(en *dialogueEntry) {
	en.mu.Lock()
	if en.evicted {
		en.mu.Unlock()
		// The change still has to reach viewers: through the current entry.
		e.publishDialogue(e.laneDialogueEntry(en.runID, en.nodeID, en.lane, ""))
		return
	}
	defer en.mu.Unlock()
	st, ok := e.buildDialogue(en, en.runID, en.nodeID)
	if !ok {
		return
	}
	st = stampDialogue(en, st)
	if en.sent != nil && !en.sent.Version().Less(st.Version()) {
		return
	}
	frame := dialogueFrame(en.sent, st)
	b, err := json.Marshal(frame)
	if err != nil {
		return
	}
	full := st
	en.sent = &full
	en.lastSent = time.Now()
	e.broker.Publish(en.runID, b)
}

// dialogueFrame is st as sent after prev: the rows after the longest sealed
// prefix both share, when there is one.
func dialogueFrame(prev *DialogueState, st DialogueState) DialogueState {
	if prev == nil || prev.Iteration != st.Iteration {
		return st
	}
	n := 0
	for n < len(prev.Rows) && n < len(st.Rows) {
		a, b := prev.Rows[n], st.Rows[n]
		if a.ID != b.ID || a.Rev != b.Rev || b.Phase != rowSealed {
			break
		}
		n++
	}
	if n == 0 {
		return st
	}
	out := st
	out.Mode = "tail"
	out.PrefixLen = n
	out.PrefixDigest = dialoguePrefixDigest(st.Rows[:n])
	out.Rows = append([]DialogueRow(nil), st.Rows[n:]...)
	return out
}

// noteDialogueControl reacts to a session control frame: every one is a
// phase change, and a turn that ends is remembered for turns without rows.
func (e *Engine) noteDialogueControl(runID, nodeID, lane, event string, payload map[string]any) {
	if event == "turn_done" || event == "error" {
		en := e.laneDialogueEntry(runID, nodeID, lane, "")
		item, _ := payload["item"].(map[string]any)
		id, _ := item["id"].(string)
		if id != "" {
			cmid, _ := item["clientMessageId"].(string)
			code, _ := payload["code"].(string)
			auto, _ := payload["autoRetryOnResume"].(bool)
			msg := ""
			if event == "error" {
				msg, _ = payload["message"].(string)
			}
			en.mu.Lock()
			en.ended = append(en.ended, endedTurn{turnID: id, clientMessageID: cmid, code: code, autoRetry: auto, errMsg: msg})
			if len(en.ended) > maxEndedTurns {
				en.ended = en.ended[len(en.ended)-maxEndedTurns:]
			}
			delete(en.started, id)
			en.mu.Unlock()
		}
	}
	e.dialogueChangedLane(runID, nodeID, lane, true)
}

// noteDialogueAcp routes a node's stream: onto the platform's running turn
// when there is one, else into the dialogue's idle stream (observed visit or
// orphan sandbox turn). The builder drops an orphan's stream once the bridge
// reports idle; an observed visit keeps its last snapshot until it ends.
func (e *Engine) noteDialogueAcp(runID, nodeID string, events []models.AcpEvent, busy bool) {
	if !hasDialogueContent(events) {
		if !busy {
			e.dialogueChanged(runID, nodeID, true)
		}
		return
	}
	if s := e.laneSession(runID, nodeID, ""); s != nil && s.SetLiveEventsIfActive(events) {
		e.dialogueChanged(runID, nodeID, false)
		return
	}
	en := e.dialogueEntry(runID, nodeID)
	en.mu.Lock()
	opened := len(en.idleEvents) == 0
	en.idleEvents = events
	en.mu.Unlock()
	e.dialogueChanged(runID, nodeID, opened || !busy)
}

// publishReact announces a conversation save: the legacy "react" refetch
// hint, then the node dialogue it changed.
func (e *Engine) publishReact(runID, nodeID string) {
	e.broker.Publish(runID, jsonMsg("react", runID, nodeID))
	e.dialogueChanged(runID, nodeID, true)
}

// sealReplyOutcome stamps how a finished turn's reply ended. An interrupted
// turn stays interrupted even when the provider returned an error with it.
func sealReplyOutcome(m *models.ReactMessage, err error) {
	if err != nil && !m.Interrupted {
		m.Outcome = models.OutcomeFailed
		m.Error = err.Error()
		return
	}
	m.Outcome = models.ReplyOutcome(*m)
}

// hasDialogueContent is false for watchdog heartbeats, which carry no reply.
func hasDialogueContent(events []models.AcpEvent) bool {
	for _, ev := range events {
		if ev.Kind != models.AcpKindLiveness {
			return true
		}
	}
	return false
}

// dialogueNodeEvent republishes a node's dialogue when its visit moves
// (enter / exit / takeover) so observed and closed chats settle.
func (e *Engine) dialogueNodeEvent(runID, nodeID string) {
	if strings.TrimSpace(nodeID) == "" {
		return
	}
	e.dialogueChanged(runID, nodeID, true)
}
