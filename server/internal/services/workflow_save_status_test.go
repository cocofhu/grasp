package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func versionCount(t *testing.T, s *WorkflowService, id string) int {
	t.Helper()
	return len(s.Versions(id))
}

func TestSaveIdenticalContentIsNoop(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-same", ProjectID: models.DefaultProjectID, Name: "S", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-same"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Get("wf-same")

	time.Sleep(2 * time.Millisecond)
	upd := &models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-same", Name: "S", Version: 99, Graph: validGraph()}
	if err := s.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-same")
	if got.Version != 1 || got.Status() != models.WorkflowStatusPublished {
		t.Fatalf("identical save changed head: v=%d status=%s", got.Version, got.Status())
	}
	if !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("UpdatedAt bumped on no-op save")
	}
	if n := versionCount(t, s, "wf-same"); n != 1 {
		t.Fatalf("no-op save created a version: %d", n)
	}
}

func TestSaveContentChangeAppendsVersion(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-graph", ProjectID: models.DefaultProjectID, Name: "G", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-graph"); err != nil {
		t.Fatal(err)
	}

	for i, mutate := range []func(*models.WorkflowDef){
		func(w *models.WorkflowDef) { w.Graph.Nodes[0].Label = "Changed" },
		func(w *models.WorkflowDef) { w.Graph.Nodes[0].Position = models.Position{X: -136, Y: -88} },
		func(w *models.WorkflowDef) { w.Name = "G renamed" },
		func(w *models.WorkflowDef) { w.Description = "d" },
	} {
		cur, _ := s.Get("wf-graph")
		upd := &models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: cur.ID, Name: cur.Name, Description: cur.Description, Graph: cur.Graph}
		mutate(upd)
		if err := s.Save(upd); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if upd.Version != i+2 {
			t.Fatalf("save %d: version = %d, want %d", i, upd.Version, i+2)
		}
	}
	got, _ := s.Get("wf-graph")
	if got.Version != 5 || got.PublishedVersion != 1 || got.Status() != models.WorkflowStatusDraft {
		t.Fatalf("head: v=%d pub=%d status=%s", got.Version, got.PublishedVersion, got.Status())
	}
	vs := s.Versions("wf-graph")
	if len(vs) != 5 || vs[0].Version != 5 || vs[0].Name != "G renamed" || vs[0].Description != "d" || vs[0].Source != models.VersionSourceSave {
		t.Fatalf("versions: %+v", vs[0])
	}
	if vs[0].NodeCount != len(validGraph().Nodes) {
		t.Fatalf("nodeCount = %d", vs[0].NodeCount)
	}
}

func TestSaveSettingsOnlyKeepsVersion(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-meta", ProjectID: models.DefaultProjectID, Name: "M", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-meta"); err != nil {
		t.Fatal(err)
	}
	upd := &models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-meta", Name: "M", NeedsRepo: true, Graph: validGraph()}
	if err := s.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-meta")
	if !got.NeedsRepo {
		t.Fatal("needsRepo not persisted")
	}
	if got.Version != 1 || got.Status() != models.WorkflowStatusPublished || versionCount(t, s, "wf-meta") != 1 {
		t.Fatalf("settings-only save must not version: v=%d status=%s", got.Version, got.Status())
	}
}

func TestSaveAsRecordsSource(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-pm", ProjectID: models.DefaultProjectID, Name: "P", Graph: validGraph()}
	if err := s.SaveAs(wf, models.VersionSourcePM); err != nil {
		t.Fatal(err)
	}
	g := validGraph()
	g.Nodes[0].Label = "by pm"
	if err := s.SaveAs(&models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-pm", Name: "P", Graph: g}, models.VersionSourcePM); err != nil {
		t.Fatal(err)
	}
	for _, v := range s.Versions("wf-pm") {
		if v.Source != models.VersionSourcePM {
			t.Fatalf("v%d source = %q", v.Version, v.Source)
		}
	}
}

func TestPublishStampsVersionRow(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-pub", ProjectID: models.DefaultProjectID, Name: "P", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	g := validGraph()
	g.Nodes[0].Label = "v2"
	if err := s.Save(&models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-pub", Name: "P", Graph: g}); err != nil {
		t.Fatal(err)
	}
	pub, err := s.Publish("wf-pub")
	if err != nil {
		t.Fatal(err)
	}
	if pub.PublishedVersion != 2 || pub.Version != 2 {
		t.Fatalf("publish: %+v", pub)
	}
	vs := s.Versions("wf-pub")
	if vs[0].Version != 2 || vs[0].PublishedAt == nil {
		t.Fatalf("v2 not stamped: %+v", vs[0])
	}
	if vs[1].PublishedAt != nil {
		t.Fatalf("v1 must stay unpublished: %+v", vs[1])
	}
	if n := versionCount(t, s, "wf-pub"); n != 2 {
		t.Fatalf("publish must not add a version, got %d", n)
	}
}

func TestRestoreAppendsVersion(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-r", ProjectID: models.DefaultProjectID, Name: "R", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	g := validGraph()
	g.Nodes[0].Label = "v2"
	if err := s.Save(&models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-r", Name: "R", Graph: g}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Restore("wf-r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || got.Graph.Nodes[0].Label != validGraph().Nodes[0].Label {
		t.Fatalf("restore head: v=%d label=%s", got.Version, got.Graph.Nodes[0].Label)
	}
	v2, _ := s.VersionGraph("wf-r", 2)
	if v2.Nodes[0].Label != "v2" {
		t.Fatal("restore must not rewrite history")
	}
	// Restoring the head content again is a no-op.
	if again, err := s.Restore("wf-r", 3); err != nil || again.Version != 3 {
		t.Fatalf("restore head again: v=%d err=%v", again.Version, err)
	}
}

func TestPruneKeepsPublishedAndRunReferencedVersions(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-prune", ProjectID: models.DefaultProjectID, Name: "P", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-prune"); err != nil { // v1 published
		t.Fatal(err)
	}
	save := func(i int) {
		g := validGraph()
		g.Nodes[0].Label = fmt.Sprintf("rev %d", i)
		if err := s.Save(&models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-prune", Name: "P", Graph: g}); err != nil {
			t.Fatal(err)
		}
	}
	save(2) // v2, referenced by a run below
	if err := db.Create(&models.Run{ID: "run-v2", WorkflowID: "wf-prune", WorkflowVersion: 2, Status: "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	total := MaxUnpinnedWorkflowVersions + 5
	for i := 3; i <= total; i++ {
		save(i)
	}

	present := map[int]bool{}
	for _, v := range s.Versions("wf-prune") {
		present[v.Version] = true
	}
	if !present[1] || !present[2] {
		t.Fatalf("published v1 / run-referenced v2 pruned: v1=%v v2=%v", present[1], present[2])
	}
	if !present[total] {
		t.Fatal("head pruned")
	}
	// Unpinned: v3..total; only the newest MaxUnpinnedWorkflowVersions + head survive.
	if present[3] || present[4] {
		t.Fatal("oldest unpinned versions should be pruned")
	}
	if got := len(present); got != MaxUnpinnedWorkflowVersions+3 {
		t.Fatalf("kept %d versions, want %d", got, MaxUnpinnedWorkflowVersions+3)
	}
}
