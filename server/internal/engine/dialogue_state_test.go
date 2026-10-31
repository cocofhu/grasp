package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// dialogueRecorder collects every dialogue frame broadcast for one run.
type dialogueRecorder struct {
	mu     sync.Mutex
	frames []DialogueState
	stop   func()
	done   chan struct{}
}

func recordDialogues(eng *Engine, runID string) *dialogueRecorder {
	ch, unsub := eng.broker.Subscribe(runID)
	r := &dialogueRecorder{stop: unsub, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for b := range ch {
			var head struct{ Type string }
			if json.Unmarshal(b, &head) != nil || head.Type != "dialogue" {
				continue
			}
			var st DialogueState
			if json.Unmarshal(b, &st) == nil {
				r.mu.Lock()
				r.frames = append(r.frames, st)
				r.mu.Unlock()
			}
		}
	}()
	return r
}

func (r *dialogueRecorder) close() []DialogueState {
	r.stop()
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]DialogueState(nil), r.frames...)
}

func findTurn(st DialogueState, id string) *DialogueTurn {
	for i := range st.Turns {
		if st.Turns[i].TurnID == id {
			return &st.Turns[i]
		}
	}
	return nil
}

func turnReply(st DialogueState, turnID string) *DialogueRow {
	var out *DialogueRow
	for i := range st.Rows {
		if st.Rows[i].TurnID == turnID && st.Rows[i].Role == "agent" {
			out = &st.Rows[i]
		}
	}
	return out
}

func waitRunningTurn(t *testing.T, eng *Engine, runID, nodeID string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := eng.DialogueSnapshot(runID, nodeID); ok {
			for _, tr := range st.Turns {
				if tr.Phase == turnRunning {
					return tr.TurnID
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no running turn")
	return ""
}

func msgEvents(text string) []models.AcpEvent {
	return []models.AcpEvent{{Kind: "thought", Text: "想一想"}, {Kind: "message", Text: text}}
}

// sandboxFlipScenario is the "本轮没有输出" timing: the platform runs a clarify
// turn while the sandbox bridge's busy flag flips several times and the
// reply keeps streaming. It returns every violation of "the turn stays
// running and its reply only grows until the turn really ends".
func sandboxFlipScenario(t *testing.T) []string {
	t.Helper()
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	hold := make(chan struct{})
	provider.reactHold = hold
	provider.reactPending = 10

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}
	rec := recordDialogues(eng, run.ID)

	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "做登录", nil, nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	turnID := waitRunningTurn(t, eng, run.ID, "clarify")

	var bad []string
	text := ""
	for i := 1; i <= 8; i++ {
		provider.mu.Lock()
		provider.sandboxBusy = i%2 == 1
		provider.sandboxRunningOp = fmt.Sprintf("op-%d", i)
		provider.mu.Unlock()
		text += "字"
		eng.publishAcp(run.ID, "clarify", msgEvents(text), true)
		st, ok := eng.DialogueSnapshot(run.ID, "clarify")
		if !ok {
			bad = append(bad, fmt.Sprintf("step %d: no dialogue", i))
			continue
		}
		tr := findTurn(st, turnID)
		row := turnReply(st, turnID)
		switch {
		case tr == nil || tr.Phase != turnRunning:
			bad = append(bad, fmt.Sprintf("step %d: turn not running: %+v", i, tr))
		case row == nil || row.Phase != rowRunning || row.Text != text:
			bad = append(bad, fmt.Sprintf("step %d: reply row %+v, want running %q", i, row, text))
		case st.Orphan != nil:
			bad = append(bad, fmt.Sprintf("step %d: orphan while platform busy", i))
		}
	}

	provider.mu.Lock()
	provider.sandboxBusy = false
	provider.reactHold = nil
	provider.mu.Unlock()
	close(hold)
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait done: %v", err)
	}
	final, _ := eng.DialogueSnapshot(run.ID, "clarify")
	time.Sleep(2 * dialogueThrottle)
	frames := rec.close()

	if row := turnReply(final, turnID); row == nil || row.Phase != rowSealed || row.Outcome != models.OutcomeOK ||
		row.ID != turnID+":a0" {
		bad = append(bad, fmt.Sprintf("final reply %+v, want sealed ok %s:a0", row, turnID))
	}
	if tr := findTurn(final, turnID); tr == nil || tr.Phase != turnEnded || tr.Outcome != models.OutcomeOK {
		bad = append(bad, fmt.Sprintf("final turn %+v", tr))
	}

	// Every broadcast version: newer than the one before, and the turn never
	// ends before its reply is sealed.
	var last DialogueState
	for i, f := range frames {
		if i > 0 && !last.Version().Less(f.Version()) {
			bad = append(bad, fmt.Sprintf("frame %d version %v not after %v", i, f.Version(), last.Version()))
		}
		last = f
		tr := findTurn(f, turnID)
		if tr == nil || tr.Phase != turnEnded {
			continue
		}
		if f.Mode == "full" {
			if row := turnReply(f, turnID); row == nil || row.Outcome != models.OutcomeOK {
				bad = append(bad, fmt.Sprintf("frame %d: turn ended with reply %+v", i, row))
			}
		}
	}
	if len(frames) == 0 {
		bad = append(bad, "no dialogue frames broadcast")
	}
	return bad
}

func TestDialogueSandboxFlipKeepsTurnRunning(t *testing.T) {
	if bad := sandboxFlipScenario(t); len(bad) > 0 {
		t.Fatalf("dialogue violations:\n%s", strings.Join(bad, "\n"))
	}
}

// startClarifyDialogue opens a clarify run that stays open, idle and ready.
func startClarifyDialogue(t *testing.T) (*Engine, *fakeProvider, string) {
	t.Helper()
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	provider.reactPending = 10
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}
	return eng, provider, run.ID
}

