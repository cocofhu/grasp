// Package workspacechanges keeps a read-only view of Solo and run workspaces.
package workspacechanges

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Scope struct {
	Kind, OwnerID, Workspace, NodeID string
	Generation, Iteration            int
	Repos                            []models.SessionRepo
}

type Service struct {
	db           *gorm.DB
	slots        chan struct{}
	locks        [64]sync.Mutex
	maxPackBytes int64
}

// New shares a small capture budget across interactive and automatic work.
func New(db *gorm.DB, maxPackBytes int64) *Service {
	if maxPackBytes <= 0 {
		maxPackBytes = 128 << 20
	}
	return &Service{db: db, slots: make(chan struct{}, 4), maxPackBytes: maxPackBytes}
}

type Trees map[string]models.WorkspaceChangeTree

type treeReader interface {
	CaptureWorkspaceTrees(context.Context, []sandbox.WorkspaceTreeQuery) ([]sandbox.WorkspaceTree, error)
	CompareWorkspaceTrees(context.Context, []sandbox.WorkspaceTreeQuery) ([]sandbox.WorkspaceTreeChanges, error)
	ExportWorkspaceTree(context.Context, sandbox.WorkspaceTreeQuery) (io.ReadCloser, error)
	RestoreWorkspaceTree(context.Context, sandbox.WorkspaceTreeQuery, io.Reader) error
}

func directory(repo models.SessionRepo) string {
	if repo.RepoDirName != "" {
		return repo.RepoDirName
	}
	return sandbox.RepoNameFromURL(repo.RepoURL)
}

func (s *Service) acquire(ctx context.Context) (func(), error) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) scoped(db *gorm.DB, scope Scope) *gorm.DB {
	return db.Where("owner_kind = ? AND owner_id = ? AND generation = ?", scope.Kind, scope.OwnerID, scope.Generation)
}

func (s *Service) ownerMutex(scope Scope) *sync.Mutex {
	return &s.locks[sha256.Sum256([]byte(scope.Kind + "/" + scope.OwnerID))[0]%uint8(len(s.locks))]
}

// Initialize reserves the baseline before reading Git. A failed first read
// stays unavailable, since a later successful read can already include edits.
func (s *Service) Initialize(ctx context.Context, sb *sandbox.Sandbox, scope Scope) {
	if s == nil || sb == nil || len(scope.Repos) == 0 {
		return
	}
	mu := s.ownerMutex(scope)
	mu.Lock()
	defer mu.Unlock()
	if scope.Kind == models.WorkspaceChangesRun {
		var count int64
		err := s.db.WithContext(ctx).Model(&models.Run{}).
			Where("id = ? AND workspace_changes_enabled = ?", scope.OwnerID, true).Count(&count).Error
		if err != nil || count == 0 {
			return
		}
	}
	row := models.WorkspaceChangeBaseline{OwnerKind: scope.Kind, OwnerID: scope.OwnerID, Generation: scope.Generation}
	claimed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOwner(tx, scope.Kind, scope.OwnerID); err != nil {
			return err
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		claimed = res.RowsAffected > 0
		return res.Error
	})
	if err != nil || !claimed {
		s.report(scope, err)
		return
	}
	captureCtx, cancel := context.WithTimeout(ctx, 16*time.Second)
	defer cancel()
	trees, err := s.capture(captureCtx, sb, scope, true)
	if err == nil {
		row.Trees = trees
		err = s.saveBaseline(captureCtx, scope, &row)
	}
	if err != nil || len(trees) == 0 {
		s.report(scope, err)
		return
	}
	_, err = s.compare(captureCtx, sb, scope, trees, trees, "", false, time.Now().UTC())
	s.report(scope, err)
	if scope.Kind == models.WorkspaceChangesRun {
		s.report(scope, s.archiveBaseline(ctx, sb, scope, &row))
	}
}

