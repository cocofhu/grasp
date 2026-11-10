package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestNodeRequestPinsWorkspaceToExactVisitAndRunAttempt(t *testing.T) {
	e, db := setupEngine(t)
	c := &execCtx{run: &models.Run{ID: "provenance", Attempt: 2}, iter: map[string]int{"work": 3}, vars: map[string]any{}}
	node := &models.Node{ID: "work", Type: "agent", Config: map[string]any{}}
	rows := []models.StateRun{
		{RunID: c.run.ID, NodeID: node.ID, Iteration: 3, Attempt: 1},
		{RunID: c.run.ID, NodeID: node.ID, Iteration: 3, Attempt: 2},
		{RunID: c.run.ID, NodeID: node.ID, Iteration: 4, Attempt: 2},
		{RunID: c.run.ID, NodeID: "another-node", Iteration: 3, Attempt: 2},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	request := e.nodeReq(c, node)
	if request.StateRunID != rows[1].ID || request.RunAttempt != 2 || request.Iteration != 3 || request.ProviderAttempt != 0 {
		t.Fatalf("request attributed to another execution: %+v", request)
	}
	// Missing or ambiguous source metadata stays unknown, even when a more
	// recent execution exists. It must never become an alleged file snapshot.
	c.run.Attempt = 9
	if got := e.nodeReq(c, node).StateRunID; got != 0 {
		t.Fatalf("missing attempt guessed visit %d", got)
	}
	c.run.Attempt = 2
	duplicate := models.StateRun{RunID: c.run.ID, NodeID: node.ID, Iteration: 3, Attempt: 2}
	if err := db.Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}
	if got := e.nodeReq(c, node).StateRunID; got != 0 {
		t.Fatalf("ambiguous execution guessed visit %d", got)
	}
}
