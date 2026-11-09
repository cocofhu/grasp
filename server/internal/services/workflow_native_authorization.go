package services

import (
	"context"
	"encoding/json"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
)

// NativeAuthorization resolves current credentials before opening a SQL
// transaction. Reattachment must match the original current-grant digest; a
// saved environment token or a live handle never grants execution by itself.
func (a *WorkspaceAuthorization) NativeAuthorization(ctx context.Context, req runtime.NodeReq) (string, error) {
	if messageOutsideTransaction(a.db) != nil || req.StateRunID == 0 || req.RunID == "" {
		return "", ErrWorkspaceJobAuthority
	}
	var run struct {
		WorkspaceActorID, OriginSoloID string
		Attempt                        int
	}
	if err := a.db.WithContext(ctx).Table("runs").Select("workspace_actor_id", "origin_solo_id", "attempt").Take(&run, "id = ?", req.RunID).Error; err != nil {
		return "", err
	}
	actor := run.WorkspaceActorID
	if run.OriginSoloID != "" {
		var solo struct{ UserID string }
		if err := a.db.WithContext(ctx).Table("solo_sessions").Select("user_id").Take(&solo, "id = ?", run.OriginSoloID).Error; err != nil {
			return "", err
		}
		if actor == "" || actor != solo.UserID {
			return "", ErrWorkspaceJobAuthority
		}
	}
	if actor == "" || run.Attempt != req.RunAttempt {
		return "", ErrWorkspaceJobAuthority
	}
	scopes, err := a.readScope(ctx, a.db, actor, "run", req.RunID)
	if err != nil {
		return "", err
	}
	grant, err := a.prepareGrant(ctx, workspaceAuthorizationResource{Purpose: "run", RunID: req.RunID, Profile: models.AgentProfile(req.Config)})
	if err != nil {
		return "", err
	}
	// Workflow compute is independent of the originating Solo environment.
	// Its owner's permissions remain mandatory, but unrelated Solo restart,
	// chat model, or stopped state does not change this node's credential grant.
	type nativeScope struct {
		Kind, ID, Owner, OriginSolo, APIKeyDigest string
		RunAttempt                                int
	}
	currentScopes := make([]nativeScope, 0, len(scopes))
	for _, scope := range scopes {
		currentScopes = append(currentScopes, nativeScope{scope.Kind, scope.ID, scope.Owner, scope.OriginSolo, scope.APIKeyDigest, scope.RunAttempt})
	}
	digest := authorizationDigest(struct {
		Actor  string
		Scopes []nativeScope
		Grant  workspaceAuthorizationGrant
	}{actor, currentScopes, grant})
	if req.Operation.Authorization != "" && req.Operation.Authorization != digest {
		return "", ErrWorkspaceJobAuthority
	}
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityRun, ID: req.RunID}, {Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(req.StateRunID), 10)}}
	err = a.execution.Transaction(ctx, a.db, entities, func(tx *gorm.DB) error {
		for _, before := range scopes {
			after, err := a.lockScope(tx.Statement.Context, tx, actor, before)
			if err != nil {
				return err
			}
			if after != before {
				return ErrWorkspaceJobConflict
			}
		}
		if err := a.checkGrantMetadata(tx, grant); err != nil {
			return err
		}
		var state struct {
			RunID, NodeID, Status, ProviderOperationID, ProviderOperationReceipt, ProviderDispatchState, WorkspaceCheckpointID string
			Iteration, Attempt, ProviderAttempt                                                                                int
		}
		if err := tx.Table("state_runs").Clauses(clause.Locking{Strength: "UPDATE"}).Select("run_id", "node_id", "status", "provider_operation_id", "provider_operation_receipt", "provider_dispatch_state", "workspace_checkpoint_id", "iteration", "attempt", "provider_attempt").Take(&state, "id = ?", req.StateRunID).Error; err != nil {
			return err
		}
		if state.RunID != req.RunID || state.NodeID != req.NodeID || state.Iteration != req.Iteration || state.Attempt != req.RunAttempt || state.ProviderAttempt != req.ProviderAttempt || state.Status != "running" || state.WorkspaceCheckpointID != "" || state.ProviderOperationID != req.Operation.ID {
			return ErrWorkspaceJobConflict
		}
		if req.AttachOnly {
			var frozen sandbox.OperationIdentity
			if state.ProviderDispatchState != "dispatched" || json.Unmarshal([]byte(state.ProviderOperationReceipt), &frozen) != nil || frozen != req.Operation || !frozen.Valid() {
				return ErrWorkspaceJobConflict
			}
		} else if state.ProviderDispatchState != "preparing" && state.ProviderDispatchState != "dispatched" {
			return ErrWorkspaceJobConflict
		}
		if req.Operation.Resource == "" {
			return nil
		}
		if err := GuardWorkflowNativeTx(tx.Statement.Context, tx, req.RunID); err != nil {
			return err
		}
		var resource workspaceAuthorizationResource
		if err := tx.Table("sandboxes").Select("id", "name", "profile", "purpose", "run_id", "node_id", "state_run_id", "iteration", "run_attempt", "provider_attempt", "generation", "status", "desired_state").Take(&resource, "name = ?", req.Operation.Resource).Error; err != nil {
			return err
		}
		if resource.Purpose != "run" || resource.RunID != req.RunID || resource.NodeID != req.NodeID || resource.StateRunID != req.StateRunID || resource.Iteration != req.Iteration || resource.RunAttempt != req.RunAttempt || resource.ProviderAttempt != req.ProviderAttempt || resource.Status != "running" || resource.DesiredState == "stopped" || resource.Profile != models.AgentProfile(req.Config) {
			return ErrWorkspaceJobConflict
		}
		if req.AttachOnly && (resource.ID != req.Operation.SandboxID || resource.Generation != req.Operation.Generation) {
			return ErrWorkspaceJobConflict
		}
		return a.execution.GuardContextTx(tx.Statement.Context, tx, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(resource.ID), 10)})
	})
	return digest, err
}
