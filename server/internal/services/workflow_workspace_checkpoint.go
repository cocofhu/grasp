package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const WorkflowCheckpointOutcomeLimit int64 = 16 << 20

var ErrWorkflowCheckpointBlocked = errors.New("workflow workspace checkpoint requires reconciliation")

type WorkflowCheckpointOptions struct {
	Enabled        bool
	Execution      *executionlineage.Controller
	AccountExists  func(string) bool
	ReadScope      func(context.Context, *gorm.DB, string, string, string) error
	ResolveBinding func(context.Context, string, string, string, uint) (WorkspaceActiveBinding, error)
	Capture        func(context.Context, string, WorkspaceCaptureInput) (WorkspaceSnapshotMetadata, error)
	Reconcile      func(context.Context, string, string, string, string) (WorkspaceSnapshotMetadata, error)
	MaxDuration    time.Duration
}

// WorkflowWorkspaceCheckpoints owns the explicit immutable route source and
// the exact visit's file-capture barrier. It uses the application's one P7
// pipeline; it starts no worker and never replays an Agent or an unknown job.
type WorkflowWorkspaceCheckpoints struct {
	db      *gorm.DB
	files   *filestore.Store
	options WorkflowCheckpointOptions
}

func NewWorkflowWorkspaceCheckpoints(db *gorm.DB, options WorkflowCheckpointOptions) (*WorkflowWorkspaceCheckpoints, error) {
	if db == nil || options.Execution == nil || options.AccountExists == nil || options.ReadScope == nil || options.Enabled && (options.ResolveBinding == nil || options.Capture == nil || options.Reconcile == nil) || messageOutsideTransaction(db) != nil {
		return nil, ErrWorkspaceJobAuthority
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return nil, err
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 30 * time.Minute
	}
	return &WorkflowWorkspaceCheckpoints{db: db, files: files, options: options}, nil
}

func (s *WorkflowWorkspaceCheckpoints) Enabled() bool                  { return s.options.Enabled }
func (s *WorkflowWorkspaceCheckpoints) CaptureDuration() time.Duration { return s.options.MaxDuration }

// LaunchActor accepts only an authenticated server admission envelope. API
// credentials remain a workflow-scoped principal, never an invented account.
func (s *WorkflowWorkspaceCheckpoints) LaunchActor(ctx context.Context, input RunStart) string {
	if input.ActorID == "" {
		return ""
	}
	if input.Source == "api" {
		const prefix = "api-key:"
		if !strings.HasPrefix(input.ActorID, prefix) {
			return ""
		}
		var key struct{ ID string }
		if err := s.db.WithContext(ctx).Table("workflow_api_keys").Select("id").Where("id = ? AND workflow_id = ? AND revoked_at IS NULL", strings.TrimPrefix(input.ActorID, prefix), input.WorkflowID).Take(&key).Error; err != nil {
			return ""
		}
		return input.ActorID
	}
	if (input.Source == "web" || input.Source == "solo" || input.Source == "pm") && s.options.AccountExists(input.ActorID) {
		return input.ActorID
	}
	return ""
}

// Actor uses a dedicated launch grant or the current originating Solo owner.
// CreatedBy remains attribution and is deliberately never consulted here.
func (s *WorkflowWorkspaceCheckpoints) Actor(ctx context.Context, run models.Run) (string, error) {
	actor := run.WorkspaceActorID
	if run.OriginSoloID != "" {
		var parent struct{ UserID string }
		if err := s.db.WithContext(ctx).Table("solo_sessions").Select("user_id").Take(&parent, "id = ?", run.OriginSoloID).Error; err != nil {
			return "", err
		}
		actor = parent.UserID
	}
	if actor == "" || !strings.HasPrefix(actor, "api-key:") && !s.options.AccountExists(actor) {
		return "", ErrWorkspaceJobAuthority
	}
	if err := s.options.ReadScope(ctx, s.db.WithContext(ctx), actor, "run", run.ID); err != nil {
		return "", err
	}
	return actor, nil
}

func WorkflowCheckpointID(run string, stateRun uint, control uint64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("visit-checkpoint-v1\x00%s\x00%d\x00%d", run, stateRun, control)))
	return hex.EncodeToString(digest[:])
}

type PreparedWorkflowCheckpoint struct {
	service    *WorkflowWorkspaceCheckpoints
	id, digest string
	body       filestore.Prepared
}

