package workspacechanges

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func baselinePin(scope Scope, directory string) string {
	key := fmt.Sprintf("%s/%s/%d/%s", scope.Kind, scope.OwnerID, scope.Generation, directory)
	return fmt.Sprintf("refs/grasp/changes/%x", sha256.Sum256([]byte(key)))
}

func (s *Service) saveBaseline(ctx context.Context, scope Scope, row *models.WorkspaceChangeBaseline) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOwner(tx, scope.Kind, scope.OwnerID); err != nil {
			return err
		}
		res := s.scoped(tx.Model(&models.WorkspaceChangeBaseline{}), scope).Select("Trees", "ArchiveID").Updates(row)
		if res.Error != nil || row.ArchiveID == "" {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Delete(&models.WorkspaceChangeGarbage{ID: row.ArchiveID}).Error
	})
}

func (s *Service) archiveBaseline(ctx context.Context, sb treeReader, scope Scope, row *models.WorkspaceChangeBaseline) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	release, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	store, err := filestore.FromDB(s.db)
	if err != nil {
		return err
	}
	archiveID := uuid.NewString()
	archiveCtx, err := s.stageArchive(ctx, archiveID)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			s.abandonArchive(ctx, archiveID)
		}
	}()
	var failures []error
	for _, repo := range scope.Repos {
		dir := directory(repo)
		tree := row.Trees[dir]
		if tree.Ref == "" || tree.Pack != "" {
			continue
		}
		query := sandbox.WorkspaceTreeQuery{Directory: dir, From: tree.Ref}
		pack, err := sb.ExportWorkspaceTree(ctx, query)
		if err == nil {
			tree.Pack, _, err = store.PutBounded(archiveCtx, pack, filestore.Binary, s.maxPackBytes)
			_ = pack.Close()
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("archive baseline for %s: %w", dir, err))
			continue
		}
		row.Trees[dir] = tree
		row.ArchiveID = archiveID
	}
	if row.ArchiveID != "" {
		// Keep completed packs if a later repository exhausted the transfer budget.
		commitCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		if err := s.saveBaseline(commitCtx, scope, row); err != nil {
			failures = append(failures, err)
		} else {
			published = true
		}
	}
	return errors.Join(failures...)
}

func (s *Service) recoverBaseline(ctx context.Context, sb treeReader, scope Scope, before Trees, queries []sandbox.WorkspaceTreeQuery, raw []sandbox.WorkspaceTreeChanges) {
	var retry []sandbox.WorkspaceTreeQuery
	var indexes []int
	for i, query := range queries {
		if !raw[i].Unavailable || query.To == "" || before[query.Directory].Pack == "" {
			continue
		}
		if err := s.restoreBaseline(ctx, sb, scope, query, before[query.Directory].Pack); err != nil {
			s.report(scope, fmt.Errorf("restore baseline for %s: %w", query.Directory, err))
			continue
		}
		retry = append(retry, query)
		indexes = append(indexes, i)
	}
	if len(retry) == 0 {
		return
	}
	recovered, err := sb.CompareWorkspaceTrees(ctx, retry)
	if err != nil {
		s.report(scope, err)
		return
	}
	for i, index := range indexes {
		raw[index] = recovered[i]
	}
}

func (s *Service) restoreBaseline(ctx context.Context, sb treeReader, scope Scope, query sandbox.WorkspaceTreeQuery, ref string) error {
	store, err := filestore.FromDB(s.db)
	if err != nil {
		return err
	}
	pack, err := store.Open(ctx, ref)
	if err != nil {
		return err
	}
	defer func() { _ = pack.Close() }()
	query.Pin = baselinePin(scope, query.Directory)
	return sb.RestoreWorkspaceTree(ctx, query, pack)
}
