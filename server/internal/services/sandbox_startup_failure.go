package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// sandboxStartupIdentity is captured under the physical startup lease. A late
// failure may retain attribution, but cannot overwrite a new operation, a
// renamed resource, or an execution whose parent provenance has changed.
type sandboxStartupIdentity struct{ row models.Sandbox }

func (s *SandboxService) beginSandboxStartup(ctx context.Context, id uint, name string) (context.Context, sandboxStartupIdentity, func(), error) {
	operation, release, err := s.acquireSandboxOperation(ctx, id, "startup")
	if err != nil {
		return nil, sandboxStartupIdentity{}, nil, err
	}
	var row models.Sandbox
	err = s.db.WithContext(operation).First(&row, id).Error
	if err == nil && (row.Name != name || (row.Status != "creating" && row.Status != "pulling") || row.DesiredState == "stopped" || row.DesiredState == "released") {
		err = errSandboxOperationLost
	}
	if err != nil {
		release()
		return nil, sandboxStartupIdentity{}, nil, err
	}
	return operation, sandboxStartupIdentity{row}, release, nil
}

func (i sandboxStartupIdentity) current(tx *gorm.DB) *gorm.DB {
	r := i.row
	return tx.Model(&models.Sandbox{}).Where("id = ? AND name = ? AND generation = ? AND purpose = ? AND COALESCE(run_id, '') = ? AND COALESCE(thread_id, '') = ? AND COALESCE(state_run_id, 0) = ? AND COALESCE(run_attempt, 0) = ? AND COALESCE(provider_attempt, 0) = ?", r.ID, r.Name, r.Generation, r.Purpose, r.RunID, r.ThreadID, r.StateRunID, r.RunAttempt, r.ProviderAttempt)
}

func startupHandleValues(sb *sandbox.Sandbox, home string) map[string]any {
	values := map[string]any{"destroy_at": nil, "updated_at": time.Now().UTC()}
	if home != "" {
		values["home_dir"] = home
	}
	if sb != nil && sb.Name != "" {
		values["name"] = sb.Name
		values["host"] = sb.Host
		values["acp_port"] = sb.Port
		values["code_server_port"] = sb.CodeServerPort
	}
	return values
}

// adoptSandboxStartupHandle runs before ACP readiness. A gateway Create can
// return a real handle together with an error; its ID must survive that error.
func (s *SandboxService) adoptSandboxStartupHandle(ctx context.Context, identity *sandboxStartupIdentity, sb *sandbox.Sandbox, home string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sb == nil || sb.Name == "" {
		return errors.New("created sandbox has no provider identity")
	}
	err := s.commitSandboxOperation(ctx, identity.row.ID, func(tx *gorm.DB) error {
		result := identity.current(tx).Where("status IN ? AND COALESCE(desired_state, '') NOT IN ?", []string{"creating", "pulling"}, []string{"stopped", "released"}).Updates(startupHandleValues(sb, home))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSandboxOperationLost
		}
		return nil
	})
	if err == nil {
		identity.row.Name = sb.Name
		identity.row.HomeDir = home
	}
	return err
}

// retainSandboxStartupFailure uses a new bounded metadata context after timeout
// or cancellation. It never renews an expired owner, calls a provider, marks
// running/stopped, removes a home, or clears another generation's binding.
// Fresh execution-lineage authorization and the exact original CAS still apply.
func (s *SandboxService) retainSandboxStartupFailure(ctx context.Context, identity sandboxStartupIdentity, sb *sandbox.Sandbox, home string, failure error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.executionTransaction(cleanup, identity.row.ID, func(tx *gorm.DB) error {
		values := startupHandleValues(sb, home)
		values["status"] = "error"
		values["error"] = "环境启动未确认，已保留原工作区，请核对原环境状态：" + truncErr(failure)
		result := identity.current(tx).Where("status IN ? AND COALESCE(desired_state, '') NOT IN ?", []string{"creating", "pulling", "error"}, []string{"stopped", "released"}).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSandboxOperationLost
		}
		return nil
	})
}