func (s *WorkflowWorkspaceCheckpoints) PrepareOutcome(ctx context.Context, row models.WorkflowVisitCheckpoint, body []byte) (PreparedWorkflowCheckpoint, error) {
	if err := messageOutsideTransaction(s.db); err != nil {
		return PreparedWorkflowCheckpoint{}, err
	}
	digest := sha256.Sum256(body)
	if len(body) == 0 || !json.Valid(body) || int64(len(body)) > WorkflowCheckpointOutcomeLimit || hex.EncodeToString(digest[:]) != row.OutcomeSHA256 {
		return PreparedWorkflowCheckpoint{}, ErrWorkspaceJobInvalid
	}
	prepared, err := s.files.PrepareBounded(ctx, bytes.NewReader(body), filestore.JSON, WorkflowCheckpointOutcomeLimit)
	if err != nil {
		return PreparedWorkflowCheckpoint{}, err
	}
	return PreparedWorkflowCheckpoint{service: s, id: row.ID, digest: row.OutcomeSHA256, body: prepared}, nil
}

func checkpointEntities(row models.WorkflowVisitCheckpoint) []executionlineage.Entity {
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityRun, ID: row.RunID}, {Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(row.StateRunID), 10)}}
	if row.SandboxID != 0 {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(row.SandboxID), 10)})
	}
	return entities
}

func (s *WorkflowWorkspaceCheckpoints) GuardTx(ctx context.Context, tx *gorm.DB, row models.WorkflowVisitCheckpoint) error {
	if err := s.options.Execution.GuardContextTx(ctx, tx); err != nil {
		return err
	}
	entities := checkpointEntities(row)
	var run struct{ OriginSoloID string }
	if err := tx.Table("runs").Select("origin_solo_id").Take(&run, "id = ?", row.RunID).Error; err != nil {
		return err
	}
	if run.OriginSoloID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
	}
	return s.options.Execution.GuardContextTx(ctx, tx, entities...)
}

