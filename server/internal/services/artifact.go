// Package services holds query/persistence helpers that sit between the
// HTTP handlers and the database/engine.
package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Sentinel errors for ArtifactService.DeleteByID (mapped by HTTP handlers).
var (
	ErrArtifactNotFound       = errors.New("artifact not found")
	ErrArtifactRunNotTerminal = errors.New("cannot delete artifact: owning run has not ended")
)

const unnamedGroupKey = "__unnamed__"

func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// ArtifactService persists run products and implements mcp.Store so the
// artifact-store MCP writes land in the platform product store.
type ArtifactService struct{ db *gorm.DB }

// NewArtifactService builds the service.
func NewArtifactService(db *gorm.DB) *ArtifactService { return &ArtifactService{db: db} }

var _ mcp.Store = (*ArtifactService)(nil)

// Save persists (or replaces) an artifact within a run namespace.
func (s *ArtifactService) Save(runID, nodeID, name, kind, content string) (string, error) {
	var run models.Run
	s.db.Select("workflow_id", "workflow_name").First(&run, "id = ?", runID)

	now := time.Now()
	// Replace an existing same-named artifact within the run (idempotent writes).
	var existing models.Artifact
	if err := s.db.Where("run_id = ? AND name = ?", runID, name).First(&existing).Error; err == nil {
		existing.Content = content
		existing.Kind = kind
		existing.NodeID = nodeID
		existing.SizeBytes = len(content)
		existing.UpdatedAt = now
		if existing.Revision < 1 {
			existing.Revision = 1
		}
		existing.Revision++
		if err := s.db.Save(&existing).Error; err != nil {
			return "", err
		}
		return existing.ID, nil
	}

	a := models.Artifact{
		ID: "art-" + uuid.NewString()[:8], RunID: runID, NodeID: nodeID,
		WorkflowID: run.WorkflowID, WorkflowName: run.WorkflowName, Name: name, Kind: kind,
		SizeBytes: len(content), Content: content, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := s.db.Create(&a).Error; err != nil {
		return "", err
	}
	return a.ID, nil
}

// GetRecord returns the full artifact row for a run+name lookup.
func (s *ArtifactService) GetRecord(runID, name string) (models.Artifact, bool) {
	var a models.Artifact
	if err := s.db.Where("run_id = ? AND name = ?", runID, name).First(&a).Error; err != nil {
		return models.Artifact{}, false
	}
	return a, true
}

// Get returns an artifact's content within a run.
func (s *ArtifactService) Get(runID, name string) (string, bool) {
	a, ok := s.GetRecord(runID, name)
	if !ok {
		return "", false
	}
	return a.Content, true
}

// List returns the run's artifact metadata (scoped to the run only).
func (s *ArtifactService) List(runID string) []mcp.ArtifactInfo {
	var arts []models.Artifact
	s.db.Where("run_id = ?", runID).Find(&arts)
	out := make([]mcp.ArtifactInfo, 0, len(arts))
	for _, a := range arts {
		out = append(out, mcp.ArtifactInfo{Name: a.Name, Node: a.NodeID, Size: a.SizeBytes})
	}
	return out
}

// Delete removes a named artifact within a run (idempotent). Used to drop stale
// node_complete.json when ClearOutcome starts a new attempt/iteration.
func (s *ArtifactService) Delete(runID, name string) error {
	if runID == "" || name == "" {
		return nil
	}
	return s.db.Where("run_id = ? AND name = ?", runID, name).Delete(&models.Artifact{}).Error
}

var _ mcp.ArtifactDeleter = (*ArtifactService)(nil)

// ByRun returns artifact records for a run (for the API). Content is omitted.
func (s *ArtifactService) ByRun(runID string) []models.Artifact {
	var arts []models.Artifact
	s.db.Where("run_id = ?", runID).Omit("Content").Order("created_at").Find(&arts)
	return arts
}

// ByRunWithContent returns all artifacts for a run including Content (for pack/download).
func (s *ArtifactService) ByRunWithContent(runID string) []models.Artifact {
	var arts []models.Artifact
	s.db.Where("run_id = ?", runID).Order("created_at").Find(&arts)
	return arts
}

// DeleteForRuns removes artifacts belonging to ephemeral internal checks.
// Callers must supply server-minted run IDs rather than user input.
func (s *ArtifactService) DeleteForRuns(runIDs ...string) error {
	if len(runIDs) == 0 {
		return nil
	}
	return s.db.Delete(&models.Artifact{}, "run_id IN ?", runIDs).Error
}

func (s *ArtifactService) allQuery(wf, projectID, q string) *gorm.DB {
	query := s.db.Table("artifacts").
		Select(`artifacts.id, artifacts.run_id, artifacts.node_id, artifacts.workflow_id, artifacts.workflow_name,
			artifacts.name, artifacts.kind, artifacts.size_bytes, artifacts.created_at, artifacts.updated_at, artifacts.revision,
			runs.title as run_title`).
		Joins("LEFT JOIN runs ON runs.id = artifacts.run_id")
	if wf == unnamedGroupKey {
		query = query.Where("(artifacts.workflow_id IS NULL OR artifacts.workflow_id = '')")
	} else if wf != "" {
		query = query.Where("artifacts.workflow_id = ?", wf)
	} else if projectID != "" {
		query = query.Where("artifacts.workflow_id IN (?)", s.db.Model(&models.WorkflowDef{}).Select("id").Where("project_id = ?", projectID))
	}
	if q = strings.TrimSpace(q); q != "" {
		pattern := "%" + strings.ToLower(escapeLikePattern(q)) + "%"
		like := " LIKE ? ESCAPE '\\'"
		query = query.Where(
			"(LOWER(artifacts.name)"+like+" OR LOWER(artifacts.node_id)"+like+" OR LOWER(COALESCE(runs.title, ''))"+like+")",
			pattern, pattern, pattern,
		)
	}
	return query
}

// All returns every artifact (for the platform-wide artifacts view), including
// runTitle from a LEFT JOIN on runs (empty when the run row is missing).
func (s *ArtifactService) All() []models.Artifact {
	var arts []models.Artifact
	s.allQuery("", "", "").Order("artifacts.created_at desc").Scan(&arts)
	return arts
}

// AllPage returns a page of artifacts plus total count, optionally filtered by
// workflow, project, and q (wf wins over projectId when both set).
func (s *ArtifactService) AllPage(wf, projectID string, page, pageSize int, q string) ([]models.Artifact, int64) {
	base := s.allQuery(wf, projectID, q)
	var total int64
	base.Count(&total)
	var arts []models.Artifact
	offset := (page - 1) * pageSize
	base.Order("artifacts.created_at desc").Limit(pageSize).Offset(offset).Scan(&arts)
	return arts, total
}

// AllPageByRun pages by Run (not artifact row): total/pageSize are Run counts.
// Stage 1 aggregates DISTINCT run_id under allQuery filters (wf/project/q),
// ordered by MAX(created_at) desc. Stage 2 expands those Runs' full artifacts
// under wf/project only — search q is NOT reapplied so a hit returns the whole Run.
func (s *ArtifactService) AllPageByRun(wf, projectID string, page, pageSize int, q string) ([]models.Artifact, int64) {
	var total int64
	s.allQuery(wf, projectID, q).Distinct("artifacts.run_id").Count(&total)
	if total == 0 || pageSize <= 0 {
		return []models.Artifact{}, total
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	type runIDRow struct {
		RunID string `gorm:"column:run_id"`
	}
	var rows []runIDRow
	s.allQuery(wf, projectID, q).
		Select("artifacts.run_id as run_id, MAX(artifacts.created_at) as latest_at").
		Group("artifacts.run_id").
		Order("latest_at DESC").
		Limit(pageSize).
		Offset(offset).
		Scan(&rows)
	if len(rows) == 0 {
		return []models.Artifact{}, total
	}

	runIDs := make([]string, len(rows))
	for i, r := range rows {
		runIDs[i] = r.RunID
	}

	var arts []models.Artifact
	// Expand without q: whole-Run return for search hits (wf/project retained).
	s.allQuery(wf, projectID, "").
		Where("artifacts.run_id IN ?", runIDs).
		Order("artifacts.created_at DESC").
		Scan(&arts)
	return arts, total
}

// Get loads one artifact by id (for download).
func (s *ArtifactService) GetByID(id string) (models.Artifact, bool) {
	var a models.Artifact
	if err := s.db.First(&a, "id = ?", id).Error; err != nil {
		return models.Artifact{}, false
	}
	return a, true
}

// DeleteByID hard-deletes one artifact by id when its owning run is terminal
// (completed / failed / cancelled). Existence is checked first so missing ids
// map to 404; non-terminal runs map to 409.
func (s *ArtifactService) DeleteByID(id string) error {
	a, ok := s.GetByID(id)
	if !ok {
		return ErrArtifactNotFound
	}
	var run models.Run
	err := s.db.Select("status").First(&run, "id = ?", a.RunID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// Missing run (orphan artifact): no active execution to protect — allow delete.
	// Otherwise require the same terminal set as inbox.terminalRunStatuses
	// (completed / failed / cancelled). Do not use visual-prototype "succeeded".
	if err == nil && !containsString(terminalRunStatuses, run.Status) {
		return fmt.Errorf("%w (status %q; allowed: completed, failed, cancelled)", ErrArtifactRunNotTerminal, run.Status)
	}
	return s.db.Delete(&models.Artifact{}, "id = ?", id).Error
}
