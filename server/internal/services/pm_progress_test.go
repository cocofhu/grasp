package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

func setupPmProgressFixtures(t *testing.T) (*PmProgress, *gorm.DB, string, string) {
	t.Helper()
	db := setupPmDB(t)
	ps := NewProjectService(db)
	p, err := ps.Create("ProgProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wfSvc := NewWorkflowService(db)
	wf := &models.WorkflowDef{ID: "wf-prog", ProjectID: p.ID, Name: "Pipeline", Graph: validGraph()}
	if err := wfSvc.Save(wf); err != nil {
		t.Fatal(err)
	}
	pm := NewPmService(db, nil)
	runs := NewRunService(db)
	arts := NewArtifactService(db)
	now := time.Now()
	db.Create(&models.Run{
		ID: "run-a", WorkflowID: wf.ID, WorkflowName: wf.Name, Status: "completed",
		Progress: 1, Title: "Run A", StartedAt: now.Add(-time.Hour), DurationSec: 60,
	})
	db.Create(&models.Run{
		ID: "run-b", WorkflowID: wf.ID, WorkflowName: wf.Name, Status: "waiting_human",
		Progress: 0.5, Title: "Run B", StartedAt: now.Add(-30 * time.Minute),
	})
	db.Create(&models.Run{
		ID: "run-f", WorkflowID: wf.ID, WorkflowName: wf.Name, Status: "failed",
		Progress: 0.2, Title: "Run F", StartedAt: now.Add(-2 * time.Hour),
	})
	arts.Save("run-a", "", "plan.json", "json", `{"goals":[{"status":"done","subgoals":[{"status":"pending"}]}]}`)
	arts.Save("run-a", "", "notes.md", "markdown", "# hello progress")
	return NewPmProgress(pm, runs, arts), db, p.ID, wf.ID
}

func TestPmProgressOverallEmptyAndUnavailable(t *testing.T) {
	p, _, pid, _ := setupPmProgressFixtures(t)
	if got := p.OverallProgress("no-project"); got["empty"] != true {
		t.Fatalf("unknown project: %+v", got)
	}
	nilRuns := NewPmProgress(nil, nil, nil)
	if got := nilRuns.OverallProgress(pid); got["empty"] != true {
		t.Fatalf("nil runs: %+v", got)
	}
}

func TestPmProgressOverallProgress(t *testing.T) {
	p, _, pid, _ := setupPmProgressFixtures(t)
	got := p.OverallProgress(pid)
	if got["empty"] != false || got["totalRuns"] != 3 {
		t.Fatalf("overall: %+v", got)
	}
	byStatus, ok := got["byStatus"].(map[string]int)
	if !ok || byStatus["completed"] != 1 || byStatus["waiting_human"] != 1 || byStatus["failed"] != 1 {
		t.Fatalf("byStatus: %+v", byStatus)
	}
	latest, ok := got["latestRun"].(map[string]any)
	if !ok || latest["id"] != "run-b" {
		t.Fatalf("latestRun: %+v", latest)
	}
	wfs, ok := got["workflows"].([]map[string]any)
	if !ok || len(wfs) != 1 || wfs[0]["runCount"] != 3 {
		t.Fatalf("workflows: %+v", wfs)
	}
}

func TestPmProgressListBlockers(t *testing.T) {
	p, db, pid, wfID := setupPmProgressFixtures(t)
	db.Create(&models.Gate{RunID: "run-b", NodeID: "gate-1", WorkflowID: wfID, Resolved: false, RequestedAt: time.Now()})
	got := p.ListBlockers(pid)
	if got["empty"] != false {
		t.Fatalf("blockers empty: %+v", got)
	}
	blockers, ok := got["blockers"].([]map[string]any)
	if !ok || len(blockers) < 1 {
		t.Fatalf("blockers: %+v", got)
	}
	// Empty project with no waiting runs.
	other, _ := NewProjectService(db).Create("Empty", "", nil)
	got2 := p.ListBlockers(other.ID)
	if got2["empty"] != true {
		t.Fatalf("no blockers: %+v", got2)
	}
	_ = wfID
}

func TestPmProgressPlanSummary(t *testing.T) {
	p, _, pid, _ := setupPmProgressFixtures(t)
	if got := p.PlanSummary(pid, ""); got["empty"] != true {
		t.Fatalf("missing runId: %+v", got)
	}
	if got := p.PlanSummary(pid, "ghost-run"); got["empty"] != true {
		t.Fatalf("bad run: %+v", got)
	}
	got := p.PlanSummary(pid, "run-a")
	if got["empty"] != false || got["runId"] != "run-a" {
		t.Fatalf("plan: %+v", got)
	}
	counts, ok := got["statusCounts"].(map[string]int)
	if !ok || counts["done"] != 1 || counts["pending"] != 1 {
		t.Fatalf("statusCounts: %+v", counts)
	}
	if got := p.PlanSummary(pid, "run-b"); got["empty"] != true {
		t.Fatalf("no plan.json: %+v", got)
	}
}

func TestPmProgressPlanSummaryInvalidJSON(t *testing.T) {
	p, db, pid, _ := setupPmProgressFixtures(t)
	arts := NewArtifactService(db)
	arts.Save("run-f", "", "plan.json", "json", "not-json{{{")
	got := p.PlanSummary(pid, "run-f")
	if got["empty"] != false || got["rawPreview"] == nil {
		t.Fatalf("invalid json preview: %+v", got)
	}
}

func TestPmProgressArtifactSummary(t *testing.T) {
	p, _, pid, _ := setupPmProgressFixtures(t)
	got := p.ArtifactSummary(pid, "run-a", 5)
	if got["empty"] != false {
		t.Fatalf("artifacts: %+v", got)
	}
	if got := p.ArtifactSummary(pid, "", 1); got["empty"] != false {
		t.Fatalf("project page: %+v", got)
	}
	if got := p.ArtifactSummary(pid, "ghost", 5); got["empty"] != true {
		t.Fatalf("bad run artifacts: %+v", got)
	}
	nilArts := NewPmProgress(nil, NewRunService(setupPmDB(t)), nil)
	if got := nilArts.ArtifactSummary(pid, "run-a", 5); got["empty"] != true {
		t.Fatalf("nil arts: %+v", got)
	}
}

func TestPmProgressRiskTrends(t *testing.T) {
	p, db, pid, _ := setupPmProgressFixtures(t)
	got := p.RiskTrends(pid)
	if got["empty"] != false || got["failed"] != 1 || got["waitingHuman"] != 1 {
		t.Fatalf("risk: %+v", got)
	}
	signals, ok := got["signals"].([]string)
	if !ok || len(signals) == 0 {
		t.Fatalf("signals: %+v", signals)
	}
	other, _ := NewProjectService(db).Create("RiskEmpty", "", nil)
	if got := p.RiskTrends(other.ID); got["empty"] != true {
		t.Fatalf("empty risk: %+v", got)
	}
}

func TestPmProgressCompareRuns(t *testing.T) {
	p, _, pid, wfID := setupPmProgressFixtures(t)
	if got := p.CompareRuns(pid, "", 3); got["empty"] != true {
		t.Fatalf("missing wf: %+v", got)
	}
	got := p.CompareRuns(pid, wfID, 2)
	if got["empty"] != false {
		t.Fatalf("compare: %+v", got)
	}
	rows, ok := got["runs"].([]map[string]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("runs rows: %+v", rows)
	}
	diffs, ok := got["diffHighlights"].([]string)
	if !ok || len(diffs) == 0 {
		t.Fatalf("diffs: %+v", diffs)
	}
	if got := p.CompareRuns(pid, "wf-missing", 3); got["empty"] != true {
		t.Fatalf("missing wf runs: %+v", got)
	}
}

func TestPmProgressHelpers(t *testing.T) {
	counts := summarizePlanStatuses(map[string]any{
		"goals": []any{
			map[string]any{"status": "done", "subgoals": []any{map[string]any{"status": "in_progress"}}},
			map[string]any{"status": "weird"},
		},
	})
	if counts["done"] != 1 || counts["in_progress"] != 1 || counts["other"] != 1 {
		t.Fatalf("counts: %+v", counts)
	}
	if truncateStr("abc", 10) != "abc" || truncateStr("abcdefghij", 5) != "abcde…" {
		t.Fatal("truncate")
	}
	if formatDuration(30*time.Second) == "" || formatDuration(2*time.Hour) == "" || formatDuration(3*24*time.Hour) == "" {
		t.Fatal("formatDuration")
	}
	if strAny(nil) != "" || strAny(42) != "42" {
		t.Fatal("strAny")
	}
}

func TestPmProgressNilServices(t *testing.T) {
	nilP := NewPmProgress(nil, nil, nil)
	if got := nilP.ListBlockers("x"); got["empty"] != true {
		t.Fatalf("nil list blockers: %+v", got)
	}
	if got := nilP.PlanSummary("p", "r"); got["empty"] != true {
		t.Fatalf("nil arts plan: %+v", got)
	}
	if got := nilP.ArtifactSummary("p", "", 10); got["empty"] != true {
		t.Fatalf("nil arts summary: %+v", got)
	}
	if got := nilP.RiskTrends("x"); got["empty"] != true {
		t.Fatalf("nil risk: %+v", got)
	}
	if got := nilP.CompareRuns("x", "", 5); got["empty"] != true {
		t.Fatalf("empty wf: %+v", got)
	}
	if got := nilP.CompareRuns("x", "wf", 5); got["empty"] != true {
		t.Fatalf("nil compare: %+v", got)
	}
}

func TestPmProgressCompareRunsVersionDiff(t *testing.T) {
	p, db, pid, wfID := setupPmProgressFixtures(t)
	now := time.Now()
	db.Create(&models.Run{
		ID: "run-v2", WorkflowID: wfID, WorkflowName: "Pipeline", Status: "completed",
		Progress: 1, Title: "V2", StartedAt: now, WorkflowVersion: 2,
	})
	db.Create(&models.Run{
		ID: "run-v1", WorkflowID: wfID, WorkflowName: "Pipeline", Status: "failed",
		Progress: 0.5, Title: "V1", StartedAt: now.Add(-time.Hour), WorkflowVersion: 1,
	})
	got := p.CompareRuns(pid, wfID, 5)
	diffs, ok := got["diffHighlights"].([]string)
	if !ok || len(diffs) == 0 {
		t.Fatalf("diffs: %+v", got)
	}
}

func TestPmProgressListBlockersGateAndClarify(t *testing.T) {
	p, db, pid, wfID := setupPmProgressFixtures(t)
	now := time.Now()
	db.Create(&models.Gate{
		RunID: "run-b", NodeID: "gate-1", WorkflowID: wfID, WorkflowName: "Pipeline",
		Title: "方案评审", Resolved: false, RequestedAt: now.Add(-10 * time.Minute),
	})
	db.Create(&models.Run{
		ID: "run-clarify", WorkflowID: wfID, WorkflowName: "Pipeline",
		Title: "澄清", Status: "waiting_human", StartedAt: now, Graph: reactGraph(),
	})
	db.Create(&models.ReactConversation{
		RunID: "run-clarify", NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "hi", At: now.Format(time.RFC3339)}},
	})
	got := p.ListBlockers(pid)
	blockers, ok := got["blockers"].([]map[string]any)
	if !ok || len(blockers) < 2 {
		t.Fatalf("blockers: %+v", got)
	}
}
