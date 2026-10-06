package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// plan g1.1 / g1.4 — new rows and AutoMigrate zero-value read as false.
func TestShowOnHomeDefaultsFalseOnCreate(t *testing.T) {
	db := newTestDB(t)
	svc := NewWorkflowService(db)

	wf := &models.WorkflowDef{
		ID:         "wf-home-new",
		ProjectID:  models.DefaultProjectID,
		Name:       "HomeNew",
		Graph:      validGraph(),
		ShowOnHome: true, // client cannot force-on via create Save
	}
	if err := svc.Save(wf); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, ok := svc.Get("wf-home-new")
	if !ok {
		t.Fatal("missing")
	}
	if got.ShowOnHome {
		t.Fatal("create ShowOnHome want false")
	}

	// Seed a legacy row without the field (zero value) — reads as hidden.
	legacy := models.WorkflowDef{
		ID:               "wf-home-legacy",
		ProjectID:        models.DefaultProjectID,
		Name:             "Legacy",
		Version:          1,
		PublishedVersion: 1,
		Graph:            validGraph(),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	reloaded, ok := svc.Get("wf-home-legacy")
	if !ok {
		t.Fatal("missing legacy")
	}
	if reloaded.ShowOnHome {
		t.Fatal("legacy row ShowOnHome want false")
	}
}

// plan g1.2 / g1.4 — PATCH-equivalent service must not demote or rewrite graph.
func TestUpdateShowOnHomeDoesNotDemotePublished(t *testing.T) {
	db := newTestDB(t)
	svc := NewWorkflowService(db)

	wf := models.WorkflowDef{
		ID:               "wf-home-only",
		ProjectID:        models.DefaultProjectID,
		Name:             "HomeOnly",
		Version:          3,
		PublishedVersion: 3,
		Graph: models.Graph{
			Nodes: []models.Node{
				{ID: "in", Type: "input", Label: "Fresh Start"},
				{ID: "g", Type: "human_gate", Label: "Gate"},
				{ID: "out", Type: "output", Label: "End"},
			},
			Edges: []models.Edge{
				{ID: "e1", Source: "in", Target: "g"},
				{ID: "e2", Source: "g", Target: "out"},
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := svc.UpdateShowOnHome(wf.ID, true)
	if err != nil {
		t.Fatalf("UpdateShowOnHome: %v", err)
	}
	if !got.ShowOnHome {
		t.Fatal("ShowOnHome not persisted")
	}
	if got.Status() != "published" {
		t.Fatalf("status demoted to %q", got.Status())
	}
	if got.Version != 3 {
		t.Fatalf("version mutated to %d", got.Version)
	}
	if len(got.Graph.Nodes) != 3 || got.Graph.Nodes[0].Label != "Fresh Start" {
		t.Fatalf("graph rewritten: %+v", got.Graph.Nodes)
	}

	reloaded, ok := svc.Get(wf.ID)
	if !ok {
		t.Fatal("missing after update")
	}
	if !reloaded.ShowOnHome || reloaded.Status() != "published" || reloaded.Graph.Nodes[0].Label != "Fresh Start" {
		t.Fatalf("persisted row corrupted: show=%v status=%s label=%s", reloaded.ShowOnHome, reloaded.Status(), reloaded.Graph.Nodes[0].Label)
	}

	if _, err := svc.UpdateShowOnHome("wf-missing", true); err != ErrWorkflowNotFound {
		t.Fatalf("missing id: %v", err)
	}
}

// plan g1.3 / g1.4 — copy and import never inherit an enabled flag.
func TestCopyAndImportResetShowOnHome(t *testing.T) {
	db := newTestDB(t)
	svc := NewWorkflowService(db)

	src := &models.WorkflowDef{
		ID: "wf-home-src", ProjectID: models.DefaultProjectID, Name: "工作流 Home",
		Graph: validGraph(),
	}
	if err := svc.Save(src); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish("wf-home-src"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateShowOnHome("wf-home-src", true); err != nil {
		t.Fatal(err)
	}

	copied, err := svc.Copy("wf-home-src", "工作流 Home 副本")
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if copied.ShowOnHome {
		t.Fatal("copy inherited showOnHome")
	}

	imported, err := svc.Import(envelopeJSON(validEnvelope("Imported Home")), models.DefaultProjectID)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported.ShowOnHome {
		t.Fatal("import ShowOnHome want false")
	}
}

// plan g1.3 / g1.4 — full Save that carries the stored true keeps it (and published).
func TestSavePreservesShowOnHomeWhenSet(t *testing.T) {
	db := newTestDB(t)
	svc := NewWorkflowService(db)

	wf := &models.WorkflowDef{ID: "wf-home-save", ProjectID: models.DefaultProjectID, Name: "Keep", Graph: validGraph()}
	if err := svc.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish("wf-home-save"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateShowOnHome("wf-home-save", true); err != nil {
		t.Fatal(err)
	}

	upd := &models.WorkflowDef{
		ProjectID: models.DefaultProjectID,
		ID:        "wf-home-save", Name: "Keep", Graph: validGraph(), ShowOnHome: true,
	}
	if err := svc.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get("wf-home-save")
	if !got.ShowOnHome {
		t.Fatal("Save dropped showOnHome")
	}
	if got.Status() != "published" {
		t.Fatalf("status=%s", got.Status())
	}
}
