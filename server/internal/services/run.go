package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// defaultRunListOrder is the production default: hybrid time DESC + id DESC.
// Queued runs use created_at; all others use started_at.
// Claim order (Priority → remaining human_gate → FIFO) is independent — do not
// change this clause to mirror claim (Demo s4 / clarified f7).
const defaultRunListOrder = "CASE WHEN status = 'queued' THEN created_at ELSE started_at END DESC, id DESC"

// runListOrderBy maps whitelist sort/order to a fixed ORDER BY clause.
// sort and order must both be valid as a pair; otherwise the default order is used.
// Never concatenates raw user input into SQL identifiers.
func runListOrderBy(sort, order string) string {
	sort = strings.TrimSpace(sort)
	order = strings.ToLower(strings.TrimSpace(order))
	var dir string
	switch order {
	case "asc":
		dir = "ASC"
	case "desc":
		dir = "DESC"
	default:
		return defaultRunListOrder
	}
	switch sort {
	case "started_at":
		return "CASE WHEN status = 'queued' THEN created_at ELSE started_at END " + dir + ", id DESC"
	case "priority":
		return "priority " + dir + ", id DESC"
	case "last_activity":
		return "COALESCE(last_activity_at, CASE WHEN status = 'queued' THEN created_at ELSE started_at END) " + dir + ", id DESC"
	default:
		return defaultRunListOrder
	}
}

// deletableRunStatuses is the allowlist for terminal DeleteRun statuses.
// A paused run is also deletable when its workspace save has already failed.
// Queued, running, waiting_human, still-saving, and ordinary pauses are not.
var deletableRunStatuses = []string{"completed", "failed", "cancelled"}

var (
	// ErrRunNotFound is returned when Delete targets a missing run id.
	ErrRunNotFound = errors.New("run not found")
	// ErrRunNotDeletable is returned when the run status is outside the
	// deletable set (queued/running/waiting_human, still saving, ordinary pause).
	ErrRunNotDeletable = errors.New("cannot delete run: only completed, failed, cancelled, or save-failed runs can be deleted")
	// errDeleteNeedsSandboxRelease rolls back the pre-check transaction so a
	// save-failed run can release its sandbox before rows are removed.
	errDeleteNeedsSandboxRelease = errors.New("delete needs sandbox release")
)

// runSandboxReleaser destroys containers still held by a run. Delete calls it
// for a save-failed run instead of only removing sandbox rows.
type runSandboxReleaser interface {
	ReleaseHeldRunSandboxes(ctx context.Context, runID string) error
}

// RunService assembles run views from the underlying tables.
type RunService struct {
	db                  *gorm.DB
	sandboxReleaser     runSandboxReleaser
	beforeDeleteRecheck func(tx *gorm.DB, id string)
}

// SetSandboxReleaser installs the sandbox teardown used when deleting a run
// whose workspace save has failed. Without it, that delete is refused.
func (s *RunService) SetSandboxReleaser(r runSandboxReleaser) {
	if s != nil {
		s.sandboxReleaser = r
	}
}

// NewRunService builds the service.
func NewRunService(db *gorm.DB) *RunService { return &RunService{db: db} }

func applyRunTagsFilter(q *gorm.DB, column string, tags []string) *gorm.DB {
	for _, tag := range tags {
		switch q.Dialector.Name() {
		case "mysql":
			candidate, _ := json.Marshal(tag)
			q = q.Where("JSON_CONTAINS(COALESCE("+column+", JSON_ARRAY()), ?, '$')", string(candidate))
		default:
			q = q.Where("EXISTS (SELECT 1 FROM json_each(COALESCE("+column+", '[]')) WHERE value = ?)", tag)
		}
	}
	return q
}

func (s *RunService) listQuery(statuses []string, wf string, tags []string) *gorm.DB {
	q := s.db.Model(&models.Run{})
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	if wf != "" {
		q = q.Where("workflow_id = ?", wf)
	}
	q = applyRunTagsFilter(q, "runs.tags", tags)
	return q
}

