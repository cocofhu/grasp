package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestEscapeLikeAndInboxHelpers(t *testing.T) {
	if got := escapeLikePattern(`a%b_c\d`); got == `a%b_c\d` {
		t.Fatalf("escapeLikePattern unchanged: %q", got)
	}
	db := newTestDB(t)
	s := NewRunService(db)
	g := models.Graph{Nodes: []models.Node{agentNode("r1", "澄清", testClarifyCaps)}}
	db.Create(&models.Run{ID: "run-ctx", Status: "waiting_human", Graph: g})
	db.Create(&models.ReactConversation{RunID: "run-ctx", NodeID: "r1", Iteration: 1})
	db.Create(&models.Gate{RunID: "run-ctx", NodeID: "r1", Iteration: 1, Title: "g", Resolved: false})
	db.Create(&models.StateRun{RunID: "run-ctx", NodeID: "r1", Iteration: 1, Status: "waiting_human"})

	if _, _, ok := s.ClarifyContext("run-ctx", "r1", 1); !ok {
		t.Fatal("ClarifyContext")
	}
	if _, ok := s.PendingGateAt("run-ctx", "r1", 1); !ok {
		t.Fatal("PendingGateAt")
	}
	if ClarifyLabel(g, "r1") == "" {
		t.Fatal("ClarifyLabel")
	}
	if _, _, ok := s.ClarifyContext("missing", "r1", 1); ok {
		t.Fatal("missing clarify")
	}
}

func TestArtifactAllPage(t *testing.T) {
	db := newTestDB(t)
	arts := NewArtifactService(db)
	db.Create(&models.WorkflowDef{ID: "wf", ProjectID: "p1", Name: "W"})
	db.Create(&models.Run{ID: "r-art", WorkflowID: "wf", WorkflowName: "W", Title: "Run Title", Status: "succeeded"})
	id, err := arts.Save("r-art", "n", "a.md", "markdown", "hi")
	if err != nil {
		t.Fatal(err)
	}
	page, total := arts.AllPage(ArtifactFilter{ProjectID: "p1", WorkflowID: "wf"}, 1, 10)
	if total < 1 || len(page) < 1 {
		t.Fatalf("AllPage: total=%d n=%d", total, len(page))
	}
	for _, a := range page {
		if a.Content != "" {
			t.Fatalf("AllPage should omit Content, got %q for %s", a.Content, a.Name)
		}
		if a.Name == "" || a.SizeBytes == 0 {
			t.Fatalf("AllPage meta incomplete: %+v", a)
		}
		if a.RunTitle != "Run Title" {
			t.Fatalf("AllPage run_title: got %q", a.RunTitle)
		}
	}
	rec, ok := arts.GetByID(id)
	if !ok || rec.Content != "hi" {
		t.Fatalf("GetByID should include Content: %+v ok=%v", rec, ok)
	}
	if a := page[0]; a.ProjectID != "p1" {
		t.Fatalf("AllPage project_id: got %q", a.ProjectID)
	}
	if _, total = arts.AllPage(ArtifactFilter{ProjectID: "p1", Q: "a_"}, 1, 10); total != 0 {
		t.Fatalf("LIKE wildcard must be escaped, total=%d", total)
	}
	if _, total = arts.AllPage(ArtifactFilter{ProjectID: "p-other"}, 1, 10); total != 0 {
		t.Fatalf("other project must not see artifacts, total=%d", total)
	}
}

