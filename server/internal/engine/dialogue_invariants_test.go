package engine

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/runtime"
)

// dialogueChecker accumulates invariant violations over a run's snapshots.
type dialogueChecker struct {
	bad []string
	// firstReply is the id of the first reply row seen for each turn; a reply
	// keeps its id from first output to sealing.
	firstReply map[string]string
	last       DialogueState
}

func (c *dialogueChecker) failf(format string, args ...any) {
	if len(c.bad) < 20 {
		c.bad = append(c.bad, fmt.Sprintf(format, args...))
	}
}

// check validates one snapshot. activeBefore/activeAfter are the platform's
// active turn read just before and after it ("" when idle).
func (c *dialogueChecker) check(step int, st DialogueState, activeBefore, activeAfter string) {
	if c.last.Seq != 0 && st.Version().Less(c.last.Version()) {
		c.failf("step %d: version %v went back from %v", step, st.Version(), c.last.Version())
	}
	if c.last.Seq != 0 && st.Version() == c.last.Version() && unversioned(st) != unversioned(c.last) {
		c.failf("step %d: state changed under the same version %v", step, st.Version())
	}
	c.last = st
	ids := map[string]bool{}
	humans := map[string]int{}
	streaming := map[string]int{}
	for _, r := range st.Rows {
		if ids[r.ID] {
			c.failf("step %d: duplicate row id %s", step, r.ID)
		}
		ids[r.ID] = true
		if r.Role == "human" {
			humans[r.TurnID]++
		}
		if r.Streaming {
			streaming[r.TurnID]++
		}
		if r.Role == "agent" && r.TurnID != "" && !strings.HasPrefix(r.TurnID, "orphan:") {
			if first, ok := c.firstReply[r.TurnID]; !ok {
				c.firstReply[r.TurnID] = r.ID
			} else if first != r.ID && !hasRowID(st.Rows, first) {
				c.failf("step %d: turn %s reply %s replaced %s", step, r.TurnID, r.ID, first)
			}
		}
		if r.Phase == rowSealed && r.Role == "agent" && r.Outcome == "" {
			c.failf("step %d: sealed reply %s has no outcome", step, r.ID)
		}
	}
	turns := map[string]DialogueTurn{}
	for _, tr := range st.Turns {
		if _, dup := turns[tr.TurnID]; dup {
			c.failf("step %d: turn %s listed twice", step, tr.TurnID)
		}
		turns[tr.TurnID] = tr
		if tr.Phase == turnEnded && tr.Outcome == "" {
			c.failf("step %d: ended turn %s has no outcome", step, tr.TurnID)
		}
		if tr.Phase == turnRunning && humans[tr.TurnID] != 1 {
			c.failf("step %d: running turn %s has %d request rows", step, tr.TurnID, humans[tr.TurnID])
		}
	}
	for id, n := range humans {
		if id != "" && n > 1 {
			c.failf("step %d: turn %s has %d request rows", step, id, n)
		}
	}
	for id, n := range streaming {
		if n > 1 {
			c.failf("step %d: turn %s has %d streaming rows", step, id, n)
		}
	}
	if activeBefore != "" && activeBefore == activeAfter {
		if tr, ok := turns[activeBefore]; !ok || tr.Phase != turnRunning {
			c.failf("step %d: platform runs %s but the dialogue shows %+v", step, activeBefore, tr)
		}
		if st.Orphan != nil {
			c.failf("step %d: orphan while the platform runs %s", step, activeBefore)
		}
	}
}

func unversioned(st DialogueState) string {
	st.Epoch, st.Seq = 0, 0
	st.Rows = append([]DialogueRow(nil), st.Rows...)
	for i := range st.Rows {
		st.Rows[i].Rev = 0
	}
	b, _ := json.Marshal(st)
	return string(b)
}

func hasRowID(rows []DialogueRow, id string) bool {
	for _, r := range rows {
		if r.ID == id {
			return true
		}
	}
	return false
}

func (c *dialogueChecker) frames(frames []DialogueState) {
	for i := 1; i < len(frames); i++ {
		if !frames[i-1].Version().Less(frames[i].Version()) {
			c.failf("frame %d: version %v not after %v", i, frames[i].Version(), frames[i-1].Version())
		}
	}
}

func activeTurnID(eng *Engine, runID, nodeID string) string {
	s := eng.laneSession(runID, nodeID, "")
	if s == nil {
		return ""
	}
	if v := s.View(); v.HasActive {
		return v.Active.ID
	}
	return ""
}

