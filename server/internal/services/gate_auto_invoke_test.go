package services

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestGateAutoVarTruthy(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]any
		key  string
		want bool
	}{
		{"missing", map[string]any{"other": true}, "pm_auto_gate", false},
		{"nil map", nil, "pm_auto_gate", false},
		{"empty name", map[string]any{"x": true}, "", false},
		{"bool true", map[string]any{"pm_auto_gate": true}, "pm_auto_gate", true},
		{"bool false", map[string]any{"pm_auto_gate": false}, "pm_auto_gate", false},
		{"string false", map[string]any{"pm_auto_gate": "false"}, "pm_auto_gate", false},
		{"string empty", map[string]any{"pm_auto_gate": ""}, "pm_auto_gate", false},
		{"string truthy", map[string]any{"pm_auto_gate": "yes"}, "pm_auto_gate", true},
		{"int zero", map[string]any{"pm_auto_gate": 0}, "pm_auto_gate", false},
		{"int nonzero", map[string]any{"pm_auto_gate": 1}, "pm_auto_gate", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GateAutoVarTruthy(tc.vars, tc.key); got != tc.want {
				t.Fatalf("GateAutoVarTruthy(%v,%q)=%v want %v", tc.vars, tc.key, got, tc.want)
			}
		})
	}
}

func TestBuildGateAutoPromptAppendsUserPrompt(t *testing.T) {
	task := GateAutoTask{
		RunID:      "run1",
		WorkflowID: "wf1",
		NodeID:     "gate",
		NodeType:   "human_gate",
		NodeLabel:  "评审",
		GateID:     42,
		GateTitle:  "代码评审确认",
		GateBodyMd: "请确认",
		GateActions: []models.GateAction{
			{ID: "approve", Label: "批准"},
			{ID: "revise", Label: "需修改"},
		},
		Vars:        map[string]any{"pm_auto_gate": true, "env": "staging"},
		PathSummary: "实现 → 评审 → 部署",
	}
	got := BuildGateAutoPrompt(task, "")
	for _, needle := range []string{
		"runId: run1",
		"nodeId: gate",
		"gateId: 42",
		"gateType: human_gate",
		"title: 代码评审确认",
		"pm_list_pending_nodes",
		"pm_decide_node",
		"path: 实现 → 评审 → 部署",
		"approve (批准)",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("default prompt missing %q\n%s", needle, got)
		}
	}
	if strings.Contains(got, "[user prompt]") {
		t.Fatalf("empty user prompt must not add section:\n%s", got)
	}

	withUser := BuildGateAutoPrompt(task, "  优先批准低风险  ")
	if !strings.Contains(withUser, "[user prompt]\n优先批准低风险\n") {
		t.Fatalf("user prompt not appended:\n%s", withUser)
	}
	if idx := strings.Index(withUser, "tools: pm_list_pending_nodes"); idx < 0 {
		t.Fatal("missing tools line")
	} else if strings.Index(withUser, "[user prompt]") < idx {
		t.Fatal("user prompt must come after system default")
	}
}

