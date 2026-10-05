package services

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "svc.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// validGraph returns a minimal structurally-valid pipeline (input -> output).
func validGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "Start"},
			{ID: "out", Type: "output", Label: "End"},
		},
		Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
	}
}

func TestWorkflowServiceCRUDPublishRestore(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	if got := s.List(""); len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get on missing should be false")
	}

	wf := &models.WorkflowDef{ID: "wf1", ProjectID: models.DefaultProjectID, Name: "Demo", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatalf("create: %v", err)
	}
	if wf.Version != 1 || wf.Status != "draft" {
		t.Fatalf("defaults wrong: v=%d status=%s", wf.Version, wf.Status)
	}

	// Update path preserves version.
	wf.Name = "Demo v2"
	wf.Version = 0
	if err := s.Save(wf); err != nil {
		t.Fatalf("update: %v", err)
	}
	if wf.Version != 1 {
		t.Fatalf("version clobbered to %d", wf.Version)
	}

	got, ok := s.Get("wf1")
	if !ok || got.Name != "Demo v2" {
		t.Fatalf("get after update: %+v ok=%v", got, ok)
	}
	if len(s.List("")) != 1 {
		t.Fatal("list should have one")
	}

	// Publish -> version bumps, snapshot created.
	pub, err := s.Publish("wf1")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.Version != 2 || pub.Status != "published" {
		t.Fatalf("publish result: %+v", pub)
	}
	if vs := s.Versions("wf1"); len(vs) != 1 || vs[0].Version != 2 {
		t.Fatalf("versions: %+v", vs)
	}

	// Publish missing.
	if _, err := s.Publish("ghost"); err == nil {
		t.Fatal("publish missing should error")
	}

	// Restore back to draft.
	rest, err := s.Restore("wf1", 2)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rest.Status != "draft" {
		t.Fatalf("restore status = %s", rest.Status)
	}
	if _, err := s.Restore("wf1", 99); err == nil {
		t.Fatal("restore missing version should error")
	}
	if _, err := s.Restore("ghost", 1); err == nil {
		t.Fatal("restore missing wf should error")
	}
}

func TestWorkflowPublishInvalidGraph(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	// A graph missing an output node fails Validate.
	bad := &models.WorkflowDef{ID: "bad", ProjectID: models.DefaultProjectID, Name: "Bad", Graph: models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}},
	}}
	if err := s.Save(bad); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("bad"); err == nil {
		t.Fatal("publish invalid graph should error")
	}
}

func TestWorkflowNameValidation(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf1 := &models.WorkflowDef{ID: "wf1", ProjectID: models.DefaultProjectID, Name: "Demo", Graph: validGraph()}
	if err := s.Save(wf1); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"", "   ", "\t"} {
		err := s.Save(&models.WorkflowDef{ID: "wf-new", ProjectID: models.DefaultProjectID, Name: name, Graph: validGraph()})
		if !errors.Is(err, ErrEmptyWorkflowName) {
			t.Fatalf("name %q: want ErrEmptyWorkflowName, got %v", name, err)
		}
	}

	err := s.Save(&models.WorkflowDef{ID: "wf2", ProjectID: models.DefaultProjectID, Name: "Demo", Graph: validGraph()})
	if !errors.Is(err, ErrWorkflowNameExists) {
		t.Fatalf("duplicate: want ErrWorkflowNameExists, got %v", err)
	}

	wfDemo := &models.WorkflowDef{ID: "wf-demo", ProjectID: models.DefaultProjectID, Name: "demo", Graph: validGraph()}
	if err := s.Save(wfDemo); err != nil {
		t.Fatalf("case variant: %v", err)
	}

	wf1.Name = "Demo"
	if err := s.Save(wf1); err != nil {
		t.Fatalf("self update: %v", err)
	}

	wf1.Name = "demo"
	if err := s.Save(wf1); !errors.Is(err, ErrWorkflowNameExists) {
		t.Fatalf("rename conflict: want ErrWorkflowNameExists, got %v", err)
	}
}