// randomDialogueRun drives one seeded interleaving and returns violations.
func randomDialogueRun(t *testing.T, seed int64, steps int) []string {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	eng, provider, runID := startClarifyDialogue(t)
	provider.mu.Lock()
	provider.reactPending = 1 << 20
	hold := make(chan struct{})
	provider.reactHold = hold
	provider.mu.Unlock()
	rec := recordDialogues(eng, runID)
	c := &dialogueChecker{firstReply: map[string]string{}}
	text := map[string]string{}

	release := func() {
		provider.mu.Lock()
		old := hold
		hold = make(chan struct{})
		provider.reactHold = hold
		provider.mu.Unlock()
		close(old)
	}
	for step := 0; step < steps; step++ {
		before := activeTurnID(eng, runID, "clarify")
		switch rng.Intn(11) {
		case 0, 1:
			_, _ = eng.EnqueueClarifyTurn(runID, "clarify", fmt.Sprintf("消息%d", step), nil, nil)
		case 2, 3:
			if before != "" {
				text[before] += "字"
				eng.publishAcp(runID, "clarify", msgEvents(text[before]), true)
			}
		case 4:
			provider.mu.Lock()
			provider.sandboxBusy = !provider.sandboxBusy
			provider.sandboxRunningOp = fmt.Sprintf("op-%d", step)
			provider.mu.Unlock()
		case 5:
			release()
		case 6:
			_ = eng.CancelClarifyTurn(runID, "clarify")
		case 7:
			_ = eng.ReactReplyRetryLast(runID, "clarify")
		case 8:
			provider.mu.Lock()
			provider.reactScript = append(provider.reactScript, runtime.ReactTurn{Msg: ""})
			provider.mu.Unlock()
		case 9:
			// Session recycle: an idle session is dropped and rebuilt on the next send.
			eng.dropLaneSessionIfIdle(runID, "clarify", "")
		case 10:
			// Dialogue entry eviction: the next build starts from no versioning state.
			eng.forgetDialogues(runID, "", "*")
		}
		time.Sleep(time.Duration(rng.Intn(3)) * time.Millisecond)
		st, ok := eng.DialogueSnapshot(runID, "clarify")
		after := activeTurnID(eng, runID, "clarify")
		if ok {
			c.check(step, st, before, after)
		}
	}

	provider.mu.Lock()
	provider.reactHold = nil
	provider.sandboxBusy = false
	provider.mu.Unlock()
	close(hold)
	if err := eng.waitReviewReadyForTest(runID, "clarify", 10*time.Second); err != nil {
		t.Fatalf("seed %d: never settled: %v", seed, err)
	}
	final, _ := eng.DialogueSnapshot(runID, "clarify")
	c.check(steps, final, "", "")
	if final.Busy {
		c.failf("final: still busy")
	}
	for _, tr := range final.Turns {
		if tr.Phase != turnEnded {
			c.failf("final: turn %s is %s", tr.TurnID, tr.Phase)
		}
	}
	for _, r := range final.Rows {
		if r.Phase != rowSealed {
			c.failf("final: row %s is %s", r.ID, r.Phase)
		}
	}
	time.Sleep(2 * dialogueThrottle)
	c.frames(rec.close())
	return c.bad
}

var dialogueSeeds = []int64{1, 2, 3, 5, 8, 13, 21, 34}

func TestDialogueRandomInterleavingsKeepInvariants(t *testing.T) {
	for _, seed := range dialogueSeeds {
		if bad := randomDialogueRun(t, seed, 80); len(bad) > 0 {
			t.Fatalf("seed %d:\n%s", seed, strings.Join(bad, "\n"))
		}
	}
}

// Each fault re-introduces a known bug class; the invariants must catch it
// on at least one seed.
func TestDialogueInvariantsCatchInjectedFaults(t *testing.T) {
	faults := []struct {
		name string
		on   func(bool)
	}{
		{"sandbox flag decides a busy turn", faultSandboxWhileBusy},
		{"changed state keeps its version", faultSkipVersion},
		{"sealed reply gets a new row id", faultResealRowID},
	}
	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			f.on(true)
			defer f.on(false)
			for _, seed := range dialogueSeeds {
				if bad := randomDialogueRun(t, seed, 80); len(bad) > 0 {
					t.Logf("seed %d caught: %s", seed, bad[0])
					return
				}
			}
			t.Fatal("fault went unnoticed on every seed")
		})
	}
}
