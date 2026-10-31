package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// connectDialogues is what a browser tab receives on (re)connect: the full
// frames handlers/ws.go writes through SendDialogues.
func connectDialogues(t *testing.T, eng *Engine, runID, nodeID string) DialogueState {
	t.Helper()
	var got []DialogueState
	if err := eng.SendDialogues(runID, func(b []byte) error {
		var st DialogueState
		if err := json.Unmarshal(b, &st); err != nil {
			return err
		}
		if st.NodeID == nodeID {
			got = append(got, st)
		}
		return nil
	}); err != nil {
		t.Fatalf("send dialogues: %v", err)
	}
	if len(got) != 1 || got[0].Mode != "full" {
		t.Fatalf("connect frames %+v, want one full frame", got)
	}
	return got[0]
}

// tab mirrors dialogueState.ts applyFrame: it keeps the newest version, takes
// full frames whole and tail frames only on a matching prefix.
type tab struct {
	st     DialogueState
	have   bool
	resync int
}

func (tb *tab) apply(f DialogueState) {
	if tb.have && !tb.st.Version().Less(f.Version()) {
		return
	}
	if f.Mode == "full" {
		tb.st, tb.have = f, true
		return
	}
	if !tb.have || f.PrefixLen > len(tb.st.Rows) || dialoguePrefixDigest(tb.st.Rows[:f.PrefixLen]) != f.PrefixDigest {
		tb.resync++
		return
	}
	rows := append(append([]DialogueRow(nil), tb.st.Rows[:f.PrefixLen]...), f.Rows...)
	f.Rows, f.Mode = rows, "full"
	tb.st = f
}

func rowsSummary(rows []DialogueRow) string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = fmt.Sprintf("%s/%s/%s/%q", r.ID, r.Phase, r.Outcome, r.Text)
	}
	return strings.Join(parts, " ")
}

// TestDialogueAcceptanceFlipReconnectRestart is the g5.3 walk-through without
// a real sandbox: the sandbox busy flag flips while the platform streams a
// clarify reply, the tab's socket drops mid-turn and reconnects, the turn
// ends, and the server restarts on the same database. The tab never sees the
// turn end early or a failure card, and every reconnect brings it up to date
// without a page reload.
func TestDialogueAcceptanceFlipReconnectRestart(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	if err := eng.InitDialogueEpoch(); err != nil {
		t.Fatal(err)
	}
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

	var view tab
	view.apply(connectDialogues(t, eng, run.ID, "clarify"))
	socket := recordDialogues(eng, run.ID)

	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "做登录", nil, nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	turnID := waitRunningTurn(t, eng, run.ID, "clarify")

	var bad []string
	check := func(where string, st DialogueState) {
		tr := findTurn(st, turnID)
		row := turnReply(st, turnID)
		if tr != nil && tr.Phase == turnEnded {
			bad = append(bad, fmt.Sprintf("%s: turn ended while the platform is busy: %+v", where, tr))
		}
		if row != nil && (row.Outcome == models.OutcomeEmpty || row.Outcome == models.OutcomeFailed) {
			bad = append(bad, fmt.Sprintf("%s: failure card %+v", where, row))
		}
		if st.Orphan != nil {
			bad = append(bad, where+": orphan banner while the platform is busy")
		}
	}

	text := ""
	stream := func(from, to int) {
		for i := from; i <= to; i++ {
			provider.mu.Lock()
			provider.sandboxBusy = i%2 == 1
			provider.sandboxRunningOp = fmt.Sprintf("op-%d", i)
			provider.mu.Unlock()
			text += "字"
			eng.publishAcp(run.ID, "clarify", msgEvents(text), true)
			time.Sleep(dialogueThrottle / 3)
		}
	}

	stream(1, 6)
	time.Sleep(2 * dialogueThrottle)
	for i, f := range socket.close() {
		view.apply(f)
		check(fmt.Sprintf("live frame %d", i), view.st)
	}
	if row := turnReply(view.st, turnID); row == nil || row.Phase != rowRunning || row.Text != text {
		bad = append(bad, fmt.Sprintf("before disconnect: reply %+v, want running %q", row, text))
	}
	beforeDrop := view.st.Version()

	// Socket down: the reply keeps streaming and the flag keeps flipping.
	stream(7, 12)

	reconnect := connectDialogues(t, eng, run.ID, "clarify")
	if !beforeDrop.Less(reconnect.Version()) {
		bad = append(bad, fmt.Sprintf("reconnect version %v not after %v", reconnect.Version(), beforeDrop))
	}
	view.apply(reconnect)
	check("reconnect", view.st)
	if row := turnReply(view.st, turnID); row == nil || row.Phase != rowRunning || row.Text != text {
		bad = append(bad, fmt.Sprintf("after reconnect: reply %+v, want running %q", row, text))
	}

	socket = recordDialogues(eng, run.ID)
	provider.mu.Lock()
	provider.sandboxBusy = false
	provider.reactScript = []runtime.ReactTurn{{Msg: text, Done: false, Events: msgEvents(text)}}
	provider.reactHold = nil
	provider.mu.Unlock()
	close(hold)
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("settle: %v", err)
	}
	time.Sleep(2 * dialogueThrottle)
	for i, f := range socket.close() {
		view.apply(f)
		if tr := findTurn(view.st, turnID); tr == nil || tr.Phase != turnEnded {
			check(fmt.Sprintf("closing frame %d", i), view.st)
		}
	}
	if view.resync != 0 {
		bad = append(bad, fmt.Sprintf("tab needed %d REST resyncs", view.resync))
	}
	row := turnReply(view.st, turnID)
	if row == nil || row.Phase != rowSealed || row.Outcome != models.OutcomeOK || row.Text != text {
		bad = append(bad, fmt.Sprintf("sealed reply %+v, want ok %q", row, text))
	}
	beforeRestart := view.st

	// Restart: a new process on the same database, and the tab reconnects.
	eng2 := New(db, provider, eng.host, eng.store, 5)
	t.Cleanup(eng2.Close)
	if err := eng2.InitDialogueEpoch(); err != nil {
		t.Fatal(err)
	}
	after := connectDialogues(t, eng2, run.ID, "clarify")
	if after.Epoch <= beforeRestart.Epoch || !beforeRestart.Version().Less(after.Version()) {
		bad = append(bad, fmt.Sprintf("restart version %v not after %v", after.Version(), beforeRestart.Version()))
	}
	view.apply(after)
	if got, want := rowsSummary(view.st.Rows), rowsSummary(beforeRestart.Rows); got != want {
		bad = append(bad, fmt.Sprintf("rows after restart:\n  %s\nbefore:\n  %s", got, want))
	}

	if len(bad) > 0 {
		t.Fatalf("acceptance violations:\n%s", strings.Join(bad, "\n"))
	}
}