func TestWorkflowSaveRequiresProjectID(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	err := s.Save(&models.WorkflowDef{ID: "wf-no-proj", Name: "X", Graph: validGraph()})
	if !errors.Is(err, ErrWorkflowProjectRequired) {
		t.Fatalf("want ErrWorkflowProjectRequired, got %v", err)
	}
}

func TestSuggestCopyName(t *testing.T) {
	cases := []struct {
		source   string
		existing []string
		want     string
	}{
		{"工作流 A", nil, "工作流 A 副本"},
		{"工作流 A", []string{"工作流 A 副本"}, "工作流 A 副本(2)"},
		{"工作流 A", []string{"工作流 A 副本", "工作流 A 副本(2)"}, "工作流 A 副本(3)"},
		{"Demo", []string{"demo"}, "Demo 副本"},
	}
	for _, tc := range cases {
		got := SuggestCopyName(tc.source, tc.existing)
		if got != tc.want {
			t.Fatalf("SuggestCopyName(%q, %v) = %q, want %q", tc.source, tc.existing, got, tc.want)
		}
	}
}

func TestWorkflowCopy(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	src := &models.WorkflowDef{
		ID: "wf-src", ProjectID: models.DefaultProjectID, Name: "工作流 A", Description: "desc", NeedsRepo: true,
		Status: "published", Version: 3, Graph: validGraph(),
	}
	if err := s.Save(src); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-src"); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Run{ID: "run1", WorkflowID: "wf-src", Status: "completed"})

	suggested, sourceName, sourceID, err := s.CopyPreview("wf-src")
	if err != nil || suggested != "工作流 A 副本" || sourceName != "工作流 A" || sourceID != "wf-src" {
		t.Fatalf("CopyPreview: suggested=%q source=%q id=%q err=%v", suggested, sourceName, sourceID, err)
	}

	copied, err := s.Copy("wf-src", suggested)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if copied.ID == "wf-src" || copied.Name != suggested {
		t.Fatalf("copy identity: id=%s name=%s", copied.ID, copied.Name)
	}
	if copied.Status != "draft" || copied.Version != 1 {
		t.Fatalf("copy defaults: status=%s version=%d", copied.Status, copied.Version)
	}
	if copied.Description != "desc" || !copied.NeedsRepo {
		t.Fatalf("metadata not copied: %+v", copied)
	}
	if len(copied.Graph.Nodes) != len(src.Graph.Nodes) {
		t.Fatalf("graph not copied")
	}
	if copied.LastRunAt != nil {
		t.Fatal("LastRunAt should be zero")
	}

	var runCount int64
	db.Model(&models.Run{}).Where("workflow_id = ?", copied.ID).Count(&runCount)
	if runCount != 0 {
		t.Fatalf("runs copied: %d", runCount)
	}
	var verCount int64
	db.Model(&models.WorkflowVersion{}).Where("workflow_id = ?", copied.ID).Count(&verCount)
	if verCount != 0 {
		t.Fatalf("versions copied: %d", verCount)
	}

	suggested2, _, _, err := s.CopyPreview("wf-src")
	if err != nil || suggested2 != "工作流 A 副本(2)" {
		t.Fatalf("CopyPreview second: %q err=%v", suggested2, err)
	}

	if _, err := s.Copy("wf-src", suggested); !errors.Is(err, ErrWorkflowNameExists) {
		t.Fatalf("duplicate copy name: %v", err)
	}

	if _, _, _, err := s.CopyPreview("ghost"); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("CopyPreview missing: %v", err)
	}
}

