package handlers

import (
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *Handlers) snapshotHistoryReady(c *gin.Context) (string, bool) {
	if c.GetString(soloEmbedUserKey) != "" {
		c.JSON(http.StatusForbidden, gin.H{"code": "workspace_snapshot_forbidden"})
		return "", false
	}
	if h.WorkspaceSnapshots == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_snapshot_unavailable"})
		return "", false
	}
	return h.sessionUser(c)
}

func snapshotHistoryError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "workspace_snapshot_unavailable"
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status, code = http.StatusNotFound, "workspace_snapshot_not_found"
	} else if errors.Is(err, services.ErrWorkspaceJobInvalid) {
		status, code = http.StatusBadRequest, "invalid_workspace_snapshot_request"
	}
	c.JSON(status, gin.H{"code": code})
}

func (h *Handlers) listWorkspaceSnapshots(c *gin.Context, kind string) {
	actor, ok := h.snapshotHistoryReady(c)
	if !ok {
		return
	}
	page, ok := parseCursorPagination(c)
	if !ok {
		return
	}
	rows, err := h.WorkspaceSnapshots.List(c.Request.Context(), actor, kind, c.Param("id"), page.Cursor, page.Limit)
	if err != nil {
		snapshotHistoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

func (h *Handlers) getWorkspaceSnapshot(c *gin.Context, kind string) {
	actor, ok := h.snapshotHistoryReady(c)
	if !ok {
		return
	}
	row, err := h.WorkspaceSnapshots.Get(c.Request.Context(), actor, kind, c.Param("id"), c.Param("snapshotId"))
	if err != nil {
		snapshotHistoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *Handlers) ListSoloWorkspaceSnapshots(c *gin.Context) { h.listWorkspaceSnapshots(c, "solo") }
func (h *Handlers) GetSoloWorkspaceSnapshot(c *gin.Context)   { h.getWorkspaceSnapshot(c, "solo") }
func (h *Handlers) ListRunWorkspaceSnapshots(c *gin.Context)  { h.listWorkspaceSnapshots(c, "run") }
func (h *Handlers) GetRunWorkspaceSnapshot(c *gin.Context)    { h.getWorkspaceSnapshot(c, "run") }
