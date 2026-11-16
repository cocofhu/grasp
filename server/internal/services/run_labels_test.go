package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestGraphNodeLabel(t *testing.T) {
	g := models.Graph{Nodes: []models.Node{
		{ID: "a", Label: "  实现  "},
		{ID: "b", Label: "   "},
		{ID: "c"},
	}}
	if got := GraphNodeLabel(g, "a"); got != "实现" {
		t.Fatalf("trimmed label: got %q", got)
	}
	if got := GraphNodeLabel(g, "b"); got != "" {
		t.Fatalf("blank label: got %q", got)
	}
	if got := GraphNodeLabel(g, "missing"); got != "" {
		t.Fatalf("missing node: got %q", got)
	}
}

func TestCurrentNodeLabels(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)

	graphWithLabels := models.Graph{Nodes: []models.Node{
		{ID: "impl", Label: "实现"},
		{ID: "gate", Label: "方案评审门禁"},
		{ID: "react", Label: "澄清"},
		{ID: "blank", Label: "   "},
	}}
	graphBlankOnly := models.Graph{Nodes: []models.Node{{ID: "impl", Label: "   "}}}

	db.Create(&models.Run{ID: "run-running", Status: "running", Graph: graphWithLabels})
	db.Create(&models.Run{ID: "run-gate", Status: "waiting_human", Graph: graphWithLabels})
	db.Create(&models.Run{ID: "run-react", Status: "waiting_human", Graph: graphWithLabels})
	db.Create(&models.Run{ID: "run-blank", Status: "running", Graph: graphBlankOnly})
	db.Create(&models.Run{ID: "run-sandbox-fail", Status: "running", Graph: graphWithLabels})
	db.Create(&models.Run{ID: "run-done", Status: "completed", Graph: graphWithLabels})
	db.Create(&models.Run{ID: "run-queued", Status: "queued", Graph: graphWithLabels})

	db.Create(&models.StateRun{RunID: "run-running", NodeID: "impl", Iteration: 1, Status: "running"})
	db.Create(&models.Gate{RunID: "run-gate", NodeID: "gate", Title: "审批"})
	db.Create(&models.StateRun{RunID: "run-react", NodeID: "react", Iteration: 1, Status: "waiting_human"})
	db.Create(&models.StateRun{RunID: "run-blank", NodeID: "blank", Iteration: 1, Status: "running"})
	db.Create(&models.StateRun{RunID: "run-sandbox-fail", NodeID: "impl", Iteration: 1, Status: "failed",
		Error: "sandbox setup failed: docker pull failed"})

	runs := s.List(nil, "", "")
	labels := map[string]string{}
	for id, cn := range s.CurrentNodes(runs) {
		if cn.Name != "" {
			labels[id] = cn.Name
		}
	}

	if labels["run-running"] != "实现" {
		t.Fatalf("running label: got %q", labels["run-running"])
	}
	if labels["run-gate"] != "方案评审门禁" {
		t.Fatalf("gate label: got %q", labels["run-gate"])
	}
	if labels["run-react"] != "澄清" {
		t.Fatalf("react label: got %q", labels["run-react"])
	}
	if _, ok := labels["run-blank"]; ok {
		t.Fatalf("blank graph label should be omitted, got %q", labels["run-blank"])
	}
	if _, ok := labels["run-sandbox-fail"]; ok {
		t.Fatalf("sandbox-fail running should have no label, got %q", labels["run-sandbox-fail"])
	}
	if _, ok := labels["run-done"]; ok {
		t.Fatalf("completed should be omitted, got %q", labels["run-done"])
	}
	if _, ok := labels["run-queued"]; ok {
		t.Fatalf("queued should be omitted, got %q", labels["run-queued"])
	}
}
