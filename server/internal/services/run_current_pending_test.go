package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func pendingAutoGraph() models.Graph {
	clarify := &models.AgentCapabilities{Interaction: models.InteractionClarify, Reads: []string{"*"}}
	return models.Graph{Nodes: []models.Node{
		{ID: "clarify", Type: "agent", Label: "需求澄清", Caps: clarify},
		{ID: "impl", Type: "agent", Label: "实现", Caps: testAutoCaps},
		{ID: "gate", Type: "human_gate", Label: "审批"},
	}}
}

func pendingAutoRun(id, status string, visit uint) models.Run {
	return models.Run{
		ID: id, WorkflowID: "wf", Status: status, Graph: pendingAutoGraph(),
		Checkpoints: map[string]map[string]any{
			PendingAutoCheckpoint: {"node": "impl", "from": "clarify", "visit": visit},
		},
	}
}

// The list reports the settled automatic successor while the clarify visit is
// still waiting and no newer running visit exists. A refresh reads this
// currentNode with no page memory.
func TestCurrentNodesReportsPendingAutoSuccessor(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	g := pendingAutoGraph()
	now := time.Now().UTC()
	db.Create(&models.Run{ID: "early", WorkflowID: "wf", Status: "waiting_human", Graph: g, StartedAt: now})
	wait := models.StateRun{RunID: "early", NodeID: "clarify", Iteration: 1, Status: "waiting_human"}
	db.Create(&wait)
	if wait.ID == 0 {
		t.Fatal("state id not assigned")
	}
	marked := pendingAutoRun("early", "waiting_human", wait.ID)
	// Round-trip the checkpoint through the database the list query uses.
	db.Model(&models.Run{}).Where("id = ?", "early").Select("Checkpoints").Updates(&models.Run{Checkpoints: marked.Checkpoints})
	var stored models.Run
	if err := db.First(&stored, "id = ?", "early").Error; err != nil {
		t.Fatal(err)
	}

	// Run status can stay "running" after the early Done write.
	runningStatus := stored
	runningStatus.Status = "running"
	db.Model(&models.Run{}).Where("id = ?", "early").Update("status", "running")
	var storedRunning models.Run
	db.First(&storedRunning, "id = ?", "early")

	for _, r := range []models.Run{stored, storedRunning} {
		cn, ok := s.CurrentNodes([]models.Run{r})[r.ID]
		if !ok || cn.ID != "impl" || cn.Name != "实现" || cn.Kind != CurrentKindAgent || cn.Mode != models.NodeModeAuto || cn.State != CurrentStateEntering {
			t.Fatalf("status %s: got %+v ok=%v", r.Status, cn, ok)
		}
	}
}

func TestCurrentNodesPendingAutoYieldsToRealState(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	g := pendingAutoGraph()
	mk := func(id, status string) {
		db.Create(&models.Run{ID: id, WorkflowID: "wf", Status: status, Graph: g, StartedAt: time.Now().UTC()})
	}
	mk("moved", "waiting_human")
	mk("failed-confirm", "waiting_human")
	mk("gate", "waiting_human")
	mk("review", "waiting_human")
	mk("reopened", "waiting_human")
	mk("done", "completed")
	mk("gap", "running")
	mk("branch", "waiting_human")

	clarifyWait := func(runID string) uint {
		t.Helper()
		sr := models.StateRun{RunID: runID, NodeID: "clarify", Iteration: 1, Status: "waiting_human"}
		db.Create(&sr)
		return sr.ID
	}
	stamp := func(id string, visit uint) {
		t.Helper()
		cp := map[string]map[string]any{PendingAutoCheckpoint: {"node": "impl", "from": "clarify", "visit": visit}}
		db.Model(&models.Run{}).Where("id = ?", id).Select("Checkpoints").Updates(&models.Run{Checkpoints: cp})
	}

	movedVisit := clarifyWait("moved")
	stamp("moved", movedVisit)
	db.Create(&models.StateRun{RunID: "moved", NodeID: "impl", Iteration: 1, Status: "running"})

	// Confirm failed: the mark is gone, the clarify visit is still waiting.
	clarifyWait("failed-confirm")

	gateVisit := clarifyWait("gate")
	stamp("gate", gateVisit)
	db.Create(&models.Gate{RunID: "gate", NodeID: "gate", Iteration: 1, Title: "审批"})

	// Successor parked for a person. The mark must not keep it automatic.
	db.Create(&models.StateRun{RunID: "review", NodeID: "impl", Iteration: 1, Status: "waiting_human"})
	var reviewWait models.StateRun
	db.Where("run_id = ? AND node_id = ?", "review", "impl").First(&reviewWait)
	stamp("review", reviewWait.ID) // visit id is the successor, not the departed clarify

	// A later clarify visit does not inherit the previous confirm's mark.
	old := models.StateRun{RunID: "reopened", NodeID: "clarify", Iteration: 1, Status: "completed"}
	db.Create(&old)
	stamp("reopened", old.ID)
	db.Create(&models.StateRun{RunID: "reopened", NodeID: "clarify", Iteration: 2, Status: "waiting_human"})

	stamp("done", 1)

	// Human visit closed, successor not started: the mark still applies.
	closed := models.StateRun{RunID: "gap", NodeID: "clarify", Iteration: 1, Status: "completed"}
	db.Create(&closed)
	stamp("gap", closed.ID)

	// Two possible targets were not marked.
	clarifyWait("branch")

	var runs []models.Run
	db.Where("id IN ?", []string{"moved", "failed-confirm", "gate", "review", "reopened", "done", "gap", "branch"}).Find(&runs)
	got := s.CurrentNodes(runs)
	want := map[string][4]string{
		"moved":          {"impl", CurrentKindAgent, models.NodeModeAuto, "running"},
		"failed-confirm": {"clarify", CurrentKindClarify, models.NodeModeManual, "waiting_human"},
		"gate":           {"gate", CurrentKindGate, models.NodeModeManual, "waiting_human"},
		"review":         {"impl", CurrentKindReview, models.NodeModeManual, "waiting_human"},
		"reopened":       {"clarify", CurrentKindClarify, models.NodeModeManual, "waiting_human"},
		"gap":            {"impl", CurrentKindAgent, models.NodeModeAuto, CurrentStateEntering},
		"branch":         {"clarify", CurrentKindClarify, models.NodeModeManual, "waiting_human"},
	}
	for id, w := range want {
		cn, ok := got[id]
		if !ok || cn.ID != w[0] || cn.Kind != w[1] || cn.Mode != w[2] || cn.State != w[3] {
			t.Fatalf("%s: got %+v ok=%v, want %v", id, cn, ok, w)
		}
	}
	if _, ok := got["done"]; ok {
		t.Fatalf("ended run must not stay current: %+v", got["done"])
	}
}

