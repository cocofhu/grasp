package workspacechanges

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

var ErrUnavailable = errors.New("workspace changes unavailable")

type Workspace struct {
	ID         string    `json:"id"`
	NodeID     string    `json:"nodeId,omitempty"`
	CapturedAt time.Time `json:"capturedAt"`
}

type View struct {
	Status     string                          `json:"status"`
	Snapshot   *models.WorkspaceChangeSnapshot `json:"snapshot,omitempty"`
	Workspaces []Workspace                     `json:"workspaces,omitempty"`
}

type File struct {
	Patch  *string `json:"patch,omitempty"`
	Before *string `json:"before,omitempty"`
	After  *string `json:"after,omitempty"`
}

func (s *Service) List(ctx context.Context, scope Scope, workspace, turnID string, turn bool) (View, error) {
	if len(scope.Repos) == 0 {
		return View{Status: "no_repositories"}, nil
	}
	view := View{Status: "unavailable"}
	var err error
	if !turn {
		view.Workspaces, err = s.workspaces(ctx, scope)
		if err != nil {
			return view, err
		}
		// A node selection only picks a default worktree. The summary then
		// tracks that worktree, even when another node shares it.
		for _, choice := range view.Workspaces {
			if workspace == "" && choice.NodeID == scope.NodeID {
				workspace = choice.ID
			}
		}
		scope.NodeID = ""
		scope.Iteration = 0
	}
	query := s.comparisons(s.db.WithContext(ctx), scope, workspace, turn, scope.NodeID, scope.Iteration)
	if turnID != "" {
		query = query.Where("turn_id = ?", turnID)
	}
	snapshot, err := selectLatest(query)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		view.Status = "ready"
		view.Snapshot = snapshot
		return view, err
	}
	if turn {
		return view, nil
	}
	var baseline models.WorkspaceChangeBaseline
	err = s.scoped(s.db.WithContext(ctx), scope).Take(&baseline).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return view, err
	}
	if len(baseline.Trees) > 0 {
		view.Status = "pending"
	}
	return view, nil
}

func (s *Service) File(ctx context.Context, kind, ownerID, snapshotID, fileID string) (File, error) {
	var snapshot models.WorkspaceChangeSnapshot
	err := s.db.WithContext(ctx).Take(&snapshot, "id = ? AND owner_kind = ? AND owner_id = ?", snapshotID, kind, ownerID).Error
	if err != nil {
		return File{}, err
	}
	for _, repo := range snapshot.Repos {
		for _, path := range repo.Files {
			if path.ID == fileID {
				return s.file(ctx, snapshotID, path)
			}
		}
	}
	return File{}, gorm.ErrRecordNotFound
}

func (s *Service) file(ctx context.Context, id string, path models.WorkspaceChangePath) (File, error) {
	if !path.HasPatch && !path.HasBefore && !path.HasAfter {
		return File{}, nil
	}
	var content models.WorkspaceChangeContent
	if err := s.db.WithContext(ctx).Take(&content, "snapshot_id = ? AND file_id = ?", id, path.ID).Error; err != nil {
		return File{}, err
	}
	file := File{}
	if path.HasPatch {
		value := string(content.Patch)
		file.Patch = &value
	}
	if path.HasBefore {
		value := string(content.Before)
		file.Before = &value
	}
	if path.HasAfter {
		value := string(content.After)
		file.After = &value
	}
	return file, nil
}

func (s *Service) workspaces(ctx context.Context, scope Scope) ([]Workspace, error) {
	var entries []Workspace
	err := s.scoped(s.db.WithContext(ctx).Model(&models.WorkspaceChangeSnapshot{}), scope).
		Where("turn_id = ''").Select("workspace AS id, node_id, captured_at").
		Order("captured_at DESC, id DESC").Scan(&entries).Error
	seen := make(map[string]bool)
	out := make([]Workspace, 0)
	for _, entry := range entries {
		if !seen[entry.ID] {
			out = append(out, entry)
			seen[entry.ID] = true
		}
	}
	return out, err
}

func (s *Service) comparisons(db *gorm.DB, scope Scope, workspace string, turn bool, nodeID string, iteration int) *gorm.DB {
	query := s.scoped(db.Model(&models.WorkspaceChangeSnapshot{}), scope)
	if workspace != "" {
		query = query.Where("workspace = ?", workspace)
	}
	if turn {
		query = query.Where("turn_id <> '' AND node_id = ? AND iteration = ?", nodeID, iteration)
	} else {
		query = query.Where("turn_id = ''")
	}
	return query
}

func (s *Service) latest(ctx context.Context, scope Scope, workspace string, turn bool, nodeID string, iteration int) (*models.WorkspaceChangeSnapshot, error) {
	return selectLatest(s.comparisons(s.db.WithContext(ctx), scope, workspace, turn, nodeID, iteration))
}

func selectLatest(query *gorm.DB) (*models.WorkspaceChangeSnapshot, error) {
	var snapshot models.WorkspaceChangeSnapshot
	if err := query.Order("captured_at DESC, id DESC").Take(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s *Service) prune(tx *gorm.DB, scope Scope, turn bool) error {
	workspace := scope.Workspace
	if turn {
		workspace = "" // the dialogue follows the turn across sandbox recovery
	}
	var ids []string
	// Keep the immediately previous review readable during a refresh in
	// another tab. Everything older in this scope can be reclaimed.
	err := s.comparisons(tx, scope, workspace, turn, scope.NodeID, scope.Iteration).
		Order("captured_at DESC, id DESC").Pluck("id", &ids).Error
	if err != nil || len(ids) <= 2 {
		return err
	}
	ids = ids[2:]
	if err := tx.Where("snapshot_id IN ?", ids).Delete(&models.WorkspaceChangeContent{}).Error; err != nil {
		return err
	}
	return tx.Where("id IN ?", ids).Delete(&models.WorkspaceChangeSnapshot{}).Error
}

// DeleteOwner is called by the owner's delete transaction so no archive is
// left independently addressable.
func DeleteOwner(tx *gorm.DB, kind, id string) error {
	snapshots := tx.Model(&models.WorkspaceChangeSnapshot{}).Select("id").
		Where("owner_kind = ? AND owner_id = ?", kind, id)
	if err := tx.Where("snapshot_id IN (?)", snapshots).Delete(&models.WorkspaceChangeContent{}).Error; err != nil {
		return err
	}
	if err := tx.Where("owner_kind = ? AND owner_id = ?", kind, id).Delete(&models.WorkspaceChangeSnapshot{}).Error; err != nil {
		return err
	}
	return tx.Where("owner_kind = ? AND owner_id = ?", kind, id).Delete(&models.WorkspaceChangeBaseline{}).Error
}
