package services

import (
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestArtifactSaveResolvesProject(t *testing.T) {
	db := newTestDB(t)
	arts := NewArtifactService(db)
	db.Create(&models.WorkflowDef{ID: "wf1", ProjectID: "p-wf", Name: "W"})
	db.Create(&models.Run{ID: "run-wf", WorkflowID: "wf1", WorkflowName: "W"})
	db.Create(&models.Sandbox{Name: "sb-test", Purpose: "test", RunID: "test-run", ProjectID: "p-sb"})
	// A sandbox sharing the workflow run id must not override the workflow project.
	db.Create(&models.Sandbox{Name: "sb-run", Purpose: "run", RunID: "run-wf", ProjectID: "p-sb"})

	cases := []struct{ runID, want string }{
		{"run-wf", "p-wf"},
		{"test-run", "p-sb"},
	}
	for _, c := range cases {
		id, err := arts.Save(c.runID, "n", "out.md", "markdown", "x")
		if err != nil {
			t.Fatalf("%s: %v", c.runID, err)
		}
		rec, _ := arts.GetByID(id)
		if rec.ProjectID != c.want {
			t.Fatalf("%s: project=%q want %q", c.runID, rec.ProjectID, c.want)
		}
	}

	if _, err := arts.Save("nowhere", "n", "out.md", "markdown", "x"); !errors.Is(err, ErrArtifactNoProject) {
		t.Fatalf("unknown run: err=%v want ErrArtifactNoProject", err)
	}
	var n int64
	db.Model(&models.Artifact{}).Where("run_id = ?", "nowhere").Count(&n)
	if n != 0 {
		t.Fatalf("unresolvable save must not write, got %d rows", n)
	}

	// Overwrites keep the stored project.
	if _, err := arts.Save("run-wf", "n2", "out.md", "markdown", "y"); err != nil {
		t.Fatal(err)
	}
	rec, _ := arts.GetRecord("run-wf", "out.md")
	if rec.ProjectID != "p-wf" || rec.Revision != 2 {
		t.Fatalf("overwrite: %+v", rec)
	}
}

func TestArtifactTree(t *testing.T) {
	db := newTestDB(t)
	arts := NewArtifactService(db)
	now := time.Now()
	for _, p := range []models.Project{
		{ID: "pb", Name: "Beta"}, {ID: "pa", Name: "Alpha"}, {ID: "pe", Name: "Empty"},
	} {
		p.Variables, p.CreatedAt, p.UpdatedAt = []models.ProjectVariable{}, now, now
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&models.WorkflowDef{ID: "wf-z", ProjectID: "pa", Name: "Zeta"})
	db.Create(&models.WorkflowDef{ID: "wf-b", ProjectID: "pa", Name: "Build"})
	for i, a := range []models.Artifact{
		{ProjectID: "pa", WorkflowID: "wf-z", WorkflowName: "Zeta"},
		{ProjectID: "pa", WorkflowID: "wf-b", WorkflowName: "old name"},
		{ProjectID: "pa", WorkflowID: "wf-b", WorkflowName: "old name"},
		{ProjectID: "pa"},
		{ProjectID: "pb", WorkflowID: "wf-gone", WorkflowName: "Deleted WF"},
	} {
		a.ID = "a" + string(rune('0'+i))
		a.RunID, a.Name, a.CreatedAt, a.UpdatedAt = "r", a.ID, now, now
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}

	tree, err := arts.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 || tree[0].ProjectID != "pa" || tree[1].ProjectID != "pb" {
		t.Fatalf("projects: %+v", tree)
	}
	a := tree[0]
	if a.ProjectName != "Alpha" || a.Count != 4 || a.SessionCount != 1 || len(a.Workflows) != 2 {
		t.Fatalf("alpha: %+v", a)
	}
	if w := a.Workflows[0]; w.WorkflowID != "wf-b" || w.WorkflowName != "Build" || w.Count != 2 {
		t.Fatalf("alpha wf[0]: %+v", w)
	}
	if w := a.Workflows[1]; w.WorkflowID != "wf-z" || w.Count != 1 {
		t.Fatalf("alpha wf[1]: %+v", w)
	}
	b := tree[1]
	if b.Count != 1 || b.SessionCount != 0 || len(b.Workflows) != 1 || b.Workflows[0].WorkflowName != "Deleted WF" {
		t.Fatalf("beta: %+v", b)
	}
}