// List returns runs, newest first by default. Queued runs have zero StartedAt
// (waiting time is excluded from duration), so they sort by created_at; all
// other statuses sort by started_at. Optional statuses (OR) and wf filter
// with AND semantics.
//
// Optional trailing sortOrder is [sort, order]. Allowed sort: started_at|priority;
// allowed order: asc|desc. Both must be valid as a pair; otherwise the default
// hybrid-time DESC + id DESC is kept. Omitted args keep the default.
func (s *RunService) List(statuses []string, wf string, sortOrder ...string) []models.Run {
	return s.ListByTags(statuses, wf, nil, sortOrder...)
}

func (s *RunService) ListByTags(statuses []string, wf string, tags []string, sortOrder ...string) []models.Run {
	sort, order := parseSortOrderArgs(sortOrder)
	var runs []models.Run
	if err := s.listQuery(statuses, wf, tags).
		Order(runListOrderBy(sort, order)).
		Find(&runs).Error; err != nil {
		log.Error().Err(err).Str("workflow_id", wf).Msg("list runs failed")
	}
	return runs
}

// ListPage returns a page of runs plus the total matching count.
// Optional trailing sortOrder is [sort, order]; see List for whitelist rules.
func (s *RunService) ListPage(statuses []string, wf string, page, pageSize int, sortOrder ...string) ([]models.Run, int64, error) {
	return s.ListPageByTags(statuses, wf, nil, page, pageSize, sortOrder...)
}

// ListPageByTags is ListPage with a tag filter. A database error is returned
// rather than an empty page, so an unreadable store is not shown as "no runs".
func (s *RunService) ListPageByTags(statuses []string, wf string, tags []string, page, pageSize int, sortOrder ...string) ([]models.Run, int64, error) {
	sort, order := parseSortOrderArgs(sortOrder)
	q := s.listQuery(statuses, wf, tags)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count runs: %w", err)
	}
	var runs []models.Run
	offset := (page - 1) * pageSize
	if err := q.Order(runListOrderBy(sort, order)).
		Limit(pageSize).Offset(offset).Find(&runs).Error; err != nil {
		return nil, 0, fmt.Errorf("list runs: %w", err)
	}
	return runs, total, nil
}

// Ping checks the database is reachable and the runs table is readable.
func (s *RunService) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return err
	}
	var n int64
	return s.db.WithContext(ctx).Raw("SELECT 1 FROM runs LIMIT 1").Scan(&n).Error
}

func parseSortOrderArgs(sortOrder []string) (sort, order string) {
	if len(sortOrder) >= 1 {
		sort = sortOrder[0]
	}
	if len(sortOrder) >= 2 {
		order = sortOrder[1]
	}
	return sort, order
}

// Get returns a single run.
func (s *RunService) Get(id string) (models.Run, bool) {
	var run models.Run
	if err := s.db.First(&run, "id = ?", id).Error; err != nil {
		return models.Run{}, false
	}
	return run, true
}

// States returns the per-node StateRun records for a run.
func (s *RunService) States(runID string) []models.StateRun {
	var states []models.StateRun
	// Ordered oldest→newest per node so callers can treat the last row for a
	// node as its latest execution and build an in-order execution history.
	s.db.Where("run_id = ?", runID).Order("iteration asc, id asc").Find(&states)
	return states
}

// TaskStates returns the run's StateRuns in execution order (id asc) with
// only the columns task previews need (no events / snapshots / MCP calls).
func (s *RunService) TaskStates(runID string) []models.StateRun {
	var states []models.StateRun
	s.db.Select("id", "run_id", "node_id", "node_type", "iteration", "status", "outputs", "vars_before").
		Where("run_id = ?", runID).Order("id asc").Find(&states)
	return states
}

// FeedbackEvents returns the run's human feedback rounds in order, satisfying
// the run-history tools' provider contract.
func (s *RunService) FeedbackEvents(runID string) []models.FeedbackEvent {
	return NewFeedbackService(s.db).Events(runID)
}

// StateRun returns one node's persisted record (used as the event-log fallback
// once the live sandbox is gone).
func (s *RunService) StateRun(runID, nodeID string) (models.StateRun, bool) {
	var sr models.StateRun
	// Latest execution of the node (a node may have several after loop-backs).
	if err := s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		return models.StateRun{}, false
	}
	return sr, true
}