// TestDialogueAcceptanceRestartMidTurn: the server restarts while a reply is
// still streaming. The new process has no live events for that turn; the tab
// must not be shown "本轮没有输出" for a reply that was never sealed.
func TestDialogueAcceptanceRestartMidTurn(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	if err := eng.InitDialogueEpoch(); err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	provider.reactHold = hold
	provider.reactPending = 10

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		close(hold)
		_ = eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second)
	})
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "做登录", nil, nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	turnID := waitRunningTurn(t, eng, run.ID, "clarify")
	eng.publishAcp(run.ID, "clarify", msgEvents("一半"), true)
	before := connectDialogues(t, eng, run.ID, "clarify")

	eng2 := New(db, provider, eng.host, eng.store, 5)
	t.Cleanup(eng2.Close)
	if err := eng2.InitDialogueEpoch(); err != nil {
		t.Fatal(err)
	}
	after := connectDialogues(t, eng2, run.ID, "clarify")
	if !before.Version().Less(after.Version()) {
		t.Fatalf("restart version %v not after %v", after.Version(), before.Version())
	}
	t.Logf("after restart: rows=%s turns=%+v busy=%v", rowsSummary(after.Rows), after.Turns, after.Busy)
	if row := turnReply(after, turnID); row != nil && (row.Outcome == models.OutcomeEmpty || row.Outcome == models.OutcomeFailed) {
		t.Fatalf("restart shows a failure card for an unsealed reply: %+v", row)
	}
	var view tab
	view.apply(before)
	view.apply(after)
	if row := turnReply(view.st, turnID); row != nil && row.Phase == rowRunning {
		t.Fatalf("tab keeps the pre-restart streaming row: %+v", row)
	}
	if !rowsHaveTurnHuman(after.Rows, turnID) {
		t.Fatalf("restart lost the human row of turn %s: %s", turnID, rowsSummary(after.Rows))
	}
}
