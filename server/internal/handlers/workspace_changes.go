package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/workspacechanges"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *Handlers) SoloWorkspaceChanges(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	session, err := h.Solo.Require(c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	h.serveWorkspaceChanges(c, services.SoloWorkspaceChangeScope(session), true)
}

func (h *Handlers) RunWorkspaceChanges(c *gin.Context) {
	run, ok := h.Runs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	iteration, _ := strconv.Atoi(c.Query("iteration"))
	live := run.Status != "completed" && run.Status != "failed" && run.Status != "cancelled"
	h.serveWorkspaceChanges(c, workspacechanges.Scope{
		Kind: models.WorkspaceChangesRun, OwnerID: run.ID, Repos: run.Repos,
		NodeID: c.Query("nodeId"), Iteration: iteration,
	}, live)
}

func (h *Handlers) serveWorkspaceChanges(c *gin.Context, scope workspacechanges.Scope, live bool) {
	if h.Changes == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workspace changes unavailable"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	ctx := c.Request.Context()
	if id := c.Param("snapshotId"); id != "" {
		file, err := h.Changes.File(ctx, scope.Kind, scope.OwnerID, id, c.Param("fileId"))
		if err != nil {
			writeWorkspaceChangesError(c, err)
			return
		}
		c.JSON(http.StatusOK, file)
		return
	}
	turn := c.Query("scope") == "turn"
	workspace := c.Query("workspace")
	var current *models.Sandbox
	if live && !turn && h.Sbx != nil && len(scope.Repos) > 0 {
		current, _ = h.Sbx.WorkspaceForChanges(ctx, scope, workspace)
	}
	canRefresh := current != nil && current.Status == "running" && !current.CleanupPending
	if c.Request.Method == http.MethodPost {
		if !canRefresh {
			writeWorkspaceChangesError(c, workspacechanges.ErrUnavailable)
			return
		}
		if err := h.Sbx.RefreshWorkspaceChanges(ctx, scope, current.Name); err != nil {
			writeWorkspaceChangesError(c, err)
			return
		}
		workspace = current.Name
	} else if canRefresh && workspace == "" {
		workspace = current.Name
	}
	view, err := h.Changes.List(ctx, scope, workspace, c.Query("turnId"), turn)
	if err != nil {
		writeWorkspaceChangesError(c, err)
		return
	}
	c.JSON(http.StatusOK, struct {
		workspacechanges.View
		CanRefresh bool `json:"canRefresh"`
	}{view, canRefresh})
}

func writeWorkspaceChangesError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "workspace change not found"})
	case errors.Is(err, workspacechanges.ErrUnavailable):
		c.JSON(http.StatusConflict, gin.H{"error": "workspace is unavailable"})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workspace changes unavailable"})
	}
}