// Retained startup failures are not idle-delete authorization, even when an
// older process wrote a deadline. Observation preserves physical attribution.
func (s *SandboxService) retainSandboxObservation(ctx context.Context, row *models.Sandbox, status, reason string) error {
	identity := sandboxStartupIdentity{*row}
	return s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error {
		result := identity.current(tx).Where("status = ? AND COALESCE(desired_state, '') <> ?", row.Status, "released").Updates(map[string]any{"status": status, "error": reason, "destroy_at": nil, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSandboxOperationLost
		}
		return nil
	})
}

func (s *SandboxService) publishSandboxStartup(ctx context.Context, identity sandboxStartupIdentity, values map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.commitSandboxOperation(ctx, identity.row.ID, func(tx *gorm.DB) error {
		result := identity.current(tx).Where("status IN ? AND COALESCE(desired_state, '') NOT IN ?", []string{"creating", "pulling"}, []string{"stopped", "released"}).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: sandbox startup record changed", errSandboxOperationLost)
		}
		return nil
	})
}

// A unique gateway correlation match can recover the original Create result
// after its response was lost. This only records identity; it never creates,
// attaches, starts, stops, or deletes a provider instance.
func (s *SandboxService) retainCorrelatedStartupHandle(ctx context.Context, row *models.Sandbox, providerID string) error {
	if providerID == "" || providerID == row.Name {
		return nil
	}
	identity := sandboxStartupIdentity{*row}
	return s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error {
		result := identity.current(tx).Where("status IN ? AND (operation_owner = '' OR operation_owner IS NULL OR operation_expires_at <= ?)", []string{"creating", "pulling", "error"}, time.Now().UTC()).Updates(map[string]any{"name": providerID, "status": "error", "error": "先前启动未确认，已找回原环境，保留工作区等待手动核对", "destroy_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSandboxOperationLost
		}
		return nil
	})
}

// Recheck the current row under the same lease used by Destroy. A stale
// sweeper scan of a formerly running row must not delete a later error receipt.
func (s *SandboxService) sweepNonresidentCandidate(ctx context.Context, observed models.Sandbox) error {
	ctx, release, err := s.acquireSandboxOperation(ctx, observed.ID, "idle_retention")
	if err != nil {
		return err
	}
	defer release()
	current, err := s.Get(observed.ID)
	if err != nil {
		return err
	}
	if current.Name != observed.Name || current.Generation != observed.Generation+1 {
		return errSandboxOperationLost
	}
	if current.Status != "running" || current.DesiredState == "stopped" || current.DesiredState == "released" {
		return s.commitSandboxOperation(ctx, current.ID, func(tx *gorm.DB) error {
			return s.updateSandboxOperation(ctx, tx, current.ID, map[string]any{"destroy_at": nil})
		})
	}
	if current.DestroyAt == nil || current.DestroyAt.After(time.Now()) || s.isBusyRow(current) {
		return nil
	}
	return s.Destroy(ctx, current.ID)
}

func (s *SandboxService) reconcileNonresidentRunning(ctx context.Context, observed models.Sandbox) error {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	operation, release, err := s.acquireSandboxOperation(bounded, observed.ID, "startup_reconcile")
	if err != nil {
		return err
	}
	defer release()
	current, err := s.Get(observed.ID)
	if err != nil {
		return err
	}
	if current.Name != observed.Name || current.Generation != observed.Generation+1 || current.Status != "running" || current.DesiredState == "stopped" || current.DesiredState == "released" {
		return errSandboxOperationLost
	}
	sb, err := s.mgr.Attach(operation, current.Name)
	if err != nil {
		cleanup, done := context.WithTimeout(context.WithoutCancel(operation), 5*time.Second)
		defer done()
		return errors.Join(err, s.retainSandboxObservation(cleanup, current, "error", "环境连接未确认，已保留原工作区："+truncErr(err)))
	}
	values := map[string]any{"host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort}
	if current.DestroyAt == nil {
		at := time.Now().Add(s.TTL())
		values["destroy_at"] = &at
	}
	return s.commitSandboxOperation(operation, current.ID, func(tx *gorm.DB) error { return s.updateSandboxOperation(operation, tx, current.ID, values) })
}
