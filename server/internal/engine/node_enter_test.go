package engine

import (
	"testing"
	"time"
)

func nodeEnterFrames(frames []map[string]any) []map[string]any {
	var out []map[string]any
	for _, f := range frames {
		if f["type"] == "node_enter" {
			out = append(out, f)
		}
	}
	return out
}

// TestNodeEnterFrame: every new visit publishes node_enter on the run topic
// (gate → kind gate/manual; a loop-back visit → next iteration, agent/auto).
// name is the node label, "" when the node has none (same as currentNode).
func TestNodeEnterFrame(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverLoopGraph())
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, env.provider)
	get, stop := collectFrames(env.eng, run.ID)
	defer stop()
	env.provider.releaseHold("impl")
	waitGateIteration(t, env.db, run.ID, "gate", 1)
	if err := env.eng.ResumeGate(run.ID, "gate", "revise", nil); err != nil {
		t.Fatalf("revise: %v", err)
	}
	waitGateIteration(t, env.db, run.ID, "gate", 2)

	var frames []map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if frames = nodeEnterFrames(get()); len(frames) >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	want := []struct {
		node string
		iter float64
		kind string
		mode string
		name string
	}{
		{"gate", 1, "gate", "manual", ""},
		{"impl", 2, "agent", "auto", "实现"},
		{"gate", 2, "gate", "manual", ""},
	}
	if len(frames) < len(want) {
		t.Fatalf("node_enter frames: %v", frames)
	}
	for i, w := range want {
		f := frames[i]
		if f["runId"] != run.ID || f["nodeId"] != w.node || f["iteration"] != w.iter || f["kind"] != w.kind || f["mode"] != w.mode || f["name"] != w.name {
			t.Fatalf("frame %d = %v, want %+v", i, f, w)
		}
	}
}
