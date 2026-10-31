package engine

import (
	"fmt"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
)

// Faults put known classes of dialogue bugs back through the build and stamp
// hooks, so the invariant tests can prove they catch them. Only one fault is
// on at a time.

// faultSandboxWhileBusy lets the bridge's busy flag end a turn the platform
// is still running (the "本轮没有输出" root cause): once the bridge was seen
// busy and then idle during a platform turn, that turn ends empty.
func faultSandboxWhileBusy(on bool) {
	if !on {
		dialogueBuildHook.Store(nil)
		return
	}
	var mu sync.Mutex
	seen := map[*dialogueEntry]*[2]bool{}
	h := func(e *Engine, en *dialogueEntry, st *DialogueState, platformBusy bool) {
		mu.Lock()
		defer mu.Unlock()
		f := seen[en]
		if f == nil {
			f = &[2]bool{}
			seen[en] = f
		}
		if !platformBusy {
			*f = [2]bool{}
			return
		}
		bs, ok := e.SandboxBridgeState(en.runID, en.nodeID)
		if ok && (bs.Busy || bs.Desynced) {
			f[0] = true
		} else if f[0] {
			f[1] = true
		}
		if !f[1] {
			return
		}
		for i := range st.Turns {
			tr := &st.Turns[i]
			if tr.Phase != turnRunning || turnHasSealedReply(st.Rows, tr.TurnID) {
				continue
			}
			rows := st.Rows[:0:0]
			k := 0
			for _, r := range st.Rows {
				if r.TurnID == tr.TurnID && r.Phase == rowRunning {
					continue
				}
				if r.TurnID == tr.TurnID && r.Role == "agent" {
					k++
				}
				rows = append(rows, r)
			}
			st.Rows = append(rows, DialogueRow{
				ReactMessage: models.ReactMessage{Role: "agent", TurnID: tr.TurnID,
					ClientMessageID: tr.ClientMessageID, Outcome: models.OutcomeEmpty},
				ID: fmt.Sprintf("%s:a%d", tr.TurnID, k), Phase: rowSealed,
			})
			tr.Phase, tr.Outcome = turnEnded, models.OutcomeEmpty
		}
	}
	dialogueBuildHook.Store(&h)
}

func turnHasSealedReply(rows []DialogueRow, turnID string) bool {
	for _, r := range rows {
		if r.TurnID == turnID && r.Role == "agent" && r.Phase == rowSealed {
			return true
		}
	}
	return false
}

// faultSkipVersion reuses the previous seq for a changed state.
func faultSkipVersion(on bool) {
	if !on {
		dialogueStampHook.Store(nil)
		return
	}
	h := func(en *dialogueEntry, next chatsession.DialogueVersion) chatsession.DialogueVersion {
		if en.cur != nil {
			return en.cur.Version()
		}
		return next
	}
	dialogueStampHook.Store(&h)
}

// faultResealRowID gives a reply a new row id when it is persisted.
func faultResealRowID(on bool) {
	if !on {
		dialogueBuildHook.Store(nil)
		return
	}
	h := func(_ *Engine, _ *dialogueEntry, st *DialogueState, _ bool) {
		for i := range st.Rows {
			r := &st.Rows[i]
			if r.Role == "agent" && r.Phase == rowSealed && r.TurnID != "" && !strings.HasPrefix(r.TurnID, "orphan:") {
				r.ID += ":sealed"
			}
		}
	}
	dialogueBuildHook.Store(&h)
}