func TestArtifactAllPageByRun(t *testing.T) {
	db := newTestDB(t)
	arts := NewArtifactService(db)
	now := time.Now()

	// Three runs under wf (project p1); run-new has two artifacts (newest latestAt).
	db.Create(&models.Run{ID: "run-old", WorkflowID: "wf", WorkflowName: "W", Title: "Old Run", Status: "succeeded"})
	db.Create(&models.Run{ID: "run-mid", WorkflowID: "wf", WorkflowName: "W", Title: "Mid Run", Status: "succeeded"})
	db.Create(&models.Run{ID: "run-new", WorkflowID: "wf", WorkflowName: "W", Title: "New Run", Status: "succeeded"})
	db.Create(&models.Run{ID: "run-other", WorkflowID: "wf-b", WorkflowName: "B", Title: "Other WF", Status: "succeeded"})
	db.Create(&models.Run{ID: "run-loose", WorkflowID: "", WorkflowName: "", Title: "Loose", Status: "succeeded"})

	db.Create(&models.Artifact{ID: "a-old", ProjectID: "p1", RunID: "run-old", WorkflowID: "wf", NodeID: "n", Name: "old.md", CreatedAt: now.Add(-3 * time.Hour)})
	db.Create(&models.Artifact{ID: "a-mid", ProjectID: "p1", RunID: "run-mid", WorkflowID: "wf", NodeID: "n", Name: "mid.md", CreatedAt: now.Add(-2 * time.Hour)})
	db.Create(&models.Artifact{ID: "a-new1", ProjectID: "p1", RunID: "run-new", WorkflowID: "wf", NodeID: "plan", Name: "plan.json", CreatedAt: now.Add(-time.Hour)})
	db.Create(&models.Artifact{ID: "a-new2", ProjectID: "p1", RunID: "run-new", WorkflowID: "wf", NodeID: "research", Name: "research.json", CreatedAt: now})
	db.Create(&models.Artifact{ID: "a-other", ProjectID: "p1", RunID: "run-other", WorkflowID: "wf-b", NodeID: "n", Name: "other.md", CreatedAt: now})
	db.Create(&models.Artifact{ID: "a-loose", ProjectID: "p1", RunID: "run-loose", WorkflowID: "", NodeID: "n", Name: "loose.md", CreatedAt: now})

	// Page size 2 Runs: newest two are run-new + run-mid; total Run count = 3 under wf.
	page1, total := arts.AllPageByRun(ArtifactFilter{ProjectID: "p1", WorkflowID: "wf"}, 1, 2)
	if total != 3 {
		t.Fatalf("AllPageByRun total want 3 Runs, got %d", total)
	}
	runIDs := uniqueRunIDs(page1)
	if len(runIDs) != 2 {
		t.Fatalf("page1 want 2 Runs, got %v (arts=%d)", runIDs, len(page1))
	}
	if runIDs[0] != "run-new" || runIDs[1] != "run-mid" {
		t.Fatalf("page1 order want [run-new, run-mid], got %v", runIDs)
	}
	// Whole-Run: run-new must bring both artifacts (not split across pages).
	newCount := 0
	for _, a := range page1 {
		if a.RunID == "run-new" {
			newCount++
		}
	}
	if newCount != 2 {
		t.Fatalf("run-new whole-Run want 2 arts on page1, got %d", newCount)
	}

	page2, total2 := arts.AllPageByRun(ArtifactFilter{ProjectID: "p1", WorkflowID: "wf"}, 2, 2)
	if total2 != 3 {
		t.Fatalf("page2 total want 3, got %d", total2)
	}
	runIDs2 := uniqueRunIDs(page2)
	if len(runIDs2) != 1 || runIDs2[0] != "run-old" {
		t.Fatalf("page2 want [run-old], got %v", runIDs2)
	}
	// Cross-page integrity: run-new must not appear on page2.
	for _, a := range page2 {
		if a.RunID == "run-new" {
			t.Fatal("run-new must not be split onto page2")
		}
	}

	// Search hits research.json only → still returns whole run-new (plan.json too).
	hit, hitTotal := arts.AllPageByRun(ArtifactFilter{ProjectID: "p1", WorkflowID: "wf", Q: "research"}, 1, 20)
	if hitTotal != 1 {
		t.Fatalf("search total want 1 Run, got %d", hitTotal)
	}
	hitRuns := uniqueRunIDs(hit)
	if len(hitRuns) != 1 || hitRuns[0] != "run-new" {
		t.Fatalf("search want run-new, got %v", hitRuns)
	}
	if len(hit) != 2 {
		t.Fatalf("search whole-Run want 2 arts, got %d names=%v", len(hit), artifactNames(hit))
	}
	names := map[string]bool{}
	for _, a := range hit {
		names[a.Name] = true
		if a.RunTitle != "New Run" {
			t.Fatalf("search run_title want New Run, got %q", a.RunTitle)
		}
	}
	if !names["plan.json"] || !names["research.json"] {
		t.Fatalf("search whole-Run missing files: %v", names)
	}

	// Session filter: workflow-less artifacts only.
	session, sTotal := arts.AllPageByRun(ArtifactFilter{ProjectID: "p1", Session: true}, 1, 20)
	if sTotal != 1 || len(session) != 1 || session[0].Name != "loose.md" {
		t.Fatalf("session: total=%d arts=%v", sTotal, artifactNames(session))
	}

	// Project-wide: every run in p1.
	if _, pTotal := arts.AllPageByRun(ArtifactFilter{ProjectID: "p1"}, 1, 20); pTotal != 5 {
		t.Fatalf("project-wide want 5 Runs, got %d", pTotal)
	}

	// Empty search.
	empty, eTotal := arts.AllPageByRun(ArtifactFilter{ProjectID: "p1", WorkflowID: "wf", Q: "zzznomatch999"}, 1, 20)
	if eTotal != 0 || len(empty) != 0 {
		t.Fatalf("no match want 0, got total=%d n=%d", eTotal, len(empty))
	}
}

func uniqueRunIDs(arts []models.Artifact) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range arts {
		if !seen[a.RunID] {
			seen[a.RunID] = true
			out = append(out, a.RunID)
		}
	}
	return out
}

func artifactNames(arts []models.Artifact) []string {
	out := make([]string, len(arts))
	for i, a := range arts {
		out[i] = a.Name
	}
	return out
}
