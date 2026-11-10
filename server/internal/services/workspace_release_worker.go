package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/leasewatch"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func (s *WorkspaceReleaseService) providerFence(ctx context.Context, row models.WorkspaceRelease) (sandbox.EnvironmentReleaseRenew, error) {
	held, ok := s.sbx.heldSandboxOperation(ctx, row.SandboxID)
	if !ok {
		return sandbox.EnvironmentReleaseRenew{}, errSandboxOperationLost
	}
	if err := ctx.Err(); err != nil {
		return sandbox.EnvironmentReleaseRenew{}, err
	}
	var source models.Sandbox
	if err := s.db.WithContext(ctx).Select("operation_owner", "generation", "operation_expires_at").Take(&source, row.SandboxID).Error; err != nil {
		return sandbox.EnvironmentReleaseRenew{}, err
	}
	now := time.Now().UTC()
	if source.OperationOwner != held.Owner || source.Generation != held.Generation || source.OperationExpiresAt == nil || !source.OperationExpiresAt.After(now) || !row.Deadline.After(now) {
		return sandbox.EnvironmentReleaseRenew{}, errSandboxOperationLost
	}
	until := *source.OperationExpiresAt
	if row.Deadline.Before(until) {
		until = row.Deadline
	}
	return sandbox.EnvironmentReleaseRenew{Owner: held.Owner, Generation: held.Generation, ExpiresAt: until}, nil
}
func (s *WorkspaceReleaseService) uncertain(ctx context.Context, row models.WorkspaceRelease, reason string) {
	prepared, err := s.options.Prepare(ctx)
	if err != nil {
		return
	}
	_ = s.sbx.commitSandboxOperationPrepared(prepared, row.SandboxID, func(tx *gorm.DB) error {
		if err := s.options.GuardTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		_, err := s.prepareScope(tx.Statement.Context, tx, row, true)
		return err
	}, func(tx *gorm.DB) error {
		return tx.Model(&models.WorkspaceRelease{}).Where("id = ? AND state = 'dispatching' AND confirm_nonce = ? AND dispatched_at IS NOT NULL", row.ID, row.ConfirmNonce).Updates(map[string]any{"state": "unknown", "error": reason}).Error
	})
}
func (s *WorkspaceReleaseService) completion(ctx context.Context, row models.WorkspaceRelease, view sandbox.EnvironmentReleaseView) error {
	var frozen sandbox.EnvironmentReleaseView
	if json.Unmarshal([]byte(row.ProviderPlan), &frozen) != nil || !validProviderReleasePlan(frozen, row.ID) || !validProviderReleasePlan(view, row.ID) || view.State != "released" || view.Receipt == nil {
		return ErrWorkspaceReleaseUnknown
	}
	receipt := view.Receipt
	objects, _ := json.Marshal(receipt.Objects)
	expected, _ := json.Marshal(frozen.Plan.Objects)
	if view.Plan.SHA256 != row.ProviderPlanSHA256 || receipt.ID != row.ID || receipt.PlanSHA256 != row.ProviderPlanSHA256 || receipt.Resource != frozen.Plan.Resource || receipt.Lineage != row.Lineage || receipt.VerifiedAt.IsZero() || string(objects) != string(expected) {
		return ErrWorkspaceReleaseConflict
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	// The object-backed result is prepared before taking any publication lock.
	notice, err := prepareSoloMessage(ctx, s.db, models.SoloMessage{ID: "workspace-release-" + row.ID, SessionID: row.SoloID, Role: "system", Content: "环境已由你确认释放，聊天与已保存的文件快照保留。原环境内尚未保存的文件和进程状态无法恢复；继续执行将创建新的计算环境。", CreatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	prepared, err := s.options.Prepare(ctx)
	if err != nil {
		return err
	}
	err = s.sbx.commitSandboxOperationPrepared(prepared, row.SandboxID, func(tx *gorm.DB) error {
		if err := s.options.GuardTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		scope, err := s.prepareScope(tx.Statement.Context, tx, row, true)
		if err != nil {
			return err
		}
		encoded, err := releaseScopeJSON(scope)
		if err != nil || encoded != row.ScopeJSON || scope.Bindings.Fingerprint != row.BindingsFingerprint {
			return ErrWorkspaceReleaseConflict
		}
		return nil
	}, func(tx *gorm.DB) error {
		if err := s.physical(tx, row); err != nil {
			return err
		}
		accepted := tx.Model(&models.WorkspaceRelease{}).Where("id = ? AND actor_id = ? AND solo_id = ? AND sandbox_id = ? AND confirm_nonce = ? AND dispatched_at IS NOT NULL AND state IN ?", row.ID, row.ActorID, row.SoloID, row.SandboxID, row.ConfirmNonce, []string{"dispatching", "unknown"}).Updates(map[string]any{"state": "released", "provider_receipt": string(body), "error": ""})
		if accepted.Error != nil {
			return accepted.Error
		}
		if accepted.RowsAffected != 1 {
			return ErrWorkspaceReleaseConflict
		}
		if err := s.options.Bindings.ReleasedTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		parent := tx.Model(&models.SoloSession{}).Where("id = ? AND user_id = ? AND sandbox_id = ? AND status = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = ?", row.SoloID, row.ActorID, row.SandboxID, models.SoloStatusClosed, row.LifecycleEpoch, row.LifecycleAttempt, workspaceReleaseAction).Updates(map[string]any{"sandbox_id": 0, "lifecycle_source_sandbox_id": 0, "lifecycle_action": "", "error": "", "conversation_context_pending": true, "workflow_context_version": gorm.Expr("workflow_context_version + 1")})
		if parent.Error != nil {
			return parent.Error
		}
		if parent.RowsAffected != 1 {
			return ErrWorkspaceReleaseConflict
		}
		if err := s.sbx.updateSandboxOperation(ctx, tx, row.SandboxID, map[string]any{"status": "released", "desired_state": "released", "destroy_at": nil, "error": "", "host": "", "acp_port": 0, "code_server_port": 0, "token": ""}); err != nil {
			return err
		}
		if _, err := insertPreparedSoloMessageTx(tx, notice); err != nil {
			return err
		}
		return s.options.GuardTx(tx.Statement.Context, tx, row)
	})
	if err != nil {
		return err
	}
	s.sbx.teardownLive(row.SandboxID)
	s.solo.publishStatus(row.SoloID)
	return nil
}

// RunOne is application-owned, not an HTTP request executor. Its single
// dispatch marker survives crashes and binds the user's exact confirmed loss.
func (s *WorkspaceReleaseService) RunOne(ctx context.Context, id string) error {
	if err := s.beginRelease(); err != nil {
		return err
	}
	defer s.finishRelease()
	var row models.WorkspaceRelease
	if err := s.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
		return err
	}
	if row.State != "confirmed" || row.DispatchedAt != nil {
		return ErrWorkspaceReleaseUnknown
	}
	if row.Lineage != s.options.Lineage || !row.Deadline.After(time.Now()) {
		return ErrWorkspaceReleaseConflict
	}
	ctx, cancel := context.WithDeadline(s.privateContext(ctx, row), row.Deadline)
	defer cancel()
	operation, release, err := s.sbx.acquireSandboxOperation(ctx, row.SandboxID, "environment_release")
	if err != nil {
		return err
	}
	defer release()
	prepared, err := s.options.Prepare(operation)
	if err != nil {
		return err
	}
	err = s.sbx.commitSandboxOperationPrepared(prepared, row.SandboxID, func(tx *gorm.DB) error {
		if err := s.options.GuardTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		scope, err := s.prepareScope(tx.Statement.Context, tx, row, true)
		if err != nil {
			return err
		}
		encoded, err := releaseScopeJSON(scope)
		if err != nil || encoded != row.ScopeJSON {
			return ErrWorkspaceReleaseConflict
		}
		return nil
	}, func(tx *gorm.DB) error {
		if err := s.physical(tx, row); err != nil {
			return err
		}
		now := time.Now().UTC()
		changed := tx.Model(&models.WorkspaceRelease{}).Where("id = ? AND state = 'confirmed' AND confirm_nonce = ? AND dispatched_at IS NULL AND deadline > ?", row.ID, row.ConfirmNonce, now).Updates(map[string]any{"state": "dispatching", "dispatched_at": now})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrWorkspaceReleaseUnknown
		}
		row.State, row.DispatchedAt = "dispatching", &now
		return nil
	})
	if err != nil {
		return err
	}
	fence, err := s.providerFence(operation, row)
	if err != nil {
		s.uncertain(operation, row, "release_owner_lost")
		return err
	}
	remote, err := s.options.Provider.Confirm(operation, row.Resource, row.ID, sandbox.EnvironmentReleaseConfirm{PlanSHA256: row.ProviderPlanSHA256, Nonce: row.ConfirmNonce, Confirmed: true, LossAccepted: true, Owner: fence.Owner, Generation: fence.Generation, ExpiresAt: fence.ExpiresAt, Deadline: row.Deadline})
	if err != nil {
		s.uncertain(operation, row, "provider_release_unconfirmed")
		return ErrWorkspaceReleaseUnknown
	}
	// The gateway's last acknowledged expiry bounds every retry. Losing a
	// renewal response never permits a worker to continue past that deadline.
	remoteWork, stopRemote := context.WithCancelCause(operation)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		known := fence.ExpiresAt
		err := leasewatch.Maintain(remoteWork, known, sandboxOperationLeaseTTL/3, ErrWorkspaceReleaseUnknown, func(check context.Context) (time.Time, error) {
			next, err := s.providerFence(check, row)
			if err != nil {
				if errors.Is(err, errSandboxOperationLost) {
					return time.Time{}, ErrWorkspaceReleaseUnknown
				}
				return time.Time{}, err
			}
			if !next.ExpiresAt.After(known) {
				return time.Time{}, errors.New("physical lease renewal not acknowledged yet")
			}
			view, err := s.options.Provider.Renew(check, row.Resource, row.ID, next)
			if err != nil {
				if releaseProviderRejected(err) {
					return time.Time{}, fmt.Errorf("%w: remote fence rejected", ErrWorkspaceReleaseUnknown)
				}
				return time.Time{}, err
			}
			if !validProviderReleasePlan(view, row.ID) || view.Plan.SHA256 != row.ProviderPlanSHA256 || (view.State != "confirmed" && view.State != "dispatching" && view.State != "observing" && view.State != "released") {
				return time.Time{}, ErrWorkspaceReleaseUnknown
			}
			known = next.ExpiresAt
			return known, nil
		})
		stopRemote(err)
	}()
	defer func() { stopRemote(context.Canceled); <-watchDone }()
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		if !validProviderReleasePlan(remote, row.ID) || remote.Plan.SHA256 != row.ProviderPlanSHA256 {
			s.uncertain(operation, row, "provider_receipt_mismatch")
			return ErrWorkspaceReleaseConflict
		}
		if remote.State == "released" {
			if err := s.completion(remoteWork, row, remote); err != nil {
				s.uncertain(operation, row, "release_publication_unconfirmed")
				return err
			}
			return nil
		}
		if remote.State == "unknown" {
			s.uncertain(operation, row, "provider_release_unconfirmed")
			return ErrWorkspaceReleaseUnknown
		}
		select {
		case <-remoteWork.Done():
			s.uncertain(operation, row, "release_owner_lost")
			return context.Cause(remoteWork)
		case <-ticker.C:
		}
		observed, err := s.options.Provider.Get(remoteWork, row.Resource, row.ID)
		if err != nil {
			if releaseProviderRejected(err) {
				s.uncertain(operation, row, "provider_receipt_unavailable")
				return ErrWorkspaceReleaseUnknown
			}
			// Transient reads retry only while the acknowledged remote lease
			// remains valid. They never dispatch or extend that lease themselves.
			continue
		}
		remote = observed
	}
}

