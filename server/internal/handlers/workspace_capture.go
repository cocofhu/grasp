package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WorkspaceCaptureAPI interface {
	Admit(context.Context, string, services.WorkspaceCaptureInput) (services.WorkspaceSnapshotMetadata, error)
	Reconcile(context.Context, string, string, string, string) (services.WorkspaceSnapshotMetadata, error)
	Resolve(context.Context, string, string, string, string) (services.WorkspaceSnapshotMetadata, error)
}

func workspaceCaptureError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "workspace_snapshot_unavailable"
	switch {
	case errors.Is(err, services.ErrWorkspaceJobInvalid):
		status, code = http.StatusBadRequest, "invalid_workspace_snapshot_request"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "workspace_snapshot_not_found"
	case errors.Is(err, services.ErrWorkspaceJobUnsupported):
		status, code = http.StatusNotImplemented, "workspace_snapshot_unsupported"
	case errors.Is(err, services.ErrWorkspaceJobConflict):
		status, code = http.StatusConflict, "workspace_snapshot_identity_conflict"
	case errors.Is(err, services.ErrWorkspaceJobOwner), errors.Is(err, services.ErrWorkspaceJobUnknown):
		status, code = http.StatusConflict, "workspace_snapshot_reconcile_required"
	}
	c.JSON(status, gin.H{"code": code})
}

func (h *Handlers) workspaceCaptureReady(c *gin.Context) (string, bool) {
	actor, ok := h.snapshotHistoryReady(c)
	if !ok {
		return "", false
	}
	if h.WorkspaceCapture == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_snapshot_unavailable"})
		return "", false
	}
	return actor, true
}

func (h *Handlers) admitWorkspaceCapture(c *gin.Context, kind string) {
	actor, ok := h.workspaceCaptureReady(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workspaceSnapshotBody)
	var input struct {
		ClientRequestID string   `json:"clientRequestId"`
		SandboxID       uint     `json:"sandboxId"`
		ExpectedBinding string   `json:"expectedBinding"`
		Paths           []string `json:"paths"`
		IncludeIgnored  []string `json:"includeIgnored"`
		DurationMillis  int64    `json:"durationMillis"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF || input.DurationMillis <= 0 || input.DurationMillis > (24*time.Hour).Milliseconds() {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace_snapshot_request"})
		return
	}
	row, err := h.WorkspaceCapture.Admit(c.Request.Context(), actor, services.WorkspaceCaptureInput{ClientRequestID: input.ClientRequestID, ScopeKind: kind, ScopeID: c.Param("id"), SandboxID: input.SandboxID, ExpectedBinding: input.ExpectedBinding, Paths: input.Paths, IncludeIgnored: input.IncludeIgnored, Duration: time.Duration(input.DurationMillis) * time.Millisecond})
	if err != nil {
		workspaceCaptureError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, row)
}

func (h *Handlers) reconcileWorkspaceCapture(c *gin.Context, kind string) {
	actor, ok := h.workspaceCaptureReady(c)
	if !ok {
		return
	}
	row, err := h.WorkspaceCapture.Reconcile(c.Request.Context(), actor, kind, c.Param("id"), c.Param("snapshotId"))
	if err != nil {
		workspaceCaptureError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *Handlers) AdmitSoloWorkspaceSnapshot(c *gin.Context) { h.admitWorkspaceCapture(c, "solo") }
func (h *Handlers) AdmitRunWorkspaceSnapshot(c *gin.Context)  { h.admitWorkspaceCapture(c, "run") }
func (h *Handlers) ReconcileSoloWorkspaceSnapshot(c *gin.Context) {
	h.reconcileWorkspaceCapture(c, "solo")
}
func (h *Handlers) ReconcileRunWorkspaceSnapshot(c *gin.Context) {
	h.reconcileWorkspaceCapture(c, "run")
}

func (h *Handlers) resolveWorkspaceCapture(c *gin.Context, kind string) {
	actor, ok := h.workspaceCaptureReady(c)
	if !ok {
		return
	}
	row, err := h.WorkspaceCapture.Resolve(c.Request.Context(), actor, kind, c.Param("id"), c.Param("requestId"))
	if err != nil {
		workspaceCaptureError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *Handlers) ResolveSoloWorkspaceSnapshot(c *gin.Context) { h.resolveWorkspaceCapture(c, "solo") }
func (h *Handlers) ResolveRunWorkspaceSnapshot(c *gin.Context)  { h.resolveWorkspaceCapture(c, "run") }
