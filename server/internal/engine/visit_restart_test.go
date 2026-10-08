package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

// TestIdenticalRewriteStillFreshAfterRestart: saving the same conclusion body
// does not bump Artifact.Revision. After the process loses its in-memory
// visit, the name recorded on the execution row still counts as this visit's
// write. Revision alone does not.
func TestIdenticalRewriteStillFreshAfterRestart(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "restart-visit"
	if err := db.Create(&models.Run{
		ID: runID, WorkflowID: "clarify-to-design", WorkflowName: "clarify-to-design", Status: "running",
	}).Error; err != nil {
		t.Fatal(err)
	}
	body := `{"summary":"same","findings":[]}`
	if _, err := eng.store.Save(runID, "n", mcp.ResearchArtifactName, "json", body); err != nil {
		t.Fatal(err)
	}
	base := eng.host.BeginArtifactVisit(runID, "n")
	if err := db.Create(&models.StateRun{
		RunID: runID, NodeID: "n", Iteration: 2, Status: "running",
		ArtifactBaseRev: base, ArtifactBaseSet: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	tok := eng.host.RegisterRun(runID)
	t.Cleanup(func() { eng.host.UnregisterRun(runID) })
	if _, err := eng.host.WriteArtifact(runID, tok, "n", mcp.ResearchArtifactName, body, "json"); err != nil {
		t.Fatal(err)
	}

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, "n", 2).First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range sr.ArtifactVisitWrites {
		if name == mcp.ResearchArtifactName {
			found = true
		}
	}
	if !found {
		t.Fatalf("execution row must remember the identical rewrite, writes=%v", sr.ArtifactVisitWrites)
	}
	stamps := eng.store.(mcp.StampStore).Stamps(runID)
	rev := 0
	for _, st := range stamps {
		if st.Name == mcp.ResearchArtifactName {
			rev = st.Revision
		}
	}
	if rev != base[mcp.ResearchArtifactName] {
		t.Fatalf("identical content must not bump revision: base=%d live=%d", base[mcp.ResearchArtifactName], rev)
	}

	restarted := mcp.NewHost(eng.store)
	restarted.RestoreArtifactVisit(runID, "n", sr.ArtifactBaseRev, sr.ArtifactVisitWrites)
	if !restarted.FreshThisVisit(runID, "n", mcp.ResearchArtifactName) {
		t.Fatal("identical rewrite must still count after restart")
	}
	bare := mcp.NewHost(eng.store)
	bare.RestoreArtifactVisit(runID, "n", sr.ArtifactBaseRev, nil)
	if bare.FreshThisVisit(runID, "n", mcp.ResearchArtifactName) {
		t.Fatal("revision alone must not treat identical content as a new write")
	}
}