// Begin captures the real workspace before a turn, including existing local edits.
func (s *Service) Begin(ctx context.Context, sb *sandbox.Sandbox, scope Scope) Trees {
	if s == nil || sb == nil || len(scope.Repos) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	trees, err := s.capture(ctx, sb, scope, false)
	s.report(scope, err)
	return trees
}

func (s *Service) capture(ctx context.Context, sb treeReader, scope Scope, baseline bool) (Trees, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	queries := make([]sandbox.WorkspaceTreeQuery, len(scope.Repos))
	for i, repo := range scope.Repos {
		dir := directory(repo)
		queries[i].Directory = dir
		if baseline {
			queries[i].Pin = baselinePin(scope, dir)
			queries[i].TreeOnly = scope.Kind == models.WorkspaceChangesRun
		}
	}
	trees, err := sb.CaptureWorkspaceTrees(ctx, queries)
	if err != nil {
		return nil, err
	}
	out := make(Trees, len(trees))
	for i, tree := range trees {
		if !tree.Unavailable {
			out[queries[i].Directory] = models.WorkspaceChangeTree{Ref: tree.Ref, Head: tree.Head, Branch: tree.Branch}
		}
	}
	return out, nil
}

// Finish archives a settled workspace and, when supplied, an individual turn.
// Lifecycle callers use a bounded detached context so cancellation still
// records the files the agent changed before it stopped.
func (s *Service) Finish(ctx context.Context, sb *sandbox.Sandbox, scope Scope, turnID string, before Trees, interrupted bool) {
	if s == nil || sb == nil || len(scope.Repos) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 24*time.Second)
	defer cancel()
	_, err := s.record(ctx, sb, scope, turnID, before, interrupted)
	s.report(scope, err)
}

func (s *Service) Refresh(ctx context.Context, sb *sandbox.Sandbox, scope Scope) error {
	if s == nil || sb == nil || len(scope.Repos) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()
	_, err := s.record(ctx, sb, scope, "", nil, false)
	return err
}

func (s *Service) record(ctx context.Context, sb treeReader, scope Scope, turnID string, before Trees, interrupted bool) (*models.WorkspaceChangeSnapshot, error) {
	lock := s.ownerMutex(scope)
	lock.Lock()
	defer lock.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var baseline models.WorkspaceChangeBaseline
	baselineErr := s.scoped(s.db.WithContext(ctx), scope).Take(&baseline).Error
	if baselineErr != nil && !errors.Is(baselineErr, gorm.ErrRecordNotFound) {
		return nil, baselineErr
	}
	if len(baseline.Trees) == 0 && turnID == "" {
		return nil, nil
	}
	after, err := s.capture(ctx, sb, scope, false)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	var turnErr error
	if turnID != "" {
		// Advance the last-turn record even if the new turn has no changes
		// or its before snapshot could not be captured.
		_, turnErr = s.compare(ctx, sb, scope, before, after, turnID, interrupted, at)
	}
	if len(baseline.Trees) == 0 {
		return nil, turnErr
	}
	snapshot, err := s.compare(ctx, sb, scope, baseline.Trees, after, "", false, at)
	return snapshot, errors.Join(turnErr, err)
}

