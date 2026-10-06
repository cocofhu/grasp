package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestExtractPmCitationsRejectsFalsePositives(t *testing.T) {
	cases := []string{
		"run:trigger",
		"run: trigger",
		"Run trigger",
		"Please run the tests",
		"run: npm install",
		"the run trigger was api",
		"gate:foo",
		"workflow:main",
		"plan:next",
		"artifact:trigger",
	}
	for _, text := range cases {
		if got := extractPmCitations(text); len(got) != 0 {
			t.Fatalf("text %q: want no citations, got %+v", text, got)
		}
	}
}

func TestExtractPmCitationsTruePositives(t *testing.T) {
	text := "See run run-a1b2c3d4 and workflow wf-deadbeef plus artifact art-11223344 and artifact:research.json; gate run-a1b2c3d4:human_gate; plan g1.2"
	got := extractPmCitations(text)
	want := map[string]string{
		"run:run-a1b2c3d4":             "run-a1b2c3d4",
		"workflow:wf-deadbeef":         "wf-deadbeef",
		"artifact:art-11223344":        "art-11223344",
		"artifact:research.json":       "research.json",
		"gate:run-a1b2c3d4:human_gate": "run-a1b2c3d4:human_gate",
		"plan:g1.2":                    "g1.2",
	}
	found := map[string]string{}
	for _, c := range got {
		found[c.Type+":"+c.TargetID] = c.TargetID
		if c.SummarySnippet != "" {
			t.Fatalf("extract should leave SummarySnippet empty, got %q", c.SummarySnippet)
		}
	}
	for key, id := range want {
		if found[key] != id {
			t.Fatalf("missing %s in %+v", key, got)
		}
	}
}

func TestExtractPmCitationsMixedOnlyLegal(t *testing.T) {
	text := "Please run the tests on run:trigger then check run:run-abcdef12 and ignore run: npm install"
	got := extractPmCitations(text)
	if len(got) != 1 || got[0].Type != "run" || got[0].TargetID != "run-abcdef12" {
		t.Fatalf("got %+v", got)
	}
}

func TestValidPmCitationShape(t *testing.T) {
	if validPmCitationShape("run", "trigger") {
		t.Fatal("trigger must be invalid run id")
	}
	if !validPmCitationShape("run", "run-a1b2c3d4") {
		t.Fatal("legal run id")
	}
	if !validPmCitationShape("plan", "run-a1b2c3d4:g1") {
		t.Fatal("scoped plan id")
	}
	if validPmCitationShape("artifact", "npm") {
		t.Fatal("bare npm must not be artifact")
	}
}

func TestFilterAndEnrichCitationsFailClosedAndSnippet(t *testing.T) {
	db := setupPmDB(t)
	ps := NewProjectService(db)
	p, err := ps.Create("CiteProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wfSvc := NewWorkflowService(db)
	wfID := "wf-abcd1234"
	wf := &models.WorkflowDef{ID: wfID, ProjectID: p.ID, Name: "Clarify Flow", Graph: validGraph()}
	if err := wfSvc.Save(wf); err != nil {
		t.Fatal(err)
	}
	runs := NewRunService(db)
	arts := NewArtifactService(db)
	runID := "run-a1b2c3d4"
	db.Create(&models.Run{
		ID: runID, WorkflowID: wfID, WorkflowName: wf.Name, Status: "running",
		Title: "需求澄清", StartedAt: time.Now(),
	})
	artID, err := arts.Save(runID, "n1", "research.json", "json", `{"ok":true}`)
	if err != nil {
		t.Fatal(err)
	}
	arts.Save(runID, "plan", "plan.json", "json", `{"goals":[{"id":"g1","status":"done","subgoals":[{"id":"g1.2","status":"pending"}]}]}`)
	db.Create(&models.Gate{
		RunID: runID, NodeID: "human_gate", WorkflowID: wfID, WorkflowName: wf.Name,
		Title: "请审批", Resolved: false, RequestedAt: time.Now(),
	})
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "human_gate", Iteration: 0, Status: "waiting_human",
	})

	pm := NewPmService(db, nil)
	thr, err := pm.CreateThread(p.ID, "u1", "t", "pm", "chat")
	if err != nil {
		t.Fatal(err)
	}

	runner := NewPmTurnRunner(pm, nil)
	runner.SetCitationDeps(runs, arts, wfSvc)

	cands := []models.ProgressCitation{
		{Type: "run", TargetID: runID},
		{Type: "run", TargetID: "run-dead0000"}, // missing
		{Type: "workflow", TargetID: wfID},
		{Type: "artifact", TargetID: artID},
		{Type: "artifact", TargetID: "research.json"},
		{Type: "gate", TargetID: runID + ":human_gate"},
		{Type: "plan", TargetID: "g1.2"},
		{Type: "plan", TargetID: "g9"}, // missing in plan
	}
	got := runner.filterAndEnrichCitations(thr.ID, cands)
	if len(got) != 6 {
		t.Fatalf("want 6 kept, got %d: %+v", len(got), got)
	}
	byKey := map[string]models.ProgressCitation{}
	for _, c := range got {
		byKey[c.Type+":"+c.TargetID] = c
	}
	if sn := byKey["run:"+runID].SummarySnippet; sn != "需求澄清" {
		t.Fatalf("run snippet=%q", sn)
	}
	if sn := byKey["workflow:"+wfID].SummarySnippet; sn != "Clarify Flow" {
		t.Fatalf("wf snippet=%q", sn)
	}
	if _, ok := byKey["run:run-dead0000"]; ok {
		t.Fatal("missing run must be dropped")
	}
	if _, ok := byKey["plan:g9"]; ok {
		t.Fatal("missing plan goal must be dropped")
	}
}

func TestFilterAndEnrichCitationsUnavailableDepsDropAll(t *testing.T) {
	db := setupPmDB(t)
	ps := NewProjectService(db)
	p, err := ps.Create("CiteFail", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, nil)
	thr, err := pm.CreateThread(p.ID, "u1", "t", "pm", "chat")
	if err != nil {
		t.Fatal(err)
	}
	runner := NewPmTurnRunner(pm, nil) // no citation deps
	got := runner.filterAndEnrichCitations(thr.ID, []models.ProgressCitation{
		{Type: "run", TargetID: "run-a1b2c3d4"},
	})
	if len(got) != 0 {
		t.Fatalf("fail-closed want empty, got %+v", got)
	}
}

func TestFilterKeepsBodyPathIndependent(t *testing.T) {
	// Documented contract: filter failures must not prevent callers from
	// appending the assistant body. This test only asserts filter returns
	// safely on bad thread.
	runner := NewPmTurnRunner(nil, nil)
	got := runner.filterAndEnrichCitations("missing", []models.ProgressCitation{
		{Type: "run", TargetID: "run-a1b2c3d4"},
	})
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}
