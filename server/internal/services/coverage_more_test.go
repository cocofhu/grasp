package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestCurrentNodeIDsAndFailedError(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	g := models.Graph{Nodes: []models.Node{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}
	db.Create(&models.Run{ID: "r1", Status: "running", Graph: g})
	db.Create(&models.Run{ID: "r2", Status: "waiting_human", Graph: g})
	db.Create(&models.Run{ID: "r3", Status: "failed", Graph: g})
	db.Create(&models.StateRun{RunID: "r1", NodeID: "a", Status: "running"})
	db.Create(&models.Gate{RunID: "r2", NodeID: "b", Title: "g"})
	db.Create(&models.StateRun{RunID: "r3", NodeID: "a", Status: "failed", Error: "boom"})

	ids := s.CurrentNodeIDs([]models.Run{
		{ID: "r1", Status: "running", Graph: g},
		{ID: "r2", Status: "waiting_human", Graph: g},
		{ID: "r3", Status: "failed", Graph: g},
	})
	if ids["r1"] != "a" || ids["r2"] != "b" {
		t.Fatalf("ids=%v", ids)
	}
	if _, ok := ids["r3"]; ok {
		t.Fatal("failed should omit")
	}
	if s.RunFailedError("r3") != "boom" {
		t.Fatalf("err=%q", s.RunFailedError("r3"))
	}
	if s.RunFailedError("missing") != "" {
		t.Fatal("missing")
	}
	if s.CurrentNodeIDs(nil) != nil {
		t.Fatal("empty")
	}
}

func TestCurrentNodeIDsMatchesCurrentNodes(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	handoff := models.Run{ID: "handoff", Status: "waiting_human", Graph: pendingAutoGraph()}
	db.Create(&handoff)
	db.Create(&models.StateRun{RunID: "handoff", NodeID: "clarify", Status: "waiting_human"})
	db.Create(&models.StateRun{RunID: "handoff", NodeID: "impl", Status: "running"})

	db.Create(&models.Run{ID: "entering", Status: "waiting_human", Graph: pendingAutoGraph()})
	wait := models.StateRun{RunID: "entering", NodeID: "clarify", Status: "waiting_human"}
	db.Create(&wait)
	entering := pendingAutoRun("entering", "waiting_human", wait.ID)

	runs := []models.Run{handoff, entering}
	if cn := s.CurrentNodes(runs)["entering"]; cn.State != CurrentStateEntering {
		t.Fatalf("entering current=%+v", cn)
	}
	ids := s.CurrentNodeIDs(runs)
	if ids["handoff"] != "impl" {
		t.Fatalf("newer running visit should be current, ids=%v", ids)
	}
	if _, ok := ids["entering"]; ok {
		t.Fatalf("entering successor has no visit, ids=%v", ids)
	}
}
