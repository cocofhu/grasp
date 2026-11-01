package engine

import (
	"testing"
	"time"
)

func hubHas(eng *Engine, runID, nodeID, lane string) bool {
	h := eng.dialogueHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.entries[dialogueKey(runID, nodeID, lane)]
	return ok
}

func sweepAt(eng *Engine, now time.Time) {
	h := eng.dialogueHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	eng.sweepDialoguesLocked(h, now)
}

// An evicted dialogue is rebuilt with the same rows under a newer version.
func TestDialogueEvictionKeepsVersionsMonotonic(t *testing.T) {
	eng, _, runID := startClarifyDialogue(t)
	before, turnID := sendAndSettle(t, eng, runID, "做登录")

	eng.forgetDialogues(runID, "", "*")
	if hubHas(eng, runID, "clarify", "") {
		t.Fatal("entry still held after forget")
	}
	after, ok := eng.DialogueSnapshot(runID, "clarify")
	if !ok {
		t.Fatal("no dialogue after eviction")
	}
	if !before.Version().Less(after.Version()) {
		t.Fatalf("version %v not after %v", after.Version(), before.Version())
	}
	// Rows are rebuilt as they were; the recent-ended-turn memory starts over.
	before.Turns, after.Turns = nil, nil
	if unversioned(before) != unversioned(after) {
		t.Fatalf("rebuilt dialogue differs:\n%s\n%s", unversioned(before), unversioned(after))
	}
	if row := turnReply(after, turnID); row == nil || row.Phase != rowSealed || row.Outcome != "ok" {
		t.Fatalf("reply after eviction %+v", row)
	}
}

// Idle entries go once no session runs them; a finished run's go at the next sweep.
func TestDialogueSweepDropsIdleEntries(t *testing.T) {
	eng, _, runID := startClarifyDialogue(t)
	sendAndSettle(t, eng, runID, "做登录")
	eng.dialogueEntry(runID, "ghost")
	eng.dialogueEntry("run-other", "clarify")

	sweepAt(eng, time.Now().Add(dialogueIdleTTL/2))
	if !hubHas(eng, runID, "ghost", "") {
		t.Fatal("entry dropped before its TTL")
	}
	sweepAt(eng, time.Now().Add(2*dialogueIdleTTL))
	if hubHas(eng, runID, "ghost", "") || hubHas(eng, "run-other", "clarify", "") {
		t.Fatal("idle entries without a session kept")
	}
	if eng.laneSession(runID, "clarify", "") != nil && !hubHas(eng, runID, "clarify", "") {
		t.Fatal("entry of a live session dropped")
	}

	eng.dialogueEntry("run-done", "clarify")
	eng.expireDialogues("run-done")
	sweepAt(eng, time.Now())
	if hubHas(eng, "run-done", "clarify", "") {
		t.Fatal("finished run's entry kept")
	}
}

func TestDialogueForgetLaneOnly(t *testing.T) {
	eng, _, runID := startClarifyDialogue(t)
	eng.dialogueEntry(runID, "clarify")
	eng.laneDialogueEntry(runID, "clarify", "v1", "link")
	eng.forgetDialogues(runID, "clarify", "v1")
	if hubHas(eng, runID, "clarify", "v1") || !hubHas(eng, runID, "clarify", "") {
		t.Fatal("forget lane touched the wrong entries")
	}
}