// Reconcile is an explicit GET-only provider observation. It never calls
// Confirm, Renew or Delete, even after a lost acknowledgement or process crash.
func (s *WorkspaceReleaseService) Reconcile(ctx context.Context, actor, solo, id string) (WorkspaceReleaseView, error) {
	if err := s.beginRelease(); err != nil {
		return WorkspaceReleaseView{}, err
	}
	defer s.finishRelease()
	row, err := s.load(ctx, actor, solo, id)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	if row.State == "released" {
		return workspaceReleaseView(row)
	}
	if row.DispatchedAt == nil {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseUnknown
	}
	if row.Lineage != s.options.Lineage {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseConflict
	}
	operation, release, err := s.sbx.acquireSandboxOperation(s.privateContext(ctx, row), row.SandboxID, "release_reconcile")
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	defer release()
	remote, err := s.options.Provider.Get(operation, row.Resource, row.ID)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	if remote.State != "released" {
		return workspaceReleaseView(row)
	}
	if err := s.completion(operation, row, remote); err != nil {
		return WorkspaceReleaseView{}, err
	}
	return s.Get(ctx, actor, solo, id)
}
func (s *WorkspaceReleaseService) Run(ctx context.Context) {
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil || s.releasePaused() {
			return
		}
		var rows []models.WorkspaceRelease
		if s.db.WithContext(ctx).Select("id").Where("state = 'confirmed' AND dispatched_at IS NULL AND lineage = ? AND deadline > ?", s.options.Lineage, time.Now().UTC()).Order("created_at,id").Limit(16).Find(&rows).Error == nil {
			for _, row := range rows {
				if ctx.Err() != nil {
					return
				}
				_ = s.RunOne(ctx, row.ID)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

func releaseProviderRejected(err error) bool {
	var status *sandbox.GatewayHTTPError
	return errors.Is(err, ErrWorkspaceReleaseUnknown) || errors.Is(err, ErrWorkspaceReleaseConflict) || errors.Is(err, ErrWorkspaceReleaseUnsupported) || (errors.As(err, &status) && status.StatusCode >= 400 && status.StatusCode < 500 && status.StatusCode != 429)
}
