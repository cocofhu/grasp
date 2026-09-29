package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSessionBridgeStateKeepsInFlightOp(t *testing.T) {
	acp := sandbox.NewACPClient("127.0.0.1", 9)
	acp.SeedBridgeForTest(sandbox.BridgeState{
		Known: true, Busy: true, Desynced: true,
		RunningOpID: "g-00fccbf7-0f2", Waiting: 0,
	}, "g-00fccbf7-0f2", "g-next")
	p := &acpProvider{sessions: map[string]*reactSession{
		"run|clarify": {acp: acp},
	}}
	st, ok := p.SessionBridgeState("run", "clarify")
	if !ok || !st.Busy || st.Desynced || st.RunningOpID != "g-next" || st.InFlightOpID != "g-next" {
		t.Fatalf("plan g1.1: in-flight op dropped: %+v ok=%v", st, ok)
	}
	if st.LastDoneOpID != "g-00fccbf7-0f2" {
		t.Fatalf("plan g1.1: last done = %q", st.LastDoneOpID)
	}
}

func TestSessionBridgeStateDropsFinishedEchoWhenIdle(t *testing.T) {
	acp := sandbox.NewACPClient("127.0.0.1", 9)
	acp.SeedBridgeForTest(sandbox.BridgeState{
		Known: true, Busy: true, Desynced: true,
		RunningOpID: "g-00fccbf7-0f2", Waiting: 0,
	}, "g-00fccbf7-0f2", "")
	p := &acpProvider{sessions: map[string]*reactSession{
		"run|clarify": {acp: acp},
	}}
	st, ok := p.SessionBridgeState("run", "clarify")
	if !ok || st.Busy || st.Desynced || st.RunningOpID != "" || st.InFlightOpID != "" {
		t.Fatalf("plan g1.1: finished echo still live: %+v ok=%v", st, ok)
	}
}

func TestClientBridgeStatusKeepsOtherBridgeOp(t *testing.T) {
	acp := sandbox.NewACPClient("127.0.0.1", 9)
	acp.SeedBridgeForTest(sandbox.BridgeState{
		Known: true, Busy: true,
		RunningOpID: "g-other", Waiting: 0,
	}, "g-done", "g-next")
	st, ok := ClientBridgeStatus(acp)
	if !ok || !st.Busy || st.RunningOpID != "g-other" || st.InFlightOpID != "g-next" {
		t.Fatalf("plan g2.2: other bridge op overwritten: %+v ok=%v", st, ok)
	}
}
