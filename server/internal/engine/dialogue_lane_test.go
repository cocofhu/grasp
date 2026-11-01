package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

func TestVisitorLaneDialogueIsItsOwnState(t *testing.T) {
	eng, _, p := setupVisitorEngine(t)
	hold := make(chan struct{})
	p.hold = hold
	lane := gateshare.VisitorLane("link1", strings.Repeat("c", 32))
	ch, unsub := eng.Broker().Subscribe("r1")
	defer unsub()

	const cmid = "6f1c1c58-2d3b-4c43-9c1b-0d9a3c6f7e10"
	target := visitorTarget(lane)
	target.ClientMessageID = cmid
	if _, err := eng.EnqueueVisitorTurn(target, "visitor asks", nil, nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var running DialogueState
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, ok := eng.LaneDialogueSnapshot("r1", "p1", lane, "link1")
		if ok && st.Busy && len(st.Rows) == 4 {
			running = st
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lane never showed its running turn: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if running.Lane != lane {
		t.Fatalf("lane state lane = %q", running.Lane)
	}
	human := running.Rows[2]
	if human.Role != "human" || human.Text != "visitor asks" || human.ClientMessageID != cmid || !strings.HasSuffix(human.ID, ":h") {
		t.Fatalf("running human row = %+v", human)
	}
	turnID := strings.TrimSuffix(human.ID, ":h")
	if r := running.Rows[3]; r.ID != turnID+":a0" || r.Phase != rowRunning || !r.Streaming {
		t.Fatalf("running reply row = %+v", r)
	}
	if node, ok := eng.DialogueSnapshot("r1", "p1"); !ok || node.Busy || len(node.Rows) != 2 || node.Lane != "" {
		t.Fatalf("the node dialogue must not see the visitor turn: %+v", node)
	}

	close(hold)
	waitLaneIdle(t, eng, lane)
	st, ok := eng.LaneDialogueSnapshot("r1", "p1", lane, "link1")
	if !ok || st.Busy || len(st.Rows) != 4 {
		t.Fatalf("settled lane state = %+v", st)
	}
	if st.Rows[2].ID != human.ID {
		t.Fatalf("persisting the human row changed its id: %s -> %s", human.ID, st.Rows[2].ID)
	}
	reply := st.Rows[3]
	if reply.ID != turnID+":a0" || reply.Phase != rowSealed || reply.Outcome != models.OutcomeOK || reply.Text != "reply to visitor asks" {
		t.Fatalf("sealed reply row = %+v", reply)
	}
	if got := turnPhase(st, turnID); got != turnEnded {
		t.Fatalf("turn phase = %q", got)
	}

	sawLane := false
	for {
		select {
		case raw := <-ch:
			var head struct {
				Type, Lane string
			}
			if json.Unmarshal(raw, &head) == nil && head.Type == "dialogue" && head.Lane == lane {
				sawLane = true
			}
			continue
		default:
		}
		break
	}
	if !sawLane {
		t.Fatal("the lane's dialogue was never published")
	}
}

func turnPhase(st DialogueState, turnID string) string {
	phase := ""
	for _, tr := range st.Turns {
		if tr.TurnID == turnID {
			phase = tr.Phase
		}
	}
	return phase
}
