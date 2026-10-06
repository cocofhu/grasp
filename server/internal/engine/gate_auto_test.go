package engine

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

type recordingGateAuto struct {
	mu  sync.Mutex
	evs []GateAutoInvokeEvent
	n   atomic.Int32
}

func newRecordingGateAuto() *recordingGateAuto {
	return &recordingGateAuto{}
}

func (r *recordingGateAuto) NotifyGatePaused(ev GateAutoInvokeEvent) {
	r.n.Add(1)
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
}

func (r *recordingGateAuto) wait(t *testing.T, n int) []GateAutoInvokeEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.evs) >= n {
			out := append([]GateAutoInvokeEvent(nil), r.evs...)
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("want >=%d gate-auto events, got %d", n, len(r.evs))
	return nil
}

func TestGateAutoInvokeOnHumanGatePause(t *testing.T) {
	g := models.Graph{
		Variables: []models.Variable{
			{Name: "pm_auto_gate", Type: "boolean", Value: true},
		},
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
	proj := models.Project{ID: "proj-gate-auto", Name: "GateAuto", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&proj).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").
		Update("project_id", proj.ID).Error; err != nil {
		t.Fatal(err)
	}

	rec := newRecordingGateAuto()
	eng.SetGateAutoInvoker(rec)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	evs := rec.wait(t, 1)
	ev := evs[0]
	if ev.ProjectID != proj.ID || ev.RunID != run.ID || ev.NodeID != "gate" || ev.NodeType != "human_gate" {
		t.Fatalf("event=%+v", ev)
	}
	if ev.GateID == 0 || ev.GateTitle != "设计评审" {
		t.Fatalf("gate fields=%+v", ev)
	}
	if v, ok := ev.Vars["pm_auto_gate"]; !ok || !truthy(v) {
		t.Fatalf("vars=%v", ev.Vars)
	}
	if ev.PathSummary == "" {
		t.Fatal("expected path summary")
	}

	if err := eng.ResumeGate(run.ID, "gate", "approve", nil); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

func TestGatePathSummary(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "impl", Type: "agent", Caps: capsPlain, Label: "实现"},
			{ID: "gate", Type: "human_gate", Label: "门禁"},
			{ID: "out", Type: "output", Label: "输出"},
		},
		Edges: []models.Edge{
			{Source: "input", Target: "impl"},
			{Source: "impl", Target: "gate"},
			{Source: "gate", Target: "out"},
		},
	}
	got := gatePathSummary(g, "gate")
	if got != "输入 → 实现 → 门禁" {
		t.Fatalf("path=%q", got)
	}
}

func TestGateAutoInvokeSkipsReviewAgent(t *testing.T) {
	g := models.Graph{
		Variables: []models.Variable{
			{Name: "pm_auto_gate", Type: "boolean", Value: true},
		},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "preview", Type: "agent", Caps: capsPreview, Label: "预览", Config: map[string]any{
				"title": "应用预览",
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "preview"},
			{ID: "e2", Source: "preview", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	proj := models.Project{ID: "proj-app-preview", Name: "AppPrev", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&proj).Error; err != nil {
		t.Fatal(err)
	}
	_ = db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").Update("project_id", proj.ID)
	rec := newRecordingGateAuto()
	eng.SetGateAutoInvoker(rec)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	time.Sleep(200 * time.Millisecond)
	if n := int(rec.n.Load()); n != 0 {
		t.Fatalf("a review agent must not fire gate-auto; got %d events", n)
	}
}

func TestResumeGateIdempotentHumanThenPM(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":   "t",
				"actions": []any{map[string]any{"id": "approve", "label": "批准"}},
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "output", SourceHandle: "approve"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := eng.ResumeGate(run.ID, "gate", "approve", nil); err != nil {
		t.Fatal(err)
	}
	err2 := eng.ResumeGate(run.ID, "gate", "approve", nil)
	if err2 == nil {
		t.Fatal("second resume expected error")
	}
	msg := err2.Error()
	if !strings.Contains(msg, "already resolved") && !strings.Contains(msg, "run already ended") {
		t.Fatalf("second resume err=%v", err2)
	}
	waitRunStatus(t, db, run.ID, "completed")
}