// Variables returns the live global variable values for a run.
func (s *RunService) Variables(runID string) []models.RunVariable {
	var vars []models.RunVariable
	s.db.Where("run_id = ?", runID).Find(&vars)
	return vars
}

// PendingGate returns the unresolved gate for a run, if any. Terminal runs
// (completed / failed / cancelled) never surface a pending gate: once a run ends
// any dangling gate is not actionable, so a stale row must not appear as an open
// approval on the run detail (the engine also supersedes such gates on finish).
// Gates whose node has already left waiting_human are also excluded.
func (s *RunService) PendingGate(runID string) (models.Gate, bool) {
	var g models.Gate
	if err := pendingGateScope(s.db).
		Where("gates.run_id = ?", runID).
		Order("gates.iteration desc, gates.id desc").First(&g).Error; err != nil {
		return models.Gate{}, false
	}
	return g, true
}

// AllPendingGates returns every unresolved gate whose run is still alive and
// whose node is still waiting_human (gates inbox). Gates on terminal runs
// (completed / failed / cancelled) are excluded: once a run ends its dangling
// gate is no longer actionable and must not linger in the approvals inbox.
func (s *RunService) AllPendingGates() []models.Gate {
	var gates []models.Gate
	pendingGateScope(s.db).Order("gates.requested_at desc").Find(&gates)
	return gates
}

// Conversation returns the react conversation the run is currently at: the
// active (not-done) dialogue if the run is paused awaiting human input,
// otherwise the most recent completed one. A run may accumulate several react
// conversations (one per react node); returning the oldest would strand the
// user on a finished dialogue while a later react node waits for a reply.
func (s *RunService) Conversation(runID string) (models.ReactConversation, bool) {
	var conv models.ReactConversation
	if err := s.db.Where("run_id = ? AND done = ?", runID, false).
		Order("id desc").First(&conv).Error; err == nil {
		return conv, true
	}
	if err := s.db.Where("run_id = ?", runID).Order("id desc").First(&conv).Error; err != nil {
		return models.ReactConversation{}, false
	}
	return conv, true
}

// Conversations returns every react conversation accumulated by a run (one per
// react node), so the UI can render each react node's own dialogue history
// rather than only the run's current one.
func (s *RunService) Conversations(runID string) []models.ReactConversation {
	var convs []models.ReactConversation
	s.db.Where("run_id = ?", runID).Order("id asc").Find(&convs)
	return convs
}

// DB exposes the underlying handle for handlers needing ad-hoc queries.
func (s *RunService) DB() *gorm.DB { return s.db }

