package engine

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestFileOnlyDialogueReplyRetainsItsTurnAndOutcome(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{db: db}
	file := models.ArtifactAttachment{ArtifactID: "report", Revision: 1, Kind: "pdf", Name: "report.pdf"}
	if err = db.Create(&models.ArtifactDelivery{ID: "delivered", RunID: "run", NodeID: "node", Iteration: 1, TurnID: "first", ArtifactAttachment: file}).Error; err != nil {
		t.Fatal(err)
	}
	st := DialogueState{RunID: "run", NodeID: "node", Iteration: 1, Rows: []DialogueRow{
		{ReactMessage: models.ReactMessage{Role: "agent", TurnID: "first"}, Phase: rowSealed},
		{ReactMessage: models.ReactMessage{Role: "agent", TurnID: "second"}, Phase: rowSealed},
	}, Turns: []DialogueTurn{{TurnID: "first", Phase: turnEnded, Outcome: models.OutcomeEmpty}, {TurnID: "second", Phase: turnEnded, Outcome: models.OutcomeEmpty}}}
	e.attachDialogueFiles(&st)
	e.attachDialogueFiles(&st)
	if len(st.Rows[0].Files) != 1 || st.Rows[0].Outcome != models.OutcomeOK || st.Turns[0].Outcome != models.OutcomeOK {
		t.Fatalf("file reply=%+v turn=%+v", st.Rows[0], st.Turns[0])
	}
	if len(st.Rows[1].Files) != 0 || st.Turns[1].Outcome != models.OutcomeEmpty {
		t.Fatal("file attached to another turn")
	}
	st.Iteration = 2
	st.Rows[0].Files = nil
	e.attachDialogueFiles(&st)
	if len(st.Rows[0].Files) != 0 {
		t.Fatal("file attached to another visit")
	}
}
