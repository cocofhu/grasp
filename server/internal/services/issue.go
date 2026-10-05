package services

import (
	"context"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// IssueService persists human-reported preview issues. Humans submit problems
// from the preview UI via REST; the engine snapshots them into a run
// variable (preview_issues) at gate resume so a downstream node can consume
// them via {{vars.preview_issues}}.
type IssueService struct {
	db    *gorm.DB
	blobs blob.Store
}

// NewIssueService builds the service.
func NewIssueService(db *gorm.DB) *IssueService { return &IssueService{db: db} }

// SetBlobStore wires attachment externalization for preview issue images.
func (s *IssueService) SetBlobStore(store blob.Store) { s.blobs = store }

// Create persists a new open preview issue, attributing it to the run's
// workflow (mirrors ArtifactService.Save's run lookup).
func (s *IssueService) Create(runID, nodeID, body, selector string, port int, images []models.PromptImage) (models.PreviewIssue, error) {
	var run models.Run
	s.db.Select("workflow_id", "workflow_name").First(&run, "id = ?", runID)

	var err error
	images, err = blob.IngestPromptImages(context.Background(), s.blobs, images)
	if err != nil {
		return models.PreviewIssue{}, fmt.Errorf("ingest attachments: %w", err)
	}

	issue := models.PreviewIssue{
		ID: "iss-" + uuid.NewString()[:8], RunID: runID, NodeID: nodeID,
		WorkflowID: run.WorkflowID, WorkflowName: run.WorkflowName,
		Body: body, Selector: selector, Port: port, Images: images,
		Status: "open", CreatedAt: time.Now(),
	}
	if err := s.db.Create(&issue).Error; err != nil {
		return models.PreviewIssue{}, err
	}
	return issue, nil
}

// ListByRunNode returns issues for a single run/node (oldest first) for the UI.
func (s *IssueService) ListByRunNode(runID, nodeID string) []models.PreviewIssue {
	var rows []models.PreviewIssue
	s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).Order("created_at asc").Find(&rows)
	return rows
}

// Delete hard-removes an issue within a run namespace (a user deletes their own
// reported problem from the UI).
func (s *IssueService) Delete(runID, nodeID, id string) error {
	return s.db.Where("run_id = ? AND node_id = ? AND id = ?", runID, nodeID, id).
		Delete(&models.PreviewIssue{}).Error
}

// MarkResolvedByNode bulk-updates open PreviewIssues for this run+node to
// resolved. Scope is strictly this node; other nodes and already-resolved rows
// are left untouched. Records are retained for List/chat history.
func (s *IssueService) MarkResolvedByNode(runID, nodeID string) error {
	return s.db.Model(&models.PreviewIssue{}).
		Where("run_id = ? AND node_id = ? AND status = ?", runID, nodeID, "open").
		Update("status", "resolved").Error
}
