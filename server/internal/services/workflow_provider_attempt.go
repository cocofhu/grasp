package services

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Only the exact allocated provider attempt may attach a physical resource to
// a visit. A late native create/attach callback cannot overwrite a newer visit
// or stamp its first-ready time. All columns are SQL metadata.
func (s *SandboxService) recordExactWorkflowSandbox(ctx context.Context, info runtime.RunSandboxInfo, fields map[string]any, begin bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityRun, ID: info.RunID}, {Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(info.StateRunID), 10)}}
	return s.execution.Transaction(ctx, s.db, entities, func(tx *gorm.DB) error {
		var run struct {
			OriginSoloID, Status string
			Attempt              int
		}
		if err := tx.Table("runs").Clauses(clause.Locking{Strength: "UPDATE"}).Select("origin_solo_id", "status", "attempt").Take(&run, "id = ?", info.RunID).Error; err != nil {
			return err
		}
		if run.OriginSoloID != "" {
			if err := s.execution.GuardContextTx(tx.Statement.Context, tx, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID}); err != nil {
				return err
			}
		}
		if run.Attempt != info.RunAttempt || run.Status != "running" && run.Status != "waiting_human" {
			return ErrWorkspaceJobConflict
		}
		var state struct {
			RunID, NodeID, Status, WorkspaceCheckpointID string
			Iteration, Attempt, ProviderAttempt          int
		}
		if err := tx.Table("state_runs").Clauses(clause.Locking{Strength: "UPDATE"}).Select("run_id", "node_id", "status", "workspace_checkpoint_id", "iteration", "attempt", "provider_attempt").Where("id = ?", info.StateRunID).Take(&state).Error; err != nil {
			return err
		}
		if state.RunID != info.RunID || state.NodeID != info.NodeID || state.Iteration != info.Iteration || state.Attempt != info.RunAttempt || state.ProviderAttempt != info.ProviderAttempt || state.WorkspaceCheckpointID != "" || state.Status != "running" && state.Status != "waiting_human" {
			return ErrWorkspaceJobConflict
		}
		var existing struct {
			ID, StateRunID                         uint
			Name, RunID, NodeID                    string
			Iteration, RunAttempt, ProviderAttempt int
		}
		name := info.Name
		if !begin && info.CorrelationName != "" {
			name = info.CorrelationName
		}
		columns := []string{"id", "name", "run_id", "node_id", "state_run_id", "iteration", "run_attempt", "provider_attempt"}
		err := tx.Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select(columns).Take(&existing, "name = ?", name).Error
		if errors.Is(err, gorm.ErrRecordNotFound) && !begin && info.CorrelationName != "" {
			err = tx.Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select(columns).Take(&existing, "name = ?", info.Name).Error
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			if existing.RunID != info.RunID {
				return ErrWorkspaceJobConflict
			}
			if begin || info.CorrelationName != "" && existing.Name == info.CorrelationName {
				legacy := !begin && existing.StateRunID == 0 && existing.RunAttempt == 0 && existing.ProviderAttempt == 0
				if !legacy && (existing.StateRunID != info.StateRunID || existing.NodeID != info.NodeID || existing.Iteration != info.Iteration || existing.RunAttempt != info.RunAttempt || existing.ProviderAttempt != info.ProviderAttempt) {
					return ErrWorkspaceJobConflict
				}
			}
			if err := s.execution.GuardContextTx(tx.Statement.Context, tx, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(existing.ID), 10)}); err != nil {
				return err
			}
			if err := guardWorkflowWorkspaceReuseTx(tx.Statement.Context, tx, existing.ID, ""); err != nil {
				return err
			}
			if err := tx.Table("sandboxes").Where("id = ?", existing.ID).Updates(fields).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Table("sandboxes").Create(fields).Error; err != nil {
				return err
			}
		}
		if !begin && info.StartupError == "" {
			return tx.Table("state_runs").Where("id = ? AND provider_attempt = ? AND sandbox_ready_at IS NULL", info.StateRunID, info.ProviderAttempt).Update("sandbox_ready_at", fields["updated_at"]).Error
		}
		return nil
	})
}