func TestWorkflowDeleteCascades(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	wf := &models.WorkflowDef{ID: "wf", ProjectID: models.DefaultProjectID, Name: "W", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf"); err != nil {
		t.Fatal(err)
	}
	// Seed a run and dependent records.
	db.Create(&models.Run{ID: "r1", WorkflowID: "wf", Status: "completed"})
	db.Create(&models.StateRun{RunID: "r1", NodeID: "n"})
	db.Create(&models.RunVariable{RunID: "r1", Name: "x"})
	db.Create(&models.Artifact{ID: "a1", RunID: "r1", Name: "doc"})
	db.Create(&models.Gate{RunID: "r1", NodeID: "g"})
	db.Create(&models.ReactConversation{RunID: "r1", NodeID: "c"})

	if err := s.Delete("wf"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := s.Get("wf"); ok {
		t.Fatal("workflow should be gone")
	}
	var n int64
	db.Model(&models.Run{}).Where("workflow_id = ?", "wf").Count(&n)
	if n != 0 {
		t.Fatalf("runs not cascaded: %d", n)
	}
	db.Model(&models.Artifact{}).Where("run_id = ?", "r1").Count(&n)
	if n != 0 {
		t.Fatalf("artifacts not cascaded: %d", n)
	}
	db.Model(&models.WorkflowVersion{}).Where("workflow_id = ?", "wf").Count(&n)
	if n != 0 {
		t.Fatalf("versions not cascaded: %d", n)
	}
}

func TestRunDeleteStatusGate(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)

	if err := s.Delete("ghost"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing run: %v", err)
	}

	for _, status := range []string{"queued", "running", "waiting_human"} {
		id := "r-" + status
		db.Create(&models.Run{ID: id, WorkflowID: "wf", Status: status})
		err := s.Delete(id)
		if !errors.Is(err, ErrRunNotDeletable) {
			t.Fatalf("status %s: want ErrRunNotDeletable, got %v", status, err)
		}
		if _, ok := s.Get(id); !ok {
			t.Fatalf("status %s: run should still exist", status)
		}
	}

	for _, status := range []string{"completed", "failed", "cancelled"} {
		id := "r-ok-" + status
		db.Create(&models.Run{ID: id, WorkflowID: "wf", Status: status})
		if err := s.Delete(id); err != nil {
			t.Fatalf("status %s delete: %v", status, err)
		}
		if _, ok := s.Get(id); ok {
			t.Fatalf("status %s: run should be gone", status)
		}
		// Second delete is a safe not-found failure.
		if err := s.Delete(id); !errors.Is(err, ErrRunNotFound) {
			t.Fatalf("status %s second delete: %v", status, err)
		}
	}
}

