package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSaveStatusLifecycle_publishMetaOnlyKeepsPublished(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-meta", ProjectID: models.DefaultProjectID, Name: "Orig", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatalf("create: %v", err)
	}
	pub, err := s.Publish("wf-meta")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.Status != "published" {
		t.Fatalf("want published, got %s", pub.Status)
	}
	prevUpdated := pub.UpdatedAt

	// Same graph, rename only — must stay published and update name.
	time.Sleep(2 * time.Millisecond)
	upd := &models.WorkflowDef{
		ID: "wf-meta", Name: "Renamed", Description: "d", NeedsRepo: true,
		Status: "draft", // client may always send draft; server must not honor it
		Graph:  validGraph(),
	}
	if err := s.Save(upd); err != nil {
		t.Fatalf("meta save: %v", err)
	}
	got, ok := s.Get("wf-meta")
	if !ok {
		t.Fatal("missing")
	}
	if got.Status != "published" {
		t.Fatalf("meta-only save downgraded status to %s", got.Status)
	}
	if got.Name != "Renamed" || got.Description != "d" || !got.NeedsRepo {
		t.Fatalf("metadata not updated: %+v", got)
	}
	if !got.UpdatedAt.After(prevUpdated) {
		t.Fatal("UpdatedAt should bump on meta change")
	}
}

func TestSaveStatusLifecycle_publishGraphChangeBecomesDraft(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-graph", ProjectID: models.DefaultProjectID, Name: "G", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-graph"); err != nil {
		t.Fatal(err)
	}

	g := validGraph()
	g.Nodes[0].Label = "Changed"
	upd := &models.WorkflowDef{ID: "wf-graph", Name: "G", Status: "draft", Graph: g}
	if err := s.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-graph")
	if got.Status != "draft" {
		t.Fatalf("graph change should draft, got %s", got.Status)
	}
}

func TestSaveStatusLifecycle_publishLayoutOnlyKeepsPublished(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-move", ProjectID: models.DefaultProjectID, Name: "M", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-move"); err != nil {
		t.Fatal(err)
	}

	g := validGraph()
	g.Nodes[0].Position = models.Position{X: -136, Y: -88}
	upd := &models.WorkflowDef{ID: "wf-move", Name: "M", Status: "draft", Graph: g}
	if err := s.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-move")
	if got.Status != "published" {
		t.Fatalf("moving nodes should keep published, got %s", got.Status)
	}
	if p := got.Graph.Nodes[0].Position; p.X != -136 || p.Y != -88 {
		t.Fatalf("new position not stored: %+v", p)
	}
}

func TestSaveStatusLifecycle_publishIdenticalPUTKeepsPublished(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-same", ProjectID: models.DefaultProjectID, Name: "S", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	pub, err := s.Publish("wf-same")
	if err != nil {
		t.Fatal(err)
	}
	if pub.Status != "published" {
		t.Fatalf("want published, got %s", pub.Status)
	}
	before, _ := s.Get("wf-same")
	prevUpdated := before.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	upd := &models.WorkflowDef{ID: "wf-same", Name: "S", Status: "draft", Graph: validGraph()}
	if err := s.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-same")
	if got.Status != "published" {
		t.Fatalf("identical PUT should stay published, got %s", got.Status)
	}
	if !got.UpdatedAt.Equal(prevUpdated) {
		t.Fatalf("UpdatedAt should not bump on no-op save: was %v now %v", prevUpdated, got.UpdatedAt)
	}
}

func TestSaveStatusLifecycle_draftNoDiffStaysDraft(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-draft", ProjectID: models.DefaultProjectID, Name: "D", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	upd := &models.WorkflowDef{ID: "wf-draft", Name: "D", Status: "published", Graph: validGraph()}
	if err := s.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-draft")
	if got.Status != "draft" {
		t.Fatalf("must not promote draft→published via Save, got %s", got.Status)
	}
}

// publish + legacy output.result in DB; client sends cleaned results + rename
// must stay published (review v1: meta-only must not demote on migrate/clean).
func TestSaveStatusLifecycle_publishLegacyOutputMetaOnlyKeepsPublished(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	g := validGraph()
	g.Nodes[1].Config = map[string]any{"result": "{{artifact(\"plan.md\")}}"}
	wf := &models.WorkflowDef{ID: "wf-legacy", ProjectID: models.DefaultProjectID, Name: "Legacy", Graph: g}
	if err := s.Save(wf); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Publish("wf-legacy"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	cleaned := validGraph()
	cleaned.Nodes[1].Config = map[string]any{"results": []any{"{{artifact(\"plan.md\")}}"}}
	upd := &models.WorkflowDef{
		ID: "wf-legacy", Name: "Renamed Legacy",
		Status: "draft",
		Graph:  cleaned,
	}
	if err := s.Save(upd); err != nil {
		t.Fatalf("meta+clean save: %v", err)
	}
	got, ok := s.Get("wf-legacy")
	if !ok {
		t.Fatal("missing")
	}
	if got.Status != "published" {
		t.Fatalf("legacy clean + rename should stay published, got %s", got.Status)
	}
	if got.Name != "Renamed Legacy" {
		t.Fatalf("name not updated: %s", got.Name)
	}
}