func (s *Service) compare(ctx context.Context, sb treeReader, scope Scope, before, after Trees, turnID string, interrupted bool, at time.Time) (*models.WorkspaceChangeSnapshot, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	item := &models.WorkspaceChangeSnapshot{
		ID: uuid.NewString(), OwnerKind: scope.Kind, OwnerID: scope.OwnerID, Generation: scope.Generation,
		Workspace: scope.Workspace, NodeID: scope.NodeID, Iteration: scope.Iteration,
		TurnID: turnID, Interrupted: interrupted, CapturedAt: at,
		Repos: make([]models.WorkspaceChangeRepository, len(scope.Repos)),
	}
	queries := make([]sandbox.WorkspaceTreeQuery, len(scope.Repos))
	for i, repo := range scope.Repos {
		dir := directory(repo)
		from, to := before[dir], after[dir]
		queries[i] = sandbox.WorkspaceTreeQuery{Directory: dir, From: from.Ref, To: to.Ref}
		name := repo.Name
		if name == "" {
			name = dir
		}
		item.Repos[i] = models.WorkspaceChangeRepository{
			Name: name, Directory: dir, Branch: to.Branch, BaseBranch: from.Branch, BaseSHA: from.Head,
			HeadSHA: to.Head, From: from.Ref, To: to.Ref, Files: []models.WorkspaceChangePath{},
		}
	}
	last, readErr := s.latest(ctx, scope, scope.Workspace, turnID != "", scope.NodeID, scope.Iteration)
	if readErr == nil && sameComparison(last, item) {
		return last, nil
	}
	raw, err := sb.CompareWorkspaceTrees(ctx, queries)
	if err != nil {
		return nil, err
	}
	if turnID == "" {
		s.recoverBaseline(ctx, sb, scope, before, queries, raw)
	}
	content := make([]models.WorkspaceChangeContent, 0)
	available := false
	for i, report := range raw {
		repo := &item.Repos[i]
		repo.Unavailable = report.Unavailable
		if report.Unavailable {
			continue
		}
		available = true
		repo.Added, repo.Deleted, repo.FileCount = report.Added, report.Deleted, report.FileCount
		for _, file := range report.Files {
			path, payload := changeFile(item.ID, file)
			repo.Files = append(repo.Files, path)
			if path.HasPatch || path.HasBefore || path.HasAfter {
				content = append(content, payload)
			}
		}
	}
	if !available && turnID == "" {
		return nil, ErrUnavailable
	}
	archiveCtx, err := s.stageArchive(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(archiveCtx).Transaction(func(tx *gorm.DB) error {
		if err := lockOwner(tx, scope.Kind, scope.OwnerID); err != nil {
			return err
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		if len(content) > 0 {
			if err := tx.CreateInBatches(&content, 25).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&models.WorkspaceChangeGarbage{ID: item.ID}).Error; err != nil {
			return err
		}
		return s.prune(tx, scope, turnID != "")
	})
	if err != nil {
		s.abandonArchive(ctx, item.ID)
		return nil, err
	}
	return item, nil
}

func changeFile(snapshot string, file sandbox.WorkspaceTreeFile) (models.WorkspaceChangePath, models.WorkspaceChangeContent) {
	id := uuid.NewString()
	path := models.WorkspaceChangePath{
		ID: id, Path: file.Path, PreviousPath: file.PreviousPath, Status: file.Status,
		Added: file.Added, Deleted: file.Deleted,
		HasPatch: file.Patch != nil, HasBefore: file.Before != nil, HasAfter: file.After != nil,
	}
	part := models.WorkspaceChangeContent{SnapshotID: snapshot, FileID: id}
	if path.HasPatch {
		part.Patch = *file.Patch
	}
	if path.HasBefore {
		part.Before = *file.Before
	}
	if path.HasAfter {
		part.After = *file.After
	}
	return path, part
}

func sameComparison(last, next *models.WorkspaceChangeSnapshot) bool {
	if last == nil || last.TurnID != next.TurnID || last.NodeID != next.NodeID ||
		(next.TurnID != "" && last.Iteration != next.Iteration) ||
		last.Interrupted != next.Interrupted || len(last.Repos) != len(next.Repos) {
		return false
	}
	for i, a := range last.Repos {
		b := next.Repos[i]
		// The refs in a persisted row are populated by the JSON serializer.
		if a.Unavailable || a.From == "" || a.To == "" || a.From != b.From || a.To != b.To || a.Branch != b.Branch || a.HeadSHA != b.HeadSHA {
			return false
		}
	}
	return true
}

func (s *Service) report(scope Scope, err error) {
	if err != nil {
		log.Debug().Str("scope", scope.Kind).Str("id", scope.OwnerID).Msg("workspace change snapshot unavailable")
	}
}