// sendAndSettle sends text and waits for its turn to finish; it returns the
// dialogue and the turn id.
func sendAndSettle(t *testing.T, eng *Engine, runID, text string) (DialogueState, string) {
	t.Helper()
	before, _ := eng.DialogueSnapshot(runID, "clarify")
	if _, err := eng.EnqueueClarifyTurn(runID, "clarify", text, nil, nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := eng.waitReviewReadyForTest(runID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("settle: %v", err)
	}
	st, _ := eng.DialogueSnapshot(runID, "clarify")
	for i := len(st.Rows) - 1; i >= 0; i-- {
		r := st.Rows[i]
		if r.Role == "human" && r.Text == text && !rowsHaveTurnHuman(before.Rows, r.TurnID) {
			return st, r.TurnID
		}
	}
	t.Fatalf("no human row for %q: %+v", text, st.Rows)
	return st, ""
}

func assertEnded(t *testing.T, st DialogueState, turnID, outcome string) *DialogueRow {
	t.Helper()
	tr := findTurn(st, turnID)
	row := turnReply(st, turnID)
	if row == nil || row.Phase != rowSealed || row.Outcome != outcome {
		t.Fatalf("reply %+v, want sealed %s", row, outcome)
	}
	if tr != nil && (tr.Phase != turnEnded || tr.Outcome != outcome) {
		t.Fatalf("turn %+v, want ended %s", tr, outcome)
	}
	if st.Busy {
		t.Fatal("dialogue still busy")
	}
	return row
}

func TestDialogueEmptyReply(t *testing.T) {
	eng, provider, runID := startClarifyDialogue(t)
	provider.mu.Lock()
	provider.reactScript = []runtime.ReactTurn{{Msg: "", Done: false}}
	provider.mu.Unlock()
	st, turnID := sendAndSettle(t, eng, runID, "做登录")
	assertEnded(t, st, turnID, models.OutcomeEmpty)
}

func TestDialogueFailedReply(t *testing.T) {
	eng, provider, runID := startClarifyDialogue(t)
	provider.mu.Lock()
	provider.reactScript = []runtime.ReactTurn{{Msg: "(澄清回复失败:boom)", Err: errors.New("boom")}}
	provider.mu.Unlock()
	st, turnID := sendAndSettle(t, eng, runID, "做登录")
	row := assertEnded(t, st, turnID, models.OutcomeFailed)
	if row.Error != "boom" {
		t.Fatalf("error=%q", row.Error)
	}
}

func TestDialogueOkReplyIsNotAFailure(t *testing.T) {
	eng, _, runID := startClarifyDialogue(t)
	st, turnID := sendAndSettle(t, eng, runID, "做登录")
	assertEnded(t, st, turnID, models.OutcomeOK)
}

func TestDialogueCancelInterrupts(t *testing.T) {
	eng, provider, runID := startClarifyDialogue(t)
	hold := make(chan struct{})
	provider.mu.Lock()
	provider.reactHold = hold
	provider.mu.Unlock()
	if _, err := eng.EnqueueClarifyTurn(runID, "clarify", "做登录", nil, nil); err != nil {
		t.Fatal(err)
	}
	turnID := waitRunningTurn(t, eng, runID, "clarify")
	eng.publishAcp(runID, "clarify", msgEvents("半句"), true)
	if err := eng.CancelClarifyTurn(runID, "clarify"); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	provider.reactHold = nil
	provider.mu.Unlock()
	close(hold)
	if err := eng.waitReviewReadyForTest(runID, "clarify", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	st, _ := eng.DialogueSnapshot(runID, "clarify")
	row := assertEnded(t, st, turnID, models.OutcomeInterrupted)
	if row.ID != turnID+":a0" {
		t.Fatalf("interrupted reply id %q, want the running row's %s:a0", row.ID, turnID)
	}
}

func TestDialogueRetryLastCoversEmptyReply(t *testing.T) {
	eng, provider, runID := startClarifyDialogue(t)
	provider.mu.Lock()
	provider.reactScript = []runtime.ReactTurn{{Msg: "", Done: false}}
	provider.mu.Unlock()
	_, firstID := sendAndSettle(t, eng, runID, "做登录")

	hold := make(chan struct{})
	provider.mu.Lock()
	provider.reactHold = hold
	provider.mu.Unlock()
	if err := eng.ReactReplyRetryLast(runID, "clarify"); err != nil {
		t.Fatal(err)
	}
	retryID := waitRunningTurn(t, eng, runID, "clarify")
	if retryID == firstID {
		t.Fatal("retry reused the first turn id")
	}
	check := func(st DialogueState, wantReply string) {
		t.Helper()
		humans := 0
		for _, r := range st.Rows {
			if r.Role == "human" && r.Text == "做登录" {
				humans++
				if r.ID != retryID+":h" {
					t.Fatalf("retried request row id %q, want %s:h", r.ID, retryID)
				}
			}
			if r.TurnID == firstID {
				t.Fatalf("row of the replaced turn still shown: %+v", r)
			}
			if r.Role == "agent" && r.Outcome == models.OutcomeEmpty {
				t.Fatalf("covered empty reply still shown: %+v", r)
			}
		}
		if humans != 1 {
			t.Fatalf("request rows=%d want 1: %+v", humans, st.Rows)
		}
		if row := turnReply(st, retryID); row == nil || row.Phase != wantReply {
			t.Fatalf("retry reply %+v want %s", row, wantReply)
		}
	}
	running, _ := eng.DialogueSnapshot(runID, "clarify")
	check(running, rowRunning)

	provider.mu.Lock()
	provider.reactHold = nil
	provider.mu.Unlock()
	close(hold)
	if err := eng.waitReviewReadyForTest(runID, "clarify", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	final, _ := eng.DialogueSnapshot(runID, "clarify")
	check(final, rowSealed)
	assertEnded(t, final, retryID, models.OutcomeOK)
}

func TestDialogueVisitEndedTurnIsDropped(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, clarifyOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	hold := make(chan struct{})
	p.mu.Lock()
	p.reactHold = hold
	p.mu.Unlock()
	if err := eng.ReactReply(run.ID, "predev", "第一条", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	waitClarifyReplyStarted(t, p, "predev")
	const cid = "cid-dropped"
	if err := eng.ReactReplyLiveCtxWithIDAs("user:a", run.ID, "predev", cid, "排队的第二条", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	queued, _ := eng.DialogueSnapshot(run.ID, "predev")
	var queuedTurn *DialogueTurn
	for i := range queued.Turns {
		if queued.Turns[i].ClientMessageID == cid {
			queuedTurn = &queued.Turns[i]
		}
	}
	if queuedTurn == nil || queuedTurn.Phase != turnQueued || queuedTurn.Position == nil || *queuedTurn.Position != 0 {
		t.Fatalf("queued turn %+v", queuedTurn)
	}
	wait := collectVisitEndedFrame(t, eng, run.ID)
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	close(hold)
	wait()

	deadline := time.Now().Add(3 * time.Second)
	for {
		st, _ := eng.DialogueSnapshot(run.ID, "predev")
		tr := findTurn(st, queuedTurn.TurnID)
		if tr != nil && tr.Phase == turnEnded {
			if tr.Outcome != models.OutcomeDropped || tr.Code != clarifyVisitEndedCode || tr.ClientMessageID != cid {
				t.Fatalf("dropped turn %+v", tr)
			}
			for _, r := range st.Rows {
				if r.TurnID == queuedTurn.TurnID {
					t.Fatalf("dropped turn has a row: %+v", r)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("dropped turn never ended: %+v", st.Turns)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDialogueOrphanTurn(t *testing.T) {
	eng, provider, runID := startClarifyDialogue(t)
	provider.mu.Lock()
	provider.sandboxBusy = true
	provider.sandboxRunningOp = "op-9"
	provider.mu.Unlock()
	eng.publishAcp(runID, "clarify", msgEvents("孤儿输出"), true)

	st, _ := eng.DialogueSnapshot(runID, "clarify")
	if st.Orphan == nil || st.Orphan.RunningOpID != "op-9" {
		t.Fatalf("orphan=%+v", st.Orphan)
	}
	tr := findTurn(st, "orphan:op-9")
	row := turnReply(st, "orphan:op-9")
	if tr == nil || tr.Phase != turnOrphan || row == nil || row.Phase != rowOrphan || row.Text != "孤儿输出" {
		t.Fatalf("orphan turn %+v row %+v", tr, row)
	}

	provider.mu.Lock()
	provider.sandboxBusy = false
	provider.sandboxRunningOp = ""
	provider.mu.Unlock()
	eng.publishAcp(runID, "clarify", nil, false)
	st, _ = eng.DialogueSnapshot(runID, "clarify")
	if st.Orphan != nil || findTurn(st, "orphan:op-9") != nil || turnReply(st, "orphan:op-9") != nil {
		t.Fatalf("orphan kept after the sandbox went idle: %+v", st)
	}
}

// An automatic visit nobody took over is observed: one running reply row fed
// by the node's stream, then sealed when the visit ends.
func TestDialogueObservesAutomaticVisit(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, env.provider)
	rec := recordDialogues(env.eng, run.ID)
	env.eng.publishAcp(run.ID, "impl", msgEvents("正在实现"), true)

	st, ok := env.eng.DialogueSnapshot(run.ID, "impl")
	if !ok || !st.Observe || !st.Busy {
		t.Fatalf("observe state %+v ok=%v", st, ok)
	}
	turnID := fmt.Sprintf("auto:%d", st.Iteration)
	row := turnReply(st, turnID)
	if tr := findTurn(st, turnID); tr == nil || tr.Phase != turnRunning || row == nil || row.Phase != rowRunning || row.Text != "正在实现" {
		t.Fatalf("observed turn %+v row %+v", findTurn(st, turnID), row)
	}

	env.provider.releaseHold("impl")
	waitRunStatus(t, env.db, run.ID, "completed")
	time.Sleep(2 * dialogueThrottle)
	frames := rec.close()
	if len(frames) == 0 {
		t.Fatal("no observe frames")
	}
	last := frames[len(frames)-1]
	tr := findTurn(last, turnID)
	row = turnReply(last, turnID)
	if !last.Observe || last.Busy || tr == nil || tr.Phase != turnEnded || tr.Outcome != models.OutcomeOK ||
		row == nil || row.Phase != rowSealed || row.ID != turnID+":a0" {
		t.Fatalf("ended observe frame %+v", last)
	}
	// A visit nobody watched while it ran is not served as a dialogue.
	if _, ok := env.eng.DialogueSnapshot(run.ID, "input"); ok {
		t.Fatal("input node has a dialogue")
	}
}

// The scenario must catch the root cause when it is put back.
func TestDialogueSandboxFlipFaultIsCaught(t *testing.T) {
	faultSandboxWhileBusy(true)
	t.Cleanup(func() { faultSandboxWhileBusy(false) })
	if bad := sandboxFlipScenario(t); len(bad) == 0 {
		t.Fatal("sandboxWhileBusy fault went unnoticed")
	}
}