func TestUpdateBindingGateAutoFields(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("GateAutoCfg", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, skills)
	en := true
	agent := "pm-agent"
	varName := "pm_auto_gate"
	prompt := "decide carefully"
	b, err := pm.UpdateBinding(&en, &agent, []string{"pm-workflow-write"}, &varName, &prompt)
	if err != nil {
		t.Fatal(err)
	}
	if b.GateAutoVar != "pm_auto_gate" || b.GateAutoPrompt != "decide carefully" {
		t.Fatalf("binding=%+v", b)
	}
	// Save without existence check: nonsense var name is allowed.
	bogus := "does_not_exist_yet"
	emptyPrompt := ""
	b2, err := pm.UpdateBinding(nil, nil, nil, &bogus, &emptyPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if b2.GateAutoVar != "does_not_exist_yet" || b2.GateAutoPrompt != "" {
		t.Fatalf("binding after clear prompt=%+v", b2)
	}
	// Empty var name disables capability.
	off := ""
	b3, err := pm.UpdateBinding(nil, nil, nil, &off, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b3.GateAutoVar != "" {
		t.Fatalf("want empty gateAutoVar, got %q", b3.GateAutoVar)
	}
}

func TestGateAutoEnqueuePreconditions(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("GateAutoPre", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, skills)
	svc := NewGateAutoInvokeService(db, pm, nil, nil, CronTokenHooks{})

	base := GateAutoTask{
		RunID: "run1", NodeID: "g", GateID: 1,
		Vars: map[string]any{"pm_auto_gate": true},
	}

	// No config var → not enqueued.
	svc.Enqueue(base)
	if svc.QueueLenForTest() != 0 || svc.BusyForTest() {
		t.Fatal("empty gateAutoVar must not enqueue")
	}

	en := true
	agent := "pm-agent"
	varName := "pm_auto_gate"
	if _, err := pm.UpdateBinding(&en, &agent, []string{"pm-progress"}, &varName, nil); err != nil {
		t.Fatal(err)
	}
	svc.Enqueue(base)
	if svc.QueueLenForTest() != 0 {
		t.Fatal("missing pm-workflow-write must not enqueue")
	}

	if _, err := pm.UpdateBinding(nil, nil, []string{"pm-workflow-write"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Var false → not enqueued.
	falseTask := base
	falseTask.Vars = map[string]any{"pm_auto_gate": false}
	svc.Enqueue(falseTask)
	if svc.QueueLenForTest() != 0 {
		t.Fatal("false var must not enqueue")
	}
	// Missing var key → not enqueued.
	miss := base
	miss.Vars = map[string]any{}
	svc.Enqueue(miss)
	if svc.QueueLenForTest() != 0 {
		t.Fatal("missing var must not enqueue")
	}

	// Happy path enqueues (runtime unavailable → process degrades, but enqueue happens).
	svc.Enqueue(base)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !svc.BusyForTest() && svc.QueueLenForTest() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if svc.BusyForTest() || svc.QueueLenForTest() != 0 {
		t.Fatalf("worker should finish degrade path; busy=%v len=%d",
			svc.BusyForTest(), svc.QueueLenForTest())
	}
}

func TestGateAutoSkipResolvedBeforeSend(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("GateAutoSkip", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, skills)
	en := true
	agent := "pm-agent"
	varName := "pm_auto_gate"
	if _, err := pm.UpdateBinding(&en, &agent, []string{"pm-workflow-write"}, &varName, nil); err != nil {
		t.Fatal(err)
	}

	gate := models.Gate{
		RunID: "run-skip", NodeID: "gate", Iteration: 1,
		WorkflowID: "wf", Title: "t", Resolved: true, RequestedAt: time.Now(),
	}
	if err := db.Create(&gate).Error; err != nil {
		t.Fatal(err)
	}

	// ForceActive so waitThreadIdle would block if we reached it — but resolved skip is earlier.
	turns := NewPmTurnRunner(pm, nil)
	svc := NewGateAutoInvokeService(db, pm, nil, turns, CronTokenHooks{})
	svc.Enqueue(GateAutoTask{
		RunID: "run-skip", NodeID: "gate", GateID: gate.ID,
		Vars: map[string]any{"pm_auto_gate": true},
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !svc.BusyForTest() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if svc.BusyForTest() {
		t.Fatal("resolved gate should skip without hanging")
	}
	// No message appended (no thread activity required).
	threads, _ := pm.ListThreadsForAgent(agent)
	for _, th := range threads {
		msgs, _ := pm.ListMessages(th.ID)
		if len(msgs) != 0 {
			t.Fatalf("skip path must not append messages, got %d on %s", len(msgs), th.ID)
		}
	}
}

func TestResolveMainThreadPrefersWritableUser(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("GateAutoThread", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, skills)
	en := true
	agent := "pm-agent"
	if _, err := pm.UpdateBinding(&en, &agent, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := pm.CreateThread("qq:guild:ch1", "频道", agent, models.ChatThreadKindUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.CreateCronThread(agent, "cron"); err != nil {
		t.Fatal(err)
	}
	human, err := pm.CreateThread("alice", "主会话", agent, models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}

	svc := NewGateAutoInvokeService(db, pm, nil, nil, CronTokenHooks{})
	got, err := svc.resolveMainThread()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != human.ID {
		t.Fatalf("want human thread %s, got %s (user=%s kind=%s)", human.ID, got.ID, got.UserID, got.Kind)
	}
}

func TestGateAutoEnqueueFollowsVarFlip(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("GateAutoFlip", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, skills)
	en := true
	agent := "pm-agent"
	varName := "pm_auto_gate"
	if _, err := pm.UpdateBinding(&en, &agent, []string{"pm-workflow-write"}, &varName, nil); err != nil {
		t.Fatal(err)
	}
	svc := NewGateAutoInvokeService(db, pm, nil, nil, CronTokenHooks{})

	ok, reason := svc.shouldEnqueue(GateAutoTask{
		RunID: "r", NodeID: "g1", GateID: 1,
		Vars: map[string]any{"pm_auto_gate": false},
	})
	if ok || reason != "var_missing_or_not_truthy" {
		t.Fatalf("false var: ok=%v reason=%s", ok, reason)
	}
	ok, reason = svc.shouldEnqueue(GateAutoTask{
		RunID: "r", NodeID: "g2", GateID: 2,
		Vars: map[string]any{"pm_auto_gate": true},
	})
	if !ok {
		t.Fatalf("truthy after set_var should enqueue, reason=%s", reason)
	}
}

func TestResolveMainThreadCreatesWhenAbsent(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("GateAutoCreateThr", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, skills)
	en := true
	agent := "pm-agent"
	if _, err := pm.UpdateBinding(&en, &agent, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	svc := NewGateAutoInvokeService(db, pm, nil, nil, CronTokenHooks{})
	got, err := svc.resolveMainThread()
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != gateAutoThreadUser || got.Kind != models.ChatThreadKindUser {
		t.Fatalf("created thread=%+v", got)
	}
}
