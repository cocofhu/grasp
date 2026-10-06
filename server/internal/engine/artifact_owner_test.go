package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// seedArtifactOwner makes runID resolve to models.DefaultProjectID in
// ArtifactService.Save. A stored run gets a WorkflowDef (and a WorkflowID if
// it had none); a run id without a row gets a test Sandbox bound to it.
func seedArtifactOwner(t *testing.T, db *gorm.DB, runID string) {
	t.Helper()
	var run models.Run
	if err := db.Limit(1).Find(&run, "id = ?", runID).Error; err != nil {
		t.Fatalf("load run %s: %v", runID, err)
	}
	if run.ID == "" {
		if err := db.Create(&models.Sandbox{
			Name: "sb-" + runID, Purpose: "test", RunID: runID, ProjectID: models.DefaultProjectID,
		}).Error; err != nil {
			t.Fatalf("seed sandbox for %s: %v", runID, err)
		}
		return
	}
	if run.WorkflowID == "" {
		run.WorkflowID = "wf-" + runID
		if err := db.Model(&models.Run{}).Where("id = ?", runID).Update("workflow_id", run.WorkflowID).Error; err != nil {
			t.Fatalf("set workflow for %s: %v", runID, err)
		}
	}
	seedWorkflowForRun(t, db, run.WorkflowID)
}

// seedWorkflowForRun ensures a WorkflowDef with wfID exists in the default project.
func seedWorkflowForRun(t *testing.T, db *gorm.DB, wfID string) {
	t.Helper()
	wf := models.WorkflowDef{ID: wfID, ProjectID: models.DefaultProjectID, Name: wfID}
	if err := db.Where("id = ?", wfID).FirstOrCreate(&wf).Error; err != nil {
		t.Fatalf("seed workflow %s: %v", wfID, err)
	}
}