// A confirm can mark its conversation done before the opening driver changes
// the run to waiting_human. Without a settled successor, the waiting visit is
// still current even though the run itself remains running.
func TestCurrentNodesRetainsWaitingVisitWhileRunIsRunning(t *testing.T) {
	cases := []struct {
		name        string
		node        string
		mode        string
		runStatus   string
		staleMark   bool
		runningNode string
		want        CurrentNode
	}{
		{
			name: "clarify", node: "clarify",
			want: CurrentNode{ID: "clarify", Name: "需求澄清", Iteration: 2, Kind: CurrentKindClarify, Mode: models.NodeModeManual, State: "waiting_human"},
		},
		{
			name: "review", node: "impl",
			want: CurrentNode{ID: "impl", Name: "实现", Iteration: 2, Kind: CurrentKindReview, Mode: models.NodeModeManual, State: "waiting_human"},
		},
		{
			name: "takeover", node: "impl", mode: models.NodeModeManual,
			want: CurrentNode{ID: "impl", Name: "实现", Iteration: 2, Kind: CurrentKindTakeover, Mode: models.NodeModeManual, State: "waiting_human"},
		},
		{
			name: "gate", node: "gate",
			want: CurrentNode{ID: "gate", Name: "审批", Iteration: 2, Kind: CurrentKindGate, Mode: models.NodeModeManual, State: "waiting_human"},
		},
		{
			name: "reopened visit ignores old pending successor", node: "clarify", staleMark: true,
			want: CurrentNode{ID: "clarify", Name: "需求澄清", Iteration: 2, Kind: CurrentKindClarify, Mode: models.NodeModeManual, State: "waiting_human"},
		},
		{
			name: "running successor takes precedence", node: "clarify", runningNode: "impl",
			want: CurrentNode{ID: "impl", Name: "实现", Iteration: 3, Kind: CurrentKindAgent, Mode: models.NodeModeAuto, State: "running"},
		},
		{name: "completed run", node: "clarify", runStatus: "completed"},
		{name: "failed run", node: "clarify", runStatus: "failed"},
		{name: "cancelled run", node: "clarify", runStatus: "cancelled"},
		{name: "no active visit"},
	}
	db := newTestDB(t)
	s := NewRunService(db)
	var runs []models.Run
	for _, tc := range cases {
		status := tc.runStatus
		if status == "" {
			status = "running"
		}
		run := models.Run{ID: tc.name, WorkflowID: "wf", Status: status, Graph: pendingAutoGraph()}
		if tc.staleMark {
			old := models.StateRun{RunID: run.ID, NodeID: "clarify", Iteration: 1, Status: "completed"}
			if err := db.Create(&old).Error; err != nil {
				t.Fatal(err)
			}
			run.Checkpoints = pendingAutoRun(run.ID, status, old.ID).Checkpoints
		}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if tc.node != "" {
			visit := models.StateRun{RunID: run.ID, NodeID: tc.node, Iteration: 2, Status: "waiting_human", Mode: tc.mode}
			if err := db.Create(&visit).Error; err != nil {
				t.Fatal(err)
			}
		}
		if tc.runningNode != "" {
			visit := models.StateRun{RunID: run.ID, NodeID: tc.runningNode, Iteration: 3, Status: "running"}
			if err := db.Create(&visit).Error; err != nil {
				t.Fatal(err)
			}
		}
		runs = append(runs, run)
	}
	got := s.CurrentNodes(runs)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cn, ok := got[tc.name]
			if tc.want.ID == "" {
				if ok {
					t.Fatalf("no current node expected, got %+v", cn)
				}
				return
			}
			if !ok || cn != tc.want {
				t.Fatalf("got %+v ok=%v, want %+v", cn, ok, tc.want)
			}
		})
	}
}
