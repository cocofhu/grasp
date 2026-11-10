package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WorkspaceRestoreAPI interface {
	Admit(context.Context, string, services.WorkspaceRestoreInput) (models.WorkspaceRestore, error)
	Get(context.Context, string, string) (models.WorkspaceRestore, error)
	Reconcile(context.Context, string, string) (models.WorkspaceRestore, error)
	Resolve(context.Context, string, string, string) (models.WorkspaceRestore, error)
	List(context.Context, string, string, string, int) (services.WorkspaceRestoreHistoryPage, error)
}

type workspaceRestoreView struct {
	ID         string    `json:"id"`
	SnapshotID string    `json:"snapshotId"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func restoreView(row models.WorkspaceRestore) workspaceRestoreView {
	return workspaceRestoreView{ID: row.ID, SnapshotID: row.SnapshotID, State: row.State, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (h *Handlers) workspaceRestoreReady(c *gin.Context) (string, bool) {
	actor, ok := h.snapshotHistoryReady(c)
	if !ok {
		return "", false
	}
	if h.WorkspaceRestore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_restore_unavailable"})
		return "", false
	}
	if err := h.WorkspaceSnapshots.AuthorizeScope(c.Request.Context(), actor, "solo", c.Param("id")); err != nil {
		snapshotHistoryError(c, err)
		return "", false
	}
	return actor, true
}

func workspaceRestoreError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "workspace_restore_unavailable"
	switch {
	case errors.Is(err, services.ErrWorkspaceJobInvalid):
		status, code = http.StatusBadRequest, "invalid_workspace_restore"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "workspace_restore_not_found"
	case errors.Is(err, services.ErrWorkspaceJobUnsupported):
		status, code = http.StatusNotImplemented, "workspace_restore_unsupported"
	case errors.Is(err, services.ErrWorkspaceJobConflict):
		status, code = http.StatusConflict, "workspace_restore_identity_conflict"
	case errors.Is(err, services.ErrWorkspaceJobOwner), errors.Is(err, services.ErrWorkspaceJobUnknown):
		status, code = http.StatusConflict, "workspace_restore_reconcile_required"
	}
	c.JSON(status, gin.H{"code": code})
}

// AdmitWorkspaceRestore records one immutable intent. The application worker
// owns execution; disconnecting this request cannot trigger another unpack.
func (h *Handlers) AdmitWorkspaceRestore(c *gin.Context) {
	actor, ok := h.workspaceRestoreReady(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var input struct {
		ClientRequestID string `json:"clientRequestId"`
		SnapshotID      string `json:"snapshotId"`
		ExpectedBinding string `json:"expectedBinding"`
		DurationMillis  int64  `json:"durationMillis"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF || input.DurationMillis <= 0 || input.DurationMillis > (24*time.Hour).Milliseconds() {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace_restore"})
		return
	}
	row, err := h.WorkspaceRestore.Admit(c.Request.Context(), actor, services.WorkspaceRestoreInput{ClientRequestID: input.ClientRequestID, SoloID: c.Param("id"), SnapshotID: input.SnapshotID, ExpectedBinding: input.ExpectedBinding, Duration: time.Duration(input.DurationMillis) * time.Millisecond})
	if err != nil {
		workspaceRestoreError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, restoreView(row))
}

func (h *Handlers) readWorkspaceRestore(c *gin.Context, reconcile bool) {
	actor, ok := h.workspaceRestoreReady(c)
	if !ok {
		return
	}
	row, err := h.WorkspaceRestore.Get(c.Request.Context(), actor, c.Param("restoreId"))
	if err == nil && row.SoloID != c.Param("id") {
		err = gorm.ErrRecordNotFound
	}
	if err != nil {
		workspaceRestoreError(c, err)
		return
	}
	if reconcile {
		row, err = h.WorkspaceRestore.Reconcile(c.Request.Context(), actor, row.ID)
		if err != nil {
			workspaceRestoreError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, restoreView(row))
}

func (h *Handlers) GetWorkspaceRestore(c *gin.Context)       { h.readWorkspaceRestore(c, false) }
func (h *Handlers) ReconcileWorkspaceRestore(c *gin.Context) { h.readWorkspaceRestore(c, true) }

func (h *Handlers) ResolveWorkspaceRestore(c *gin.Context) {
	actor, ok := h.workspaceRestoreReady(c)
	if !ok {
		return
	}
	row, err := h.WorkspaceRestore.Resolve(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"))
	if err != nil {
		workspaceRestoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, restoreView(row))
}

func (h *Handlers) ListWorkspaceRestores(c *gin.Context) {
	actor, ok := h.workspaceRestoreReady(c)
	if !ok {
		return
	}
	params, ok := parseCursorPagination(c)
	if !ok {
		return
	}
	page, err := h.WorkspaceRestore.List(c.Request.Context(), actor, c.Param("id"), params.Cursor, params.Limit)
	if err != nil {
		workspaceRestoreError(c, err)
		return
	}
	items := make([]workspaceRestoreView, 0, len(page.Items))
	for _, row := range page.Items {
		items = append(items, restoreView(row))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "hasMore": page.HasMore, "nextCursor": page.NextCursor})
}
