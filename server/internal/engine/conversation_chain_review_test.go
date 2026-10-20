package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

type segView struct {
	id   string
	kind string
	mode string
	nt   int
	task string
}

func chainOf(t *testing.T, env takeoverEnv, runID string) []segView {
	t.Helper()
	var run models.Run
	if err := env.db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	segs, states, err := services.NewRunService(env.db).ConversationChain(run, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint]bool{}
	for _, s := range segs {
		want[s.StateRunID] = s.NodeType == "agent"
	}
	tasks := env.eng.SegmentTasks(run, states, func(sr models.StateRun) bool { return want[sr.ID] })
	out := make([]segView, 0, len(segs))
	for _, s := range segs {
		n := -1
		if s.Turns != nil {
			n = len(*s.Turns)
		}
		out = append(out, segView{fmt.Sprintf("%s#%d", s.NodeID, s.Iteration), s.Kind, s.Mode, n, tasks[s.StateRunID]})
	}
	return out
}

func enterKeys(frames []map[string]any) []string {
	var out []string
	for _, f := range frames {
		if f["type"] == "node_enter" {
			out = append(out, fmt.Sprintf("%v#%v", f["nodeId"], f["iteration"]))
		}
	}
	return out
}

func waitEnters(get func() []map[string]any, n int) []string {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if k := enterKeys(get()); len(k) >= n {
			time.Sleep(100 * time.Millisecond)
			return enterKeys(get())
		}
		time.Sleep(10 * time.Millisecond)
	}
	return enterKeys(get())
}

// T1: takeover impl#1 → confirm → gate#1 revise → impl#2 auto → gate#2.
func TestReview850ChainTakeoverLoop(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverLoopGraph())
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	get, stop := collectFrames(env.eng, run.ID)
	defer stop()
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")
	if err := env.eng.ReactReply(run.ID, "impl", "确认", nil, nil, true); err != nil {
		t.Fatal(err)
	}
	waitGateIteration(t, env.db, run.ID, "gate", 1)
	env.provider.releaseHold("impl")
	if err := env.eng.ResumeGate(run.ID, "gate", "revise", nil); err != nil {
		t.Fatal(err)
	}
	waitGateIteration(t, env.db, run.ID, "gate", 2)

	segs := chainOf(t, env, run.ID)
	var got []string
	for _, s := range segs {
		got = append(got, s.id+"/"+s.kind+"/"+s.mode)
	}
	// input#1 was entered before collectFrames subscribed.
	wantChain := "input#1/agent/auto,impl#1/takeover/manual,gate#1/gate/manual,impl#2/agent/auto,gate#2/gate/manual"
	if strings.Join(got, ",") != wantChain {
		t.Fatalf("chain=%s", strings.Join(got, ","))
	}
	if segs[1].nt < 1 || segs[3].nt != -1 || segs[1].task != "实现" || segs[3].task != "实现" {
		t.Fatalf("turns/task: %+v", segs)
	}
	keys := waitEnters(get, 4)
	if strings.Join(keys, ",") != "impl#1,gate#1,impl#2,gate#2" {
		// input#1 may or may not be seen depending on subscribe timing
		if strings.Join(keys, ",") != "input#1,impl#1,gate#1,impl#2,gate#2" {
			t.Fatalf("node_enter keys=%v", keys)
		}
	}
	// node_enter follows the trace enter of the same visit.
	fr := get()
	for i, f := range fr {
		if f["type"] != "node_enter" {
			continue
		}
		ok := false
		for j := i - 1; j >= 0; j-- {
			if fr[j]["type"] == "trace" {
				e, _ := fr[j]["entry"].(map[string]any)
				ok = e["event"] == "enter" && e["nodeId"] == f["nodeId"]
				break
			}
		}
		if !ok {
			t.Fatalf("node_enter %v not right after its trace enter", f)
		}
	}
}

// T2: restart reattach of a manual visit: no node_enter, chain one segment.
func TestReview850RestartNoNodeEnterSingleSegment(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, "manual")
	get, stop := collectFrames(env.eng, run.ID)
	defer stop()
	eng2 := New(env.db, env.provider, env.host, env.arts, 5)
	t.Cleanup(eng2.Close)
	get2, stop2 := collectFrames(eng2, run.ID)
	defer stop2()
	time.Sleep(200 * time.Millisecond)
	if k := append(enterKeys(get()), enterKeys(get2())...); len(k) != 0 {
		t.Fatalf("restart must not push node_enter: %v", k)
	}
	segs := chainOf(t, takeoverEnv{eng: eng2, db: env.db}, run.ID)
	if len(segs) != 1 || segs[0].id != "impl#1" || segs[0].kind != "takeover" || segs[0].nt < 1 {
		t.Fatalf("chain=%+v", segs)
	}
}

