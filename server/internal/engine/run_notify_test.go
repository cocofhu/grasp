package engine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

type recordingRunNotify struct {
	mu  sync.Mutex
	evs []RunNotifyEvent
	n   atomic.Int32
}

func (r *recordingRunNotify) NotifyRunLifecycle(ev RunNotifyEvent) {
	r.n.Add(1)
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
}

func (r *recordingRunNotify) wait(t *testing.T, n int) []RunNotifyEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.evs) >= n {
			out := append([]RunNotifyEvent(nil), r.evs...)
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("want >=%d run-notify events, got %d", n, len(r.evs))
	return nil
}

func (r *recordingRunNotify) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.evs)
}

func TestRunNotifyOnWaitingHuman(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "gate", Type: "human_gate", Label: "评审", Config: map[string]any{
				"title": "设计评审",
				"actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
				},
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "output", SourceHandle: "approve"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	proj := models.Project{ID: "proj-run-notify", Name: "Notify", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&proj).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").
		Update("project_id", proj.ID).Error; err != nil {
		t.Fatal(err)
	}

	rec := &recordingRunNotify{}
	eng.SetRunNotifier(rec)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	evs := rec.wait(t, 1)
	if evs[0].Kind != models.NotifyKindWaitingHuman || evs[0].NodeID != "gate" || evs[0].Iteration < 1 {
		t.Fatalf("unexpected event: %+v", evs[0])
	}
	time.Sleep(50 * time.Millisecond)
	if rec.count() != 1 {
		t.Fatalf("count=%d want 1", rec.count())
	}
}

func TestRunNotifyOnNodeFailed(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "boom", Type: "agent", Caps: capsPlain, Label: "实现", Config: map[string]any{
				"prompt": "x", "produces": "out.md",
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "boom"},
			{ID: "e2", Source: "boom", Target: "output"},
		},
	}
	eng, db, prov := setupEngineGraphP(t, g)
	prov.failLeft = map[string]int{"boom": 99}
	proj := models.Project{ID: "proj-run-fail", Name: "Fail", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&proj).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").
		Update("project_id", proj.ID).Error; err != nil {
		t.Fatal(err)
	}
	rec := &recordingRunNotify{}
	eng.SetRunNotifier(rec)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "failed")
	evs := rec.wait(t, 1)
	if evs[0].Kind != models.NotifyKindFailed || evs[0].NodeID != "boom" {
		t.Fatalf("unexpected: %+v", evs[0])
	}
	time.Sleep(50 * time.Millisecond)
	if rec.count() != 1 {
		t.Fatalf("failed notify count=%d want 1", rec.count())
	}
}

func TestRunNotifyFailRunNoNodeSkipped(t *testing.T) {
	eng, _, _ := setupEngineGraphP(t, models.Graph{
		Nodes: []models.Node{{ID: "input", Type: "input"}},
	})
	rec := &recordingRunNotify{}
	eng.SetRunNotifier(rec)
	eng.failRun("missing-run", "early")
	time.Sleep(80 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("failRun must not notify, got %d", rec.count())
	}
}

func TestFireRunNotifyDirect(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Label: "G"},
		},
		Edges: []models.Edge{{ID: "e1", Source: "input", Target: "gate"}},
	})
	proj := models.Project{ID: "proj-direct", Name: "D", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = db.Create(&proj)
	_ = db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").Update("project_id", proj.ID)
	rec := &recordingRunNotify{}
	eng.SetRunNotifier(rec)
	c := &execCtx{
		run:  &models.Run{ID: "run-x", WorkflowID: "wf", WorkflowName: "W"},
		iter: map[string]int{"gate": 2},
	}
	eng.fireRunNotify(c, &models.Node{ID: "gate", Label: "G", Type: "human_gate"}, models.NotifyKindWaitingHuman)
	evs := rec.wait(t, 1)
	if evs[0].Iteration != 2 || evs[0].ProjectID != "proj-direct" {
		t.Fatalf("%+v", evs[0])
	}
}

func TestRunNotifyOnCompleted(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "output", Type: "output", Label: "结束"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	proj := models.Project{ID: "proj-run-done", Name: "Done", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&proj).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").
		Update("project_id", proj.ID).Error; err != nil {
		t.Fatal(err)
	}
	rec := &recordingRunNotify{}
	eng.SetRunNotifier(rec)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	evs := rec.wait(t, 1)
	if evs[0].Kind != models.NotifyKindCompleted || evs[0].NodeID != "output" {
		t.Fatalf("unexpected completed event: %+v", evs[0])
	}
	if evs[0].NodeLabel != "结束" || evs[0].Iteration < 1 {
		t.Fatalf("label/iter: %+v", evs[0])
	}
	time.Sleep(50 * time.Millisecond)
	if rec.count() != 1 {
		t.Fatalf("completed notify count=%d want 1", rec.count())
	}
}

func TestFireCompletedRunNotifySentinel(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, models.Graph{
		Nodes: []models.Node{{ID: "input", Type: "input", Label: "输入"}},
	})
	proj := models.Project{ID: "proj-sentinel", Name: "S", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = db.Create(&proj)
	_ = db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").Update("project_id", proj.ID)
	rec := &recordingRunNotify{}
	eng.SetRunNotifier(rec)
	run := models.Run{ID: "run-sentinel", WorkflowID: "wf", WorkflowName: "W", Status: "completed"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	eng.fireCompletedRunNotify(run.ID)
	evs := rec.wait(t, 1)
	if evs[0].Kind != models.NotifyKindCompleted || evs[0].NodeID != models.NotifyCompletedSentinelNodeID {
		t.Fatalf("%+v", evs[0])
	}
	if evs[0].NodeLabel != models.NotifyCompletedFallbackLabel || evs[0].Iteration != 1 {
		t.Fatalf("%+v", evs[0])
	}
}

func TestFireCompletedRunNotifyGraphFallback(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "output", Type: "output", Label: "结束"},
		},
	})
	proj := models.Project{ID: "proj-graph-fb", Name: "G", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = db.Create(&proj)
	_ = db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").Update("project_id", proj.ID)
	rec := &recordingRunNotify{}
	eng.SetRunNotifier(rec)
	run := models.Run{
		ID: "run-graph-fb", WorkflowID: "wf", WorkflowName: "W", Status: "completed",
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "output", Type: "output", Label: "结束"},
		}},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	eng.fireCompletedRunNotify(run.ID)
	evs := rec.wait(t, 1)
	if evs[0].NodeID != "output" || evs[0].NodeLabel != "结束" || evs[0].Iteration != 1 {
		t.Fatalf("%+v", evs[0])
	}
}
