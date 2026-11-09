package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WorkflowCheckpointAPI interface {
	WorkspaceCheckpoints(context.Context, string, string) ([]models.WorkflowVisitCheckpoint, error)
	ReconcileWorkspaceCheckpoint(context.Context, string, string, string) (models.WorkflowVisitCheckpoint, error)
}

type workflowCheckpointView struct {
	ID              string    `json:"id"`
	RunID           string    `json:"runId"`
	StateRunID      uint      `json:"stateRunId"`
	NodeID          string    `json:"nodeId"`
	Iteration       int       `json:"iteration"`
	RunAttempt      int       `json:"runAttempt"`
	ProviderAttempt int       `json:"providerAttempt"`
	SnapshotID      string    `json:"snapshotId,omitempty"`
	FileState       string    `json:"fileState"`
	RouteState      string    `json:"routeState"`
	Reason          string    `json:"reason,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func checkpointView(row models.WorkflowVisitCheckpoint) workflowCheckpointView {
	reason := ""
	switch row.Reason {
	case "workspace_operations_disabled", "workspace_source_unavailable", "workspace_actor_unavailable", "binding_unavailable", "capture_admission_unconfirmed", "snapshot_unknown", "snapshot_failed", "snapshot_cancelled", "snapshot_expired", "outcome_sealing_failed", "provider_outcome_unconfirmed":
		reason = row.Reason
	}
	return workflowCheckpointView{ID: row.ID, RunID: row.RunID, StateRunID: row.StateRunID, NodeID: row.NodeID, Iteration: row.Iteration, RunAttempt: row.RunAttempt, ProviderAttempt: row.ProviderAttempt, SnapshotID: row.SnapshotID, FileState: row.FileState, RouteState: row.RouteState, Reason: reason, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (h *Handlers) checkpointReady(c *gin.Context) (string, bool) {
	if c.GetString(soloEmbedUserKey) != "" {
		c.JSON(http.StatusForbidden, gin.H{"code": "workspace_checkpoint_forbidden"})
		return "", false
	}
	actor, ok := h.sessionUser(c)
	if !ok {
		return "", false
	}
	if h.WorkspaceCheckpoints == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"code": "workspace_checkpoint_unsupported"})
		return "", false
	}
	return actor, true
}

func checkpointError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "workspace_checkpoint_unavailable"
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "workspace_checkpoint_not_found"
	case errors.Is(err, services.ErrWorkspaceJobUnsupported):
		status, code = http.StatusNotImplemented, "workspace_checkpoint_unsupported"
	case errors.Is(err, services.ErrWorkflowCheckpointBlocked):
		status, code = http.StatusConflict, "workspace_checkpoint_reconcile_required"
	}
	c.JSON(status, gin.H{"code": code})
}

func (h *Handlers) ListWorkflowWorkspaceCheckpoints(c *gin.Context) {
	actor, ok := h.checkpointReady(c)
	if !ok {
		return
	}
	rows, err := h.WorkspaceCheckpoints.WorkspaceCheckpoints(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		checkpointError(c, err)
		return
	}
	views := make([]workflowCheckpointView, 0, len(rows))
	for _, row := range rows {
		views = append(views, checkpointView(row))
	}
	c.JSON(http.StatusOK, gin.H{"items": views})
}

func (h *Handlers) ReconcileWorkflowWorkspaceCheckpoint(c *gin.Context) {
	actor, ok := h.checkpointReady(c)
	if !ok {
		return
	}
	row, err := h.WorkspaceCheckpoints.ReconcileWorkspaceCheckpoint(c.Request.Context(), actor, c.Param("id"), c.Param("checkpointId"))
	if err != nil {
		checkpointError(c, err)
		return
	}
	c.JSON(http.StatusOK, checkpointView(row))
}