// T3: cancel impl#1 then ResumeFrom impl → impl#2: two segments, two enters.
func TestReview850CancelResumeFromSplits(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	get, stop := collectFrames(env.eng, run.ID)
	defer stop()
	waitStarted(t, env.provider)
	if err := env.eng.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, env.db, run.ID, "cancelled")
	env.provider.releaseHold("impl")
	if err := env.eng.ResumeFrom(run.ID, "impl"); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, env.db, run.ID, "completed")
	segs := chainOf(t, env, run.ID)
	var got []string
	for _, s := range segs {
		got = append(got, s.id+"/"+s.kind)
	}
	if strings.Join(got, ",") != "input#1/agent,impl#1/agent,impl#2/agent,output#1/agent" {
		t.Fatalf("chain=%v", got)
	}
	keys := waitEnters(get, 3)
	cnt := map[string]int{}
	for _, k := range keys {
		cnt[k]++
	}
	if cnt["impl#2"] != 1 || cnt["output#1"] != 1 {
		t.Fatalf("node_enter=%v", keys)
	}
}

// T4: a workspace variable flagged Secret is masked in task (GraspReview #850 suggestion 1).
func TestReview850SecretWorkspaceVarInTask(t *testing.T) {
	g := takeoverPlainGraph(capsPlain)
	g.Nodes[1].Config = map[string]any{"agent_profile": "dev", "prompt": "deploy with {{vars.tok}}"}
	env := setupTakeoverEngine(t, g)
	env.eng.SetWorkspaceVarsLookup(func(string) []models.WorkspaceVariable {
		return []models.WorkspaceVariable{{Name: "tok", Type: "string", Value: "s3cr3t-value", Secret: true}}
	})
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, env.provider)
	segs := chainOf(t, env, run.ID)
	found := false
	for _, s := range segs {
		if strings.Contains(s.task, "s3cr3t-value") {
			t.Fatalf("secret workspace var leaked in task: %q", s.task)
		}
		if s.id == "impl#1" {
			found = s.task == "deploy with "+SecretMask
		}
	}
	if !found {
		t.Fatalf("impl#1 task not masked: %+v", segs)
	}
	var rv models.RunVariable
	if err := env.db.Where("run_id = ? AND name = ?", run.ID, "tok").First(&rv).Error; err != nil || !rv.Secret || rv.Value != "s3cr3t-value" {
		t.Fatalf("run var keeps real value and Secret flag: %+v %v", rv, err)
	}
}

// Gate hot-revise through the real engine: turns are tagged with the gate,
// land on the gate segment, and the producer stays agent/auto; approve sets
// decision=approved.
func TestChainGateReactThroughEngine(t *testing.T) {
	eng, db, _ := setupGateReactEngine(t)
	run, err := eng.StartRun("gate-react-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitGatePending(t, db, run.ID, "select")
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := eng.GateReactRevise(run.ID, "select", "按标注改", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range conv.Messages {
		if m.GateNodeID != "select" {
			t.Fatalf("gate-react turn not tagged: %+v", m)
		}
	}
	chain := func() []services.ChainSegment {
		var r models.Run
		db.First(&r, "id = ?", run.ID)
		segs, _, err := services.NewRunService(db).ConversationChain(r, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		return segs
	}
	segs := chain()
	var got []string
	for _, s := range segs {
		n := -1
		if s.Turns != nil {
			n = len(*s.Turns)
		}
		got = append(got, fmt.Sprintf("%s#%d/%s/%s/%d", s.NodeID, s.Iteration, s.Kind, s.Mode, n))
	}
	if strings.Join(got, ",") != fmt.Sprintf("input#1/agent/auto/-1,prop#1/agent/auto/-1,select#1/gate/manual/%d", len(conv.Messages)) {
		t.Fatalf("chain=%v", got)
	}
	if err := eng.ResumeGate(run.ID, "select", "approve", nil); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	segs = chain()
	if s := segs[2]; s.NodeID != "select" || s.Decision != services.ChainDecisionApproved || s.Turns == nil || len(*s.Turns) != len(conv.Messages) {
		t.Fatalf("resolved gate: %+v", s)
	}
}