func TestRunDeleteCascadesAndKeepsWorkflow(t *testing.T) {
	db := newTestDB(t)
	wfSvc := NewWorkflowService(db)
	runSvc := NewRunService(db)
	keySvc := NewAPIKeyService(db)

	wf := &models.WorkflowDef{ID: "wf-del-run", ProjectID: models.DefaultProjectID, Name: "KeepMe", Graph: validGraph()}
	if err := wfSvc.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := keySvc.Create("wf-del-run", "ops"); err != nil {
		t.Fatal(err)
	}

	const runID = "r-cascade"
	db.Create(&models.Run{ID: runID, WorkflowID: "wf-del-run", Status: "completed"})
	db.Create(&models.StateRun{RunID: runID, NodeID: "n"})
	db.Create(&models.RunVariable{RunID: runID, Name: "x"})
	db.Create(&models.Artifact{ID: "a-cascade", RunID: runID, Name: "doc"})
	db.Create(&models.Gate{RunID: runID, NodeID: "g"})
	db.Create(&models.ReactConversation{RunID: runID, NodeID: "c"})
	db.Create(&models.PreviewIssue{ID: "pi1", RunID: runID, NodeID: "preview", Body: "bug"})
	db.Create(&models.RunPreviewPort{RunID: runID, NodeID: "preview", Port: 3000, ProxyURL: "http://x"})
	db.Create(&models.SandboxLog{Name: "sbx-log-1", RunID: runID, NodeID: "n", Content: "log"})
	db.Create(&models.Sandbox{Name: "sbx-1", Purpose: "run", RunID: runID, Status: "stopped"})

	if err := runSvc.Delete(runID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var n int64
	for _, q := range []struct {
		name string
		cnt  func() int64
	}{
		{"run", func() int64 { db.Model(&models.Run{}).Where("id = ?", runID).Count(&n); return n }},
		{"state", func() int64 { db.Model(&models.StateRun{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"var", func() int64 { db.Model(&models.RunVariable{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"artifact", func() int64 { db.Model(&models.Artifact{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"gate", func() int64 { db.Model(&models.Gate{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"react", func() int64 { db.Model(&models.ReactConversation{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"previewIssue", func() int64 { db.Model(&models.PreviewIssue{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"previewPort", func() int64 { db.Model(&models.RunPreviewPort{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"sandboxLog", func() int64 { db.Model(&models.SandboxLog{}).Where("run_id = ?", runID).Count(&n); return n }},
		{"sandbox", func() int64 { db.Model(&models.Sandbox{}).Where("run_id = ?", runID).Count(&n); return n }},
	} {
		if got := q.cnt(); got != 0 {
			t.Fatalf("%s not cascaded: %d", q.name, got)
		}
	}

	if _, ok := wfSvc.Get("wf-del-run"); !ok {
		t.Fatal("WorkflowDef should remain")
	}
	if keys := keySvc.List("wf-del-run"); len(keys) != 1 {
		t.Fatalf("WorkflowAPIKey should remain: %d", len(keys))
	}
}

func TestRunService(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	if s.DB() != db {
		t.Fatal("DB() mismatch")
	}
	if got := s.List(nil, "", ""); len(got) != 0 {
		t.Fatal("empty list")
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatal("missing run")
	}

	now := time.Now()
	db.Create(&models.Run{ID: "r1", Status: "running", StartedAt: now})
	db.Create(&models.Run{ID: "r2", Status: "completed", StartedAt: now.Add(time.Second)})

	if len(s.List(nil, "", "")) != 2 {
		t.Fatal("list=2")
	}
	if run, ok := s.Get("r1"); !ok || run.Status != "running" {
		t.Fatalf("get r1: %+v %v", run, ok)
	}

	// StateRuns ordering + latest.
	db.Create(&models.StateRun{RunID: "r1", NodeID: "n1", Iteration: 1, OutputMd: "first"})
	db.Create(&models.StateRun{RunID: "r1", NodeID: "n1", Iteration: 2, OutputMd: "second"})
	states := s.States("r1")
	if len(states) != 2 || states[0].OutputMd != "first" {
		t.Fatalf("states order: %+v", states)
	}
	sr, ok := s.StateRun("r1", "n1")
	if !ok || sr.OutputMd != "second" {
		t.Fatalf("latest staterun: %+v %v", sr, ok)
	}
	if _, ok := s.StateRun("r1", "ghost"); ok {
		t.Fatal("missing node staterun")
	}

	// Variables.
	db.Create(&models.RunVariable{RunID: "r1", Name: "a", Value: 1})
	if len(s.Variables("r1")) != 1 {
		t.Fatal("vars")
	}

	// Gates.
	db.Create(&models.Gate{RunID: "r1", NodeID: "g1", Resolved: false, RequestedAt: now})
	db.Create(&models.StateRun{RunID: "r1", NodeID: "g1", Iteration: 0, Status: "waiting_human"})
	db.Create(&models.Gate{RunID: "r2", NodeID: "g2", Resolved: false, RequestedAt: now.Add(time.Second)})
	// r2 is completed — dangling unresolved gate must not enter the inbox even
	// if a stale waiting_human StateRun were present; terminal run excludes it.
	if g, ok := s.PendingGate("r1"); !ok || g.NodeID != "g1" {
		t.Fatalf("pending gate: %+v %v", g, ok)
	}
	if _, ok := s.PendingGate("rX"); ok {
		t.Fatal("no gate for rX")
	}
	// Only r1's gate is actionable: r2 is completed, so its dangling gate is
	// excluded from the inbox.
	if len(s.AllPendingGates()) != 1 {
		t.Fatal("all pending gates")
	}
	// Live run with unresolved gate but node already left waiting_human.
	db.Create(&models.Run{ID: "r3", Status: "running", StartedAt: now, CreatedAt: now})
	db.Create(&models.Gate{RunID: "r3", NodeID: "g3", Resolved: false, RequestedAt: now.Add(2 * time.Second)})
	db.Create(&models.StateRun{RunID: "r3", NodeID: "g3", Iteration: 0, Status: "completed"})
	if _, ok := s.PendingGate("r3"); ok {
		t.Fatal("transferred node gate must not be pending")
	}
	if len(s.AllPendingGates()) != 1 {
		t.Fatal("transferred gate must stay out of AllPendingGates")
	}

	// Conversations: active (done=false) preferred.
	db.Create(&models.ReactConversation{RunID: "r1", NodeID: "c1", Done: true})
	db.Create(&models.ReactConversation{RunID: "r1", NodeID: "c2", Done: false})
	if conv, ok := s.Conversation("r1"); !ok || conv.NodeID != "c2" {
		t.Fatalf("conversation active: %+v %v", conv, ok)
	}
	if len(s.Conversations("r1")) != 2 {
		t.Fatal("conversations list")
	}
	// When all done, latest completed returned.
	db.Model(&models.ReactConversation{}).Where("run_id = ?", "r1").Update("done", true)
	if conv, ok := s.Conversation("r1"); !ok || !conv.Done {
		t.Fatalf("conversation done fallback: %+v %v", conv, ok)
	}
	if _, ok := s.Conversation("rZ"); ok {
		t.Fatal("no conversation")
	}
}

func TestRunServiceListMixedSort(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	base := time.Date(2026, 7, 4, 18, 30, 0, 0, time.UTC)

	// running runs sort by started_at DESC
	db.Create(&models.Run{ID: "run-running-old", Status: "running", StartedAt: base.Add(10 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-running-new", Status: "running", StartedAt: base.Add(12 * time.Second), CreatedAt: base})

	// queued runs sort by created_at DESC (StartedAt zero)
	db.Create(&models.Run{ID: "run-queued-old", Status: "queued", CreatedAt: base.Add(8 * time.Second)})
	db.Create(&models.Run{ID: "run-queued-new", Status: "queued", CreatedAt: base.Add(11 * time.Second)})
	// tie-break: same created_at, higher id DESC
	db.Create(&models.Run{ID: "run-queued-tie-b", Status: "queued", CreatedAt: base.Add(9 * time.Second)})
	db.Create(&models.Run{ID: "run-queued-tie-a", Status: "queued", CreatedAt: base.Add(9 * time.Second)})

	// completed run sorts by started_at
	db.Create(&models.Run{ID: "run-completed", Status: "completed", StartedAt: base, CreatedAt: base.Add(-time.Hour)})

	got := s.List(nil, "", "")
	want := []string{
		"run-running-new",  // started_at 12s
		"run-queued-new",   // created_at 11s
		"run-running-old",  // started_at 10s
		"run-queued-tie-b", // created_at 9s, id tie-break DESC
		"run-queued-tie-a",
		"run-queued-old", // created_at 8s
		"run-completed",  // started_at 0
	}
	if len(got) != len(want) {
		t.Fatalf("list len = %d, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("index %d: got %s, want %s (full order: %v)", i, got[i].ID, id, idsOf(got))
		}
	}
}

func TestRunServiceListSortWhitelist(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	base := time.Date(2026, 7, 4, 18, 30, 0, 0, time.UTC)

	db.Create(&models.Run{ID: "run-high", Status: "running", Priority: models.PriorityHigh, StartedAt: base.Add(5 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-low", Status: "running", Priority: models.PriorityLow, StartedAt: base.Add(10 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-normal", Status: "completed", Priority: models.PriorityNormal, StartedAt: base.Add(8 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-queued", Status: "queued", Priority: models.PriorityNormal, CreatedAt: base.Add(12 * time.Second)})

	// Default / empty args: same hybrid-time DESC as TestRunServiceListMixedSort.
	defaultWant := []string{"run-queued", "run-low", "run-normal", "run-high"}
	for _, args := range [][]string{nil, {"", ""}, {"duration", "desc"}, {"priority", "up"}, {"started_at", ""}, {"", "desc"}} {
		got := s.List(nil, "", "", args...)
		if idsOf(got) == nil {
			t.Fatalf("args %#v: empty result", args)
		}
		for i, id := range defaultWant {
			if got[i].ID != id {
				t.Fatalf("default/illegal args %#v index %d: got %s, want %s (full: %v)", args, i, got[i].ID, id, idsOf(got))
			}
		}
	}

	prioDesc := s.List(nil, "", "", "priority", "desc")
	// Same priority: id DESC → run-queued before run-normal.
	wantPrioDesc := []string{"run-high", "run-queued", "run-normal", "run-low"}
	for i, id := range wantPrioDesc {
		if prioDesc[i].ID != id {
			t.Fatalf("priority desc index %d: got %s, want %s (full: %v)", i, prioDesc[i].ID, id, idsOf(prioDesc))
		}
	}

	prioAsc := s.List(nil, "", "", "priority", "asc")
	wantPrioAsc := []string{"run-low", "run-queued", "run-normal", "run-high"}
	for i, id := range wantPrioAsc {
		if prioAsc[i].ID != id {
			t.Fatalf("priority asc index %d: got %s, want %s (full: %v)", i, prioAsc[i].ID, id, idsOf(prioAsc))
		}
	}

	// started_at ASC: hybrid key ascending (oldest first).
	startedAsc := s.List(nil, "", "", "started_at", "asc")
	wantStartedAsc := []string{"run-high", "run-normal", "run-low", "run-queued"}
	for i, id := range wantStartedAsc {
		if startedAsc[i].ID != id {
			t.Fatalf("started_at asc index %d: got %s, want %s (full: %v)", i, startedAsc[i].ID, id, idsOf(startedAsc))
		}
	}

	startedDesc := s.List(nil, "", "", "started_at", "desc")
	for i, id := range defaultWant {
		if startedDesc[i].ID != id {
			t.Fatalf("started_at desc index %d: got %s, want %s (full: %v)", i, startedDesc[i].ID, id, idsOf(startedDesc))
		}
	}
}

func TestRunServiceListPageSortGlobalOrder(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	base := time.Date(2026, 7, 4, 18, 30, 0, 0, time.UTC)

	// Priorities 3,2,1 with two of each → global priority DESC across pages.
	ids := []string{"p3a", "p3b", "p2a", "p2b", "p1a", "p1b"}
	prios := []int{models.PriorityHigh, models.PriorityHigh, models.PriorityNormal, models.PriorityNormal, models.PriorityLow, models.PriorityLow}
	for i, id := range ids {
		db.Create(&models.Run{
			ID: id, Status: "completed", Priority: prios[i],
			StartedAt: base.Add(time.Duration(i) * time.Second), CreatedAt: base,
		})
	}

	page1, total := s.ListPage(nil, "", "", 1, 2, "priority", "desc")
	page2, _ := s.ListPage(nil, "", "", 2, 2, "priority", "desc")
	page3, _ := s.ListPage(nil, "", "", 3, 2, "priority", "desc")
	if total != 6 {
		t.Fatalf("total=%d, want 6", total)
	}
	got := append(append(idsOf(page1), idsOf(page2)...), idsOf(page3)...)
	// Within same priority, id DESC tie-break.
	want := []string{"p3b", "p3a", "p2b", "p2a", "p1b", "p1a"}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("global order index %d: got %s, want %s (full: %v)", i, got[i], id, got)
		}
	}
}

func TestRunServiceListFilters(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	base := time.Date(2026, 7, 4, 18, 30, 0, 0, time.UTC)

	db.Create(&models.Run{ID: "run-a", Status: "running", WorkflowID: "wf-1", StartedAt: base.Add(12 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-b", Status: "completed", WorkflowID: "wf-1", StartedAt: base.Add(10 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-c", Status: "running", WorkflowID: "wf-2", StartedAt: base.Add(8 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-d", Status: "failed", WorkflowID: "wf-2", StartedAt: base.Add(6 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-queued", Status: "queued", WorkflowID: "wf-1", CreatedAt: base.Add(11 * time.Second)})

	if len(s.List(nil, "", "")) != 5 {
		t.Fatalf("no filter: want 5 runs, got %d", len(s.List(nil, "", "")))
	}

	byStatus := s.List([]string{"running"}, "", "")
	if len(byStatus) != 2 {
		t.Fatalf("status filter: want 2, got %d", len(byStatus))
	}
	if byStatus[0].ID != "run-a" || byStatus[1].ID != "run-c" {
		t.Fatalf("status filter sort: got %v", idsOf(byStatus))
	}

	multiStatus := s.List([]string{"running", "failed"}, "", "")
	if len(multiStatus) != 3 {
		t.Fatalf("multi status OR: want 3, got %d", len(multiStatus))
	}
	wantMulti := []string{"run-a", "run-c", "run-d"}
	for i, id := range wantMulti {
		if multiStatus[i].ID != id {
			t.Fatalf("multi status index %d: got %s, want %s (full: %v)", i, multiStatus[i].ID, id, idsOf(multiStatus))
		}
	}

	byWf := s.List(nil, "wf-1", "")
	if len(byWf) != 3 {
		t.Fatalf("wf filter: want 3, got %d", len(byWf))
	}
	wantWf := []string{"run-a", "run-queued", "run-b"}
	for i, id := range wantWf {
		if byWf[i].ID != id {
			t.Fatalf("wf filter index %d: got %s, want %s (full: %v)", i, byWf[i].ID, id, idsOf(byWf))
		}
	}

	both := s.List([]string{"running"}, "wf-2", "")
	if len(both) != 1 || both[0].ID != "run-c" {
		t.Fatalf("AND filter: got %v, want [run-c]", idsOf(both))
	}

	multiAndWf := s.List([]string{"running", "failed"}, "wf-2", "")
	if len(multiAndWf) != 2 {
		t.Fatalf("multi status AND wf: want 2, got %d", len(multiAndWf))
	}
	wantMultiWf := []string{"run-c", "run-d"}
	for i, id := range wantMultiWf {
		if multiAndWf[i].ID != id {
			t.Fatalf("multi AND wf index %d: got %s, want %s", i, multiAndWf[i].ID, id)
		}
	}

	none := s.List([]string{"cancelled"}, "wf-1", "")
	if len(none) != 0 {
		t.Fatalf("empty intersection: got %v", idsOf(none))
	}
}

func idsOf(runs []models.Run) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.ID
	}
	return out
}

func TestArtifactService(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.Run{ID: "r1", WorkflowName: "WF"})
	s := NewArtifactService(db)

	id, err := s.Save("r1", "n1", "doc", "markdown", "hello")
	if err != nil || id == "" {
		t.Fatalf("save: %v id=%s", err, id)
	}
	// Idempotent replace by name.
	id2, err := s.Save("r1", "n2", "doc", "markdown", "updated")
	if err != nil || id2 != id {
		t.Fatalf("replace should keep id: %s vs %s", id2, id)
	}
	content, ok := s.Get("r1", "doc")
	if !ok || content != "updated" {
		t.Fatalf("get: %q %v", content, ok)
	}
	if _, ok := s.Get("r1", "missing"); ok {
		t.Fatal("missing artifact")
	}

	s.Save("r1", "n1", "second", "text", "x")
	if len(s.List("r1")) != 2 {
		t.Fatal("list scoped to run")
	}
	if len(s.ByRun("r1")) != 2 {
		t.Fatal("byrun")
	}
	for _, a := range s.ByRun("r1") {
		if a.Content != "" {
			t.Fatalf("ByRun should omit Content, got %q for %s", a.Content, a.Name)
		}
		if a.Name == "" || a.SizeBytes == 0 {
			t.Fatalf("ByRun meta incomplete: %+v", a)
		}
	}
	if len(s.All()) != 2 {
		t.Fatal("all")
	}
	for _, a := range s.All() {
		if a.Content != "" {
			t.Fatalf("All should omit Content, got %q for %s", a.Content, a.Name)
		}
		if a.Name == "" || a.SizeBytes == 0 {
			t.Fatalf("All meta incomplete: %+v", a)
		}
	}
	rec, ok := s.GetByID(id)
	if !ok || rec.WorkflowName != "WF" {
		t.Fatalf("getbyid: %+v %v", rec, ok)
	}
	if rec.Content != "updated" {
		t.Fatalf("GetByID should include Content, got %q", rec.Content)
	}
	if _, ok := s.GetByID("ghost"); ok {
		t.Fatal("missing id")
	}
}

func TestArtifactServiceDeleteByID(t *testing.T) {
	db := newTestDB(t)
	s := NewArtifactService(db)

	if err := s.DeleteByID("ghost"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("missing id: %v", err)
	}

	for _, status := range []string{"completed", "failed", "cancelled"} {
		runID := "r-" + status
		db.Create(&models.Run{ID: runID, Status: status, WorkflowName: "WF"})
		artID, err := s.Save(runID, "n1", "doc-"+status, "text", "body")
		if err != nil {
			t.Fatalf("save %s: %v", status, err)
		}
		if err := s.DeleteByID(artID); err != nil {
			t.Fatalf("delete terminal %s: %v", status, err)
		}
		if _, ok := s.GetByID(artID); ok {
			t.Fatalf("artifact %s should be gone after delete", artID)
		}
		if _, ok := s.Get(runID, "doc-"+status); ok {
			t.Fatalf("Get should miss deleted %s", status)
		}
	}

	for _, status := range []string{"running", "waiting_human"} {
		runID := "r-" + status
		db.Create(&models.Run{ID: runID, Status: status})
		artID, err := s.Save(runID, "n1", "keep", "text", "x")
		if err != nil {
			t.Fatalf("save %s: %v", status, err)
		}
		if err := s.DeleteByID(artID); !errors.Is(err, ErrArtifactRunNotTerminal) {
			t.Fatalf("non-terminal %s: %v", status, err)
		}
		if _, ok := s.GetByID(artID); !ok {
			t.Fatalf("artifact must remain for non-terminal %s", status)
		}
	}

	// Same-name Save after hard delete creates a new id.
	db.Create(&models.Run{ID: "r-rewrite", Status: "completed"})
	oldID, err := s.Save("r-rewrite", "n1", "doc", "text", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteByID(oldID); err != nil {
		t.Fatal(err)
	}
	newID, err := s.Save("r-rewrite", "n1", "doc", "text", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if newID == oldID {
		t.Fatal("Save after delete should allocate a new id")
	}
	content, ok := s.Get("r-rewrite", "doc")
	if !ok || content != "v2" {
		t.Fatalf("rewrite get: %q %v", content, ok)
	}
}

func TestDashboardService(t *testing.T) {
	db := newTestDB(t)
	projects := NewProjectService(db)
	s := NewDashboardService(db, projects)
	db.Create(&models.Run{ID: "a", Status: "running"})
	db.Create(&models.Run{ID: "b", Status: "waiting_human"})
	db.Create(&models.Run{ID: "c", Status: "failed"})
	db.Create(&models.Run{ID: "d", Status: "completed"})
	db.Create(&models.Run{ID: "e", Status: "completed"})
	db.Create(&models.WorkflowDef{ID: "wf"})
	db.Create(&models.Artifact{ID: "art", RunID: "a", Name: "n"})

	st := s.Compute()
	if st.Running != 1 || st.WaitingHuman != 1 || st.Failed != 1 || st.Completed != 2 {
		t.Fatalf("stats runs: %+v", st)
	}
	if st.Workflows != 1 || st.Artifacts != 1 {
		t.Fatalf("stats counts: %+v", st)
	}
	// No project Usage → Token fields stay null (not 0).
	if st.TotalTokens != nil || st.WorkflowTokens != nil || st.PMTokens != nil {
		t.Fatalf("expected null tokens without usage, got total=%v wf=%v pm=%v",
			st.TotalTokens, st.WorkflowTokens, st.PMTokens)
	}
}