// ReserveTx runs before any outcome PUT and fences a shared disk immediately.
// Caller locks Run/StateRun and validates the original graph/control tuple.
func (s *WorkflowWorkspaceCheckpoints) ReserveTx(ctx context.Context, tx *gorm.DB, row models.WorkflowVisitCheckpoint) error {
	if !restoreDigest(row.ID) || row.ID != WorkflowCheckpointID(row.RunID, row.StateRunID, row.ControlVersion) || !restoreDigest(row.GraphSHA256) || !restoreDigest(row.OutcomeSHA256) || row.Iteration < 1 || row.StateRunID == 0 || row.RouteState != "sealing" || row.OutcomeRef != "" {
		return ErrWorkspaceJobInvalid
	}
	if err := s.GuardTx(ctx, tx, row); err != nil {
		return err
	}
	if row.SandboxID != 0 {
		if err := guardWorkflowWorkspaceReuseTx(ctx, tx, row.SandboxID, row.ID); err != nil {
			return err
		}
	}
	var prior models.WorkflowVisitCheckpoint
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&prior, "id = ?", row.ID).Error
	if err == nil {
		if prior.RunID != row.RunID || prior.StateRunID != row.StateRunID || prior.OutcomeSHA256 != row.OutcomeSHA256 || prior.GraphSHA256 != row.GraphSHA256 || prior.SandboxID != row.SandboxID {
			return ErrWorkspaceJobConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(&row).Error
}

func (s *WorkflowWorkspaceCheckpoints) PublishOutcomeTx(ctx context.Context, tx *gorm.DB, row models.WorkflowVisitCheckpoint, prepared PreparedWorkflowCheckpoint) error {
	if prepared.service != s || prepared.id != row.ID || prepared.digest != row.OutcomeSHA256 {
		return ErrWorkspaceJobAuthority
	}
	ref, err := s.files.PreparedRef(prepared.body)
	if err != nil {
		return err
	}
	if err := s.GuardTx(ctx, tx, row); err != nil {
		return err
	}
	result := tx.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ? AND outcome_sha256 = ? AND route_state = ? AND (outcome_ref = '' OR outcome_ref IS NULL)", row.ID, row.OutcomeSHA256, "sealing").Updates(map[string]any{"outcome_ref": ref, "route_state": "sealed"})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (s *WorkflowWorkspaceCheckpoints) Get(ctx context.Context, id string) (models.WorkflowVisitCheckpoint, error) {
	var row models.WorkflowVisitCheckpoint
	if !restoreDigest(id) {
		return row, gorm.ErrRecordNotFound
	}
	err := s.db.WithContext(ctx).Take(&row, "id = ?", id).Error
	return row, err
}

func (s *WorkflowWorkspaceCheckpoints) LoadOutcome(ctx context.Context, row models.WorkflowVisitCheckpoint) ([]byte, error) {
	if err := messageOutsideTransaction(s.db); err != nil {
		return nil, err
	}
	if row.OutcomeRef == "" || !restoreDigest(row.OutcomeSHA256) {
		return nil, ErrWorkflowCheckpointBlocked
	}
	body, err := s.files.LoadBounded(ctx, row.OutcomeRef, filestore.JSON, WorkflowCheckpointOutcomeLimit)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != row.OutcomeSHA256 {
		return nil, ErrWorkspaceJobConflict
	}
	return body, nil
}

// GuardRouteTx rechecks the immutable evidence in the same transaction that
// applies the sealed route. It never reads an object or wakes a provider.
func (s *WorkflowWorkspaceCheckpoints) GuardRouteTx(ctx context.Context, tx *gorm.DB, row models.WorkflowVisitCheckpoint) error {
	if err := s.GuardTx(ctx, tx, row); err != nil {
		return err
	}
	if row.SnapshotID != "" {
		var source struct{ ID string }
		if err := tx.Table("workspace_snapshots").Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Take(&source, "id = ?", row.SnapshotID).Error; err != nil {
			return err
		}
	}
	if row.SandboxID != 0 {
		var source struct {
			ID, StateRunID                         uint
			Name, RunID, NodeID                    string
			Iteration, RunAttempt, ProviderAttempt int
		}
		if err := tx.Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "state_run_id", "name", "run_id", "node_id", "iteration", "run_attempt", "provider_attempt").Take(&source, row.SandboxID).Error; err != nil {
			return err
		}
		if source.Name != row.Resource || source.RunID != row.RunID || source.NodeID != row.NodeID || source.StateRunID != row.StateRunID || source.Iteration != row.Iteration || source.RunAttempt != row.RunAttempt || source.ProviderAttempt != row.ProviderAttempt {
			return ErrWorkspaceJobConflict
		}
	}
	var current models.WorkflowVisitCheckpoint
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current, "id = ?", row.ID).Error; err != nil {
		return err
	}
	if current.RouteState != "sealed" || current.OutcomeRef != row.OutcomeRef || current.OutcomeSHA256 != row.OutcomeSHA256 || current.SnapshotID != row.SnapshotID || current.FileState != row.FileState || !checkpointFilesComplete(current.FileState) {
		return ErrWorkflowCheckpointBlocked
	}
	if current.FileState != "ready" {
		return nil
	}
	if err := s.options.ReadScope(ctx, tx, row.ActorID, "run", row.RunID); err != nil {
		return err
	}
	var snapshot struct {
		State, ActorID, RunID, NodeID, Lineage, ArchiveRef, ManifestRef, SourceBindingFingerprint string
		SourceSandboxID, StateRunID                                                               uint
		Iteration, RunAttempt, ProviderAttempt                                                    int
		VerifiedAt                                                                                *time.Time
	}
	if err := tx.Table("workspace_snapshots").Clauses(clause.Locking{Strength: "SHARE"}).Select("state", "actor_id", "run_id", "node_id", "lineage", "archive_ref", "manifest_ref", "source_binding_fingerprint", "source_sandbox_id", "state_run_id", "iteration", "run_attempt", "provider_attempt", "verified_at").Take(&snapshot, "id = ?", row.SnapshotID).Error; err != nil {
		return err
	}
	if snapshot.State != "ready" || snapshot.VerifiedAt == nil || snapshot.ArchiveRef == "" || snapshot.ManifestRef == "" || snapshot.ActorID != row.ActorID || snapshot.RunID != row.RunID || snapshot.NodeID != row.NodeID || snapshot.Lineage != row.Lineage || snapshot.SourceBindingFingerprint != row.BindingFingerprint || snapshot.SourceSandboxID != row.SandboxID || snapshot.StateRunID != row.StateRunID || snapshot.Iteration != row.Iteration || snapshot.RunAttempt != row.RunAttempt || snapshot.ProviderAttempt != row.ProviderAttempt {
		return ErrWorkspaceJobConflict
	}
	return nil
}

// GuardWorkflowWorkspaceReuseTx is deliberately a current locking read after
// the same Sandbox row fence used by physical leases. An Agent, re-prime or
// branch checkout cannot replace provenance while an older checkpoint owns it.
func GuardWorkflowWorkspaceReuseTx(ctx context.Context, tx *gorm.DB, sandboxID uint) error {
	return guardWorkflowWorkspaceReuseTx(ctx, tx, sandboxID, "")
}

func guardWorkflowWorkspaceReuseTx(ctx context.Context, tx *gorm.DB, sandboxID uint, sameCheckpoint string) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceJobInvalid
	}
	var resource struct{ ID uint }
	if err := tx.WithContext(ctx).Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Take(&resource, sandboxID).Error; err != nil {
		return err
	}
	var checkpoints []struct{ ID string }
	if err := tx.WithContext(ctx).Model(&models.WorkflowVisitCheckpoint{}).Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("sandbox_id = ? AND route_state <> ?", sandboxID, "applied").Find(&checkpoints).Error; err != nil {
		return err
	}
	for _, row := range checkpoints {
		if row.ID != sameCheckpoint {
			return ErrWorkflowCheckpointBlocked
		}
	}
	return nil
}
