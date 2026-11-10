package services

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *WorkflowWorkspaceCheckpoints) mutate(ctx context.Context, row models.WorkflowVisitCheckpoint, body func(*gorm.DB) error) error {
	return s.options.Execution.Transaction(ctx, s.db, checkpointEntities(row), func(tx *gorm.DB) error {
		if err := s.GuardTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		if err := body(tx); err != nil {
			return err
		}
		return s.GuardTx(tx.Statement.Context, tx, row)
	})
}

func checkpointFilesComplete(state string) bool {
	return state == "ready" || state == "skipped" || state == "not_created"
}

// FilesReady only accepts immutable ready metadata with the exact physical
// visit provenance. A currently mounted directory cannot stand in for history.
func (s *WorkflowWorkspaceCheckpoints) FilesReady(ctx context.Context, row models.WorkflowVisitCheckpoint) (bool, error) {
	if row.RouteState != "sealed" || row.OutcomeRef == "" {
		return false, ErrWorkflowCheckpointBlocked
	}
	if checkpointFilesComplete(row.FileState) {
		return true, nil
	}
	if row.FileState == "blocked" || row.FileState == "unknown" || row.FileState == "unavailable" {
		return false, ErrWorkflowCheckpointBlocked
	}
	if row.SnapshotID == "" {
		return false, nil
	}
	var snapshot struct {
		ID, State, ActorID, ScopeKind, ScopeID, RunID, NodeID, Lineage, SourceBindingFingerprint, ArchiveRef, ManifestRef, JobID, PublicationJobID string
		StateRunID, SourceSandboxID                                                                                                                uint
		Iteration, RunAttempt, ProviderAttempt                                                                                                     int
		VerifiedAt                                                                                                                                 *time.Time
	}
	err := s.db.WithContext(ctx).Table("workspace_snapshots").Select("id", "state", "actor_id", "scope_kind", "scope_id", "run_id", "node_id", "lineage", "source_binding_fingerprint", "archive_ref", "manifest_ref", "job_id", "publication_job_id", "state_run_id", "source_sandbox_id", "iteration", "run_attempt", "provider_attempt", "verified_at").Take(&snapshot, "id = ?", row.SnapshotID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if row.CaptureStartedAt != nil && time.Since(*row.CaptureStartedAt) > 10*time.Second {
			_ = s.block(ctx, row, "unknown", "capture_admission_unconfirmed")
			return false, ErrWorkflowCheckpointBlocked
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if snapshot.ActorID != row.ActorID || snapshot.ScopeKind != "run" || snapshot.ScopeID != row.RunID || snapshot.RunID != row.RunID || snapshot.StateRunID != row.StateRunID || snapshot.SourceSandboxID != row.SandboxID || snapshot.NodeID != row.NodeID || snapshot.Iteration != row.Iteration || snapshot.RunAttempt != row.RunAttempt || snapshot.ProviderAttempt != row.ProviderAttempt || snapshot.Lineage != row.Lineage || snapshot.SourceBindingFingerprint != row.BindingFingerprint {
		return false, ErrWorkspaceJobConflict
	}
	if snapshot.State == "ready" && snapshot.VerifiedAt != nil && snapshot.ArchiveRef != "" && snapshot.ManifestRef != "" {
		err := s.mutate(ctx, row, func(tx *gorm.DB) error {
			result := tx.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ? AND snapshot_id = ? AND route_state = ? AND file_state = ?", row.ID, row.SnapshotID, "sealed", "pending").Updates(map[string]any{"file_state": "ready", "reason": ""})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrWorkspaceJobConflict
			}
			return nil
		})
		return err == nil, err
	}
	var jobs []struct{ State string }
	if err := s.db.WithContext(ctx).Table("workspace_snapshot_jobs").Select("state").Where("id IN ?", []string{snapshot.JobID, snapshot.PublicationJobID}).Find(&jobs).Error; err != nil {
		return false, err
	}
	for _, job := range jobs {
		if job.State == "unknown" || job.State == "failed" || job.State == "cancelled" || job.State == "expired" {
			state := "blocked"
			if job.State == "unknown" {
				state = "unknown"
			}
			_ = s.block(ctx, row, state, "snapshot_"+job.State)
			return false, ErrWorkflowCheckpointBlocked
		}
	}
	return false, nil
}

func (s *WorkflowWorkspaceCheckpoints) block(ctx context.Context, row models.WorkflowVisitCheckpoint, state, reason string) error {
	return s.mutate(ctx, row, func(tx *gorm.DB) error {
		return tx.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ? AND route_state = ? AND file_state NOT IN ?", row.ID, "sealed", []string{"ready", "skipped", "not_created"}).Updates(map[string]any{"file_state": state, "reason": reason}).Error
	})
}

// RequestCapture records its dispatch boundary before calling the ordinary
// product capture facade. A crash/ambiguous result at that boundary only reads
// the planned original receipt; it never invents a replacement nonce.
func (s *WorkflowWorkspaceCheckpoints) RequestCapture(ctx context.Context, row models.WorkflowVisitCheckpoint) error {
	if row.RouteState != "sealed" || row.FileState != "pending" || row.ActorID == "" || row.SandboxID == 0 {
		return ErrWorkflowCheckpointBlocked
	}
	if !s.options.Enabled {
		return ErrWorkflowCheckpointBlocked
	} // Existing intents do not silently skip on config change.
	if err := s.options.ReadScope(ctx, s.db.WithContext(ctx), row.ActorID, "run", row.RunID); err != nil {
		return err
	}
	if row.CaptureStartedAt != nil {
		_, err := s.FilesReady(ctx, row)
		return err
	}
	binding, err := s.options.ResolveBinding(ctx, row.ActorID, "run", row.RunID, row.SandboxID)
	if err != nil {
		_ = s.block(ctx, row, "blocked", "binding_unavailable")
		return err
	}
	if binding.SandboxID != row.SandboxID || binding.Resource != row.Resource || binding.Lineage != row.Lineage || !restoreDigest(binding.Fingerprint) {
		return ErrWorkspaceJobConflict
	}
	duration := time.Duration(row.CaptureDurationMS) * time.Millisecond
	if duration <= 0 || duration > s.options.MaxDuration {
		return ErrWorkspaceJobInvalid
	}
	nonce := row.CaptureNonce
	if nonce == "" {
		nonce = "checkpoint-" + row.ID
	}
	snapshotID := captureRequestID(row.ActorID, "run", row.RunID, nonce)
	now := time.Now().UTC()
	err = s.mutate(ctx, row, func(tx *gorm.DB) error {
		var resource struct {
			RunID, NodeID, Name                    string
			StateRunID                             uint
			Iteration, RunAttempt, ProviderAttempt int
		}
		if err := tx.Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("name", "run_id", "node_id", "state_run_id", "iteration", "run_attempt", "provider_attempt").Where("id = ?", row.SandboxID).Take(&resource).Error; err != nil {
			return err
		}
		if resource.Name != row.Resource || resource.RunID != row.RunID || resource.NodeID != row.NodeID || resource.StateRunID != row.StateRunID || resource.Iteration != row.Iteration || resource.RunAttempt != row.RunAttempt || resource.ProviderAttempt != row.ProviderAttempt {
			return ErrWorkspaceJobConflict
		}
		var current models.WorkflowVisitCheckpoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current, "id = ?", row.ID).Error; err != nil {
			return err
		}
		if current.RouteState != "sealed" || current.FileState != "pending" || current.CaptureStartedAt != nil {
			return ErrWorkflowCheckpointBlocked
		}
		result := tx.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ? AND capture_started_at IS NULL", row.ID).Updates(map[string]any{"snapshot_id": snapshotID, "binding_fingerprint": binding.Fingerprint, "capture_nonce": nonce, "capture_started_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWorkspaceJobConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, err = s.options.Capture(ctx, row.ActorID, WorkspaceCaptureInput{ClientRequestID: nonce, ScopeKind: "run", ScopeID: row.RunID, SandboxID: row.SandboxID, Paths: []string{"."}, ExpectedBinding: binding.Fingerprint, Duration: duration})
	if err != nil {
		row.SnapshotID, row.BindingFingerprint = snapshotID, binding.Fingerprint
		var accepted int64
		if readErr := s.db.WithContext(ctx).Model(&models.WorkspaceSnapshot{}).Where("id = ? AND actor_id = ?", snapshotID, row.ActorID).Count(&accepted).Error; readErr != nil || accepted == 0 {
			_ = s.block(ctx, row, "unknown", "capture_admission_unconfirmed")
		}
	}
	return err
}

// retryableCapture is a capture whose job definitely ended without producing a
// snapshot, so a fresh capture under a new request id cannot duplicate one.
// Ambiguous (unknown) captures still require reconciling the original job.
func retryableCapture(row models.WorkflowVisitCheckpoint) bool {
	switch row.Reason {
	case "snapshot_failed", "snapshot_cancelled", "snapshot_expired":
		return row.RouteState == "sealed" && row.FileState == "blocked" && row.CaptureStartedAt != nil
	}
	return false
}

func nextCaptureNonce(row models.WorkflowVisitCheckpoint) string {
	base := "checkpoint-" + row.ID
	attempt := 1
	if rest, ok := strings.CutPrefix(row.CaptureNonce, base+"-r"); ok {
		if n, err := strconv.Atoi(rest); err == nil && n > 0 {
			attempt = n
		}
	}
	return base + "-r" + strconv.Itoa(attempt+1)
}

func rearmCaptureTx(tx *gorm.DB, row models.WorkflowVisitCheckpoint) (bool, error) {
	result := tx.Model(&models.WorkflowVisitCheckpoint{}).
		Where("id = ? AND route_state = ? AND file_state = ? AND capture_nonce = ?", row.ID, "sealed", "blocked", row.CaptureNonce).
		Updates(map[string]any{"file_state": "pending", "reason": "", "snapshot_id": "", "binding_fingerprint": "", "capture_nonce": nextCaptureNonce(row), "capture_started_at": nil, "next_probe_at": nil})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// RetryCaptureTx re-arms the run's checkpoint when its capture definitely
// failed, inside the caller's execution transaction. The engine's checkpoint
// loop then captures again and routes the run on; false means nothing to retry.
func (s *WorkflowWorkspaceCheckpoints) RetryCaptureTx(tx *gorm.DB, runID string) (bool, error) {
	var row models.WorkflowVisitCheckpoint
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND route_state = ? AND file_state = ?", runID, "sealed", "blocked").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil || !retryableCapture(row) {
		return false, err
	}
	return rearmCaptureTx(tx, row)
}

// Reconcile is explicitly requested. Native uncertainty only goes through the
// capture facade's GET of the exact original job; no new capture is submitted.
func (s *WorkflowWorkspaceCheckpoints) Reconcile(ctx context.Context, actor, id string) (models.WorkflowVisitCheckpoint, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return row, err
	}
	if err := s.options.ReadScope(ctx, s.db.WithContext(ctx), actor, "run", row.RunID); err != nil {
		return row, err
	}
	if row.ActorID != actor || row.SnapshotID == "" || row.CaptureStartedAt == nil || s.options.Reconcile == nil {
		return row, ErrWorkflowCheckpointBlocked
	}
	if retryableCapture(row) {
		if err := s.mutate(ctx, row, func(tx *gorm.DB) error {
			_, err := rearmCaptureTx(tx, row)
			return err
		}); err != nil {
			return row, err
		}
		return s.Get(ctx, id)
	}
	if _, err := s.options.Reconcile(ctx, actor, "run", row.RunID, row.SnapshotID); err != nil {
		return row, err
	}
	if err := s.mutate(ctx, row, func(tx *gorm.DB) error {
		return tx.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ? AND route_state = ? AND file_state IN ?", row.ID, "sealed", []string{"unknown", "blocked"}).Updates(map[string]any{"file_state": "pending", "reason": ""}).Error
	}); err != nil {
		return row, err
	}
	row, err = s.Get(ctx, id)
	if err != nil {
		return row, err
	}
	_, err = s.FilesReady(ctx, row)
	if err != nil {
		return row, err
	}
	return s.Get(ctx, id)
}