// RunTags returns every distinct run tag.
func (s *RunService) RunTags() []string {
	var rows []models.Run
	s.db.Model(&models.Run{}).Select("tags").Find(&rows)
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, row := range rows {
		for _, tag := range row.Tags {
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

// Delete permanently removes a completed, failed, cancelled, or save-failed run
// and its associated rows so the run no longer appears in lists, details, or
// other run entry points. Queued, running, waiting_human, still-saving, and
// ordinary paused runs are rejected with ErrRunNotDeletable. Missing runs
// return ErrRunNotFound. Does not touch WorkflowDef, WorkflowVersion, or
// WorkflowAPIKey.
//
// A save-failed delete holds LockRunMutation across the status check, sandbox
// Destroy, and row removal, the same lock a failure retry holds until the run
// is queued or running. If the run has left save-failed — including a
// transitional failed written before admission — Destroy is not called and the
// run, its artifacts, and its sandbox stay.
func (s *RunService) Delete(id string) error {
	run, ok := s.Get(id)
	if !ok {
		return ErrRunNotFound
	}
	if err := s.rejectIfNotDeletable(s.db, run); err != nil {
		return err
	}
	if !WorkspaceSaveFailed(s.db, run) {
		return s.deleteTerminalRun(id)
	}
	return s.deleteSaveFailedRun(id)
}

func (s *RunService) deleteTerminalRun(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.lockDeletable(tx, id, true); err != nil {
			return err
		}
		if err := EndOpenWorkspaceSavesTx(tx, id); err != nil {
			return err
		}
		return deleteRunRows(tx, id)
	})
}

// deleteSaveFailedRun releases the sandbox only while the run is still
// save-failed, then removes rows under the same mutation lock.
func (s *RunService) deleteSaveFailedRun(id string) error {
	unlock := LockRunMutation(id)
	defer unlock()

	err := s.db.Transaction(func(tx *gorm.DB) error {
		current, err := s.lockDeletable(tx, id, true)
		if err != nil {
			return err
		}
		if !WorkspaceSaveFailed(tx, current) {
			return fmt.Errorf("%w: status changed before delete", ErrRunNotDeletable)
		}
		if s.sandboxReleaser == nil {
			return errors.New("cannot delete save-failed run without releasing its sandbox")
		}
		return errDeleteNeedsSandboxRelease
	})
	if !errors.Is(err, errDeleteNeedsSandboxRelease) {
		return err
	}
	current, ok := s.Get(id)
	if !ok {
		return ErrRunNotFound
	}
	if !WorkspaceSaveFailed(s.db, current) {
		return fmt.Errorf("%w: status changed before sandbox release", ErrRunNotDeletable)
	}
	if err := s.sandboxReleaser.ReleaseHeldRunSandboxes(context.Background(), id); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		current, err := s.lockDeletable(tx, id, false)
		if err != nil {
			return err
		}
		// Destroy can take seconds. A failure retry that slipped through must
		// not have its transitional failed, or a now-active run, deleted.
		if !WorkspaceSaveFailed(tx, current) {
			return fmt.Errorf("%w: status changed during sandbox release", ErrRunNotDeletable)
		}
		if err := EndOpenWorkspaceSavesTx(tx, id); err != nil {
			return err
		}
		return deleteRunRows(tx, id)
	})
}

// EnsureSaveFailedForRelease reports whether a sandbox Destroy is still
// allowed. Queued, running, waiting_human, and a transitional failed are not.
func EnsureSaveFailedForRelease(db *gorm.DB, runID string) error {
	if db == nil {
		return errors.New("cannot release a sandbox without a database")
	}
	var run models.Run
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRunNotFound
		}
		return err
	}
	if !WorkspaceSaveFailed(db, run) {
		return fmt.Errorf("%w: sandbox release aborted", ErrRunNotDeletable)
	}
	return nil
}

func (s *RunService) lockDeletable(tx *gorm.DB, id string, applyHook bool) (models.Run, error) {
	var current models.Run
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return current, ErrRunNotFound
		}
		return current, err
	}
	if applyHook && s.beforeDeleteRecheck != nil {
		s.beforeDeleteRecheck(tx, id)
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return current, ErrRunNotFound
			}
			return current, err
		}
	}
	if err := s.rejectIfNotDeletable(tx, current); err != nil {
		return current, err
	}
	return current, nil
}

func deleteRunRows(tx *gorm.DB, id string) error {
	for _, m := range []any{
		&models.WorkflowInstruction{},
		&models.WorkflowInstructionMailbox{},
		&models.StateRun{},
		&models.RunVariable{},
		&models.Artifact{},
		&models.Gate{},
		&models.ReactConversation{},
		&models.RunReference{},
		&models.PreviewIssue{},
		&models.RunPreviewPort{},
		&models.SandboxLog{},
		&models.WorkflowVisitCheckpoint{},
		&models.Sandbox{},
	} {
		if err := tx.Where("run_id = ?", id).Delete(m).Error; err != nil {
			return err
		}
	}
	return tx.Delete(&models.Run{}, "id = ?", id).Error
}

func (s *RunService) rejectIfNotDeletable(db *gorm.DB, run models.Run) error {
	if containsString(deletableRunStatuses, run.Status) {
		return nil
	}
	if WorkspaceSaveFailed(db, run) {
		return nil
	}
	switch run.Status {
	case "queued", "running", "waiting_human":
		return fmt.Errorf("%w: cancel or wait until the run ends (status %q)", ErrRunNotDeletable, run.Status)
	default:
		if WorkspaceSaveInProgress(db, run) {
			return fmt.Errorf("%w: workspace save still in progress", ErrRunNotDeletable)
		}
		return fmt.Errorf("%w (status %q)", ErrRunNotDeletable, run.Status)
	}
}
