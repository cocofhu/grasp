package services

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// artifactSQL preserves the existing content column/ref format without a
// serializer. Version snapshots copy that raw cell; no GET or PUT holds SQL.
type artifactSQL struct {
	ID, RunID, NodeID, WorkflowID, WorkflowName, Name, Kind, Content string
	SizeBytes, Revision                                              int
	MimeType, SHA256, SessionID                                      string
	PrivateFile                                                      bool
	CreatedAt, UpdatedAt                                             time.Time
}

func (artifactSQL) TableName() string { return "artifacts" }

func (row artifactSQL) logical() models.Artifact {
	return models.Artifact{ID: row.ID, RunID: row.RunID, NodeID: row.NodeID, WorkflowID: row.WorkflowID, WorkflowName: row.WorkflowName, Name: row.Name, Kind: row.Kind, SizeBytes: row.SizeBytes, Revision: row.Revision, MimeType: row.MimeType, SHA256: row.SHA256, SessionID: row.SessionID, PrivateFile: row.PrivateFile, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, PathRef: row.Content}
}

func archiveArtifactTx(tx *gorm.DB, row artifactSQL) error {
	return tx.Table("artifact_versions").Create(map[string]any{"artifact_id": row.ID, "run_id": row.RunID, "node_id": row.NodeID, "revision": max(row.Revision, 1), "kind": row.Kind, "size_bytes": row.SizeBytes, "content": row.Content, "mime_type": row.MimeType, "sha256": row.SHA256, "created_at": row.UpdatedAt}).Error
}

func (s *ArtifactService) SetExecutionController(c *executionlineage.Controller) { s.execution = c }

func (s *ArtifactService) executionEntities(ctx context.Context, runID string) ([]executionlineage.Entity, error) {
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityRun, ID: runID}}
	var run models.Run
	err := s.db.WithContext(ctx).Select("origin_solo_id").First(&run, "id = ?", runID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if run.OriginSoloID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
	}
	var resources []models.Sandbox
	if err := s.db.WithContext(ctx).Select("id", "purpose", "thread_id", "state_run_id").Where("run_id = ?", runID).Find(&resources).Error; err != nil {
		return nil, err
	}
	for _, row := range resources {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(row.ID), 10)})
		if row.Purpose == SandboxPurposeSolo {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: row.ThreadID}, executionlineage.Entity{Kind: executionlineage.EntityQueue, ID: soloQueueScope(row.ThreadID).ID()})
		}
		if row.StateRunID != 0 {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(row.StateRunID), 10)})
		}
	}
	return entities, nil
}

func (s *ArtifactService) executionTransaction(ctx context.Context, runID string, body func(*gorm.DB) error, extra ...executionlineage.Entity) error {
	if err := messageOutsideTransaction(s.db); err != nil {
		return err
	}
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	entities, err := s.executionEntities(ctx, runID)
	if err != nil {
		return err
	}
	return s.execution.Transaction(ctx, s.db, append(entities, extra...), body)
}

func (s *ArtifactService) savePreparedArtifact(ctx context.Context, runID, nodeID, name, kind, content string) (string, error) {
	if err := messageOutsideTransaction(s.db); err != nil {
		return "", err
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return "", err
	}
	prepared, err := files.PrepareBounded(ctx, strings.NewReader(content), filestore.Text, s.maxFileBytes)
	if err != nil {
		return "", err
	}
	var id string
	err = s.executionTransaction(ctx, runID, func(tx *gorm.DB) error {
		ref, err := files.PreparedRef(prepared)
		if err != nil {
			return err
		}
		if err := lockArtifactNamespace(tx, runID); err != nil {
			return err
		}
		var row artifactSQL
		find := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND name = ?", runID, name).Take(&row).Error
		now := time.Now()
		if find == nil {
			row.Revision = max(row.Revision, 1)
			if row.Content != ref && row.Content != content || row.Kind != kind {
				if err := archiveArtifactTx(tx, row); err != nil {
					return err
				}
				row.Revision++
			}
		} else if !errors.Is(find, gorm.ErrRecordNotFound) {
			return find
		} else {
			var run models.Run
			if err := tx.Select("workflow_id", "workflow_name").Where("id = ?", runID).Find(&run).Error; err != nil {
				return err
			}
			row = artifactSQL{ID: "art-" + uuid.NewString()[:8], RunID: runID, WorkflowID: run.WorkflowID, WorkflowName: run.WorkflowName, Name: name, Revision: 1, CreatedAt: now}
		}
		row.NodeID, row.Kind, row.Content, row.SizeBytes, row.UpdatedAt = nodeID, kind, ref, len(content), now
		row.SHA256, row.MimeType = "", ""
		id = row.ID
		return tx.Save(&row).Error
	})
	return id, err
}
