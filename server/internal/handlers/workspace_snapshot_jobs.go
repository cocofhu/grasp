package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WorkspaceSnapshotJobService interface {
	Admit(context.Context, string, services.WorkspaceJobInput) (models.WorkspaceSnapshotJob, error)
	Get(context.Context, string, string) (models.WorkspaceSnapshotJob, error)
	Reconcile(context.Context, string, string) (models.WorkspaceSnapshotJob, error)
}

// This independent handler is not registered by the control foundation. The
// application must provide cookie-session identity, metadata authorization,
// external execution authority and an installed kind before exposing admission.
type WorkspaceSnapshotJobHandler struct {
	Jobs     WorkspaceSnapshotJobService
	Identity func(*gin.Context) (string, bool)
}

type workspaceSnapshotJobView struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	Deadline  time.Time `json:"deadline"`
	CreatedAt time.Time `json:"createdAt"`
	// No owner capability, runtime identity, paths, provider secrets, internal
	// generation or claimed file availability are returned to the browser.
}

func snapshotJobView(row models.WorkspaceSnapshotJob) workspaceSnapshotJobView {
	return workspaceSnapshotJobView{ID: row.ID, State: row.State, Deadline: row.Deadline, CreatedAt: row.CreatedAt}
}

func (h *WorkspaceSnapshotJobHandler) ready(c *gin.Context) (string, bool) {
	if c.GetString(soloEmbedUserKey) != "" {
		c.JSON(http.StatusForbidden, gin.H{"code": "workspace_job_forbidden"})
		return "", false
	}
	if h.Jobs == nil || h.Identity == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_job_unavailable"})
		return "", false
	}
	actor, ok := h.Identity(c)
	if !ok || actor == "" {
		if !c.Writer.Written() {
			c.JSON(http.StatusUnauthorized, gin.H{"code": "workspace_job_unauthorized"})
		}
		return "", false
	}
	return actor, true
}

func snapshotJobError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "workspace_job_unavailable"
	switch {
	case errors.Is(err, services.ErrWorkspaceJobInvalid):
		status, code = http.StatusBadRequest, "invalid_workspace_job"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "workspace_job_not_found"
	case errors.Is(err, services.ErrWorkspaceJobUnsupported):
		status, code = http.StatusNotImplemented, "workspace_job_unsupported"
	case errors.Is(err, services.ErrWorkspaceJobConflict):
		status, code = http.StatusConflict, "workspace_job_identity_conflict"
	case errors.Is(err, services.ErrWorkspaceJobOwner), errors.Is(err, services.ErrWorkspaceJobUnknown):
		status, code = http.StatusConflict, "workspace_job_reconcile_required"
	}
	c.JSON(status, gin.H{"code": code})
}

func (h *WorkspaceSnapshotJobHandler) Admit(c *gin.Context) {
	actor, ok := h.ready(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workspaceSnapshotBody)
	var input struct {
		ClientRequestID string          `json:"clientRequestId"`
		SandboxID       uint            `json:"sandboxId"`
		Kind            string          `json:"kind"`
		Body            json.RawMessage `json:"body"`
		DurationMillis  int64           `json:"durationMillis"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.DurationMillis <= 0 || input.DurationMillis > (24*time.Hour).Milliseconds() {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace_job"})
		return
	}
	// HTTP only records a durable intent. It must not call RunOne or start a
	// request-scoped executor after sending a successful response.
	row, err := h.Jobs.Admit(c.Request.Context(), actor, services.WorkspaceJobInput{ClientRequestID: input.ClientRequestID, ScopeKind: "solo", ScopeID: c.Param("id"), SandboxID: input.SandboxID, Kind: input.Kind, Body: input.Body, Duration: time.Duration(input.DurationMillis) * time.Millisecond})
	if err != nil {
		snapshotJobError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, snapshotJobView(row))
}

func (h *WorkspaceSnapshotJobHandler) read(c *gin.Context, reconcile bool) {
	actor, ok := h.ready(c)
	if !ok {
		return
	}
	row, err := h.Jobs.Get(c.Request.Context(), actor, c.Param("jobId"))
	if err != nil || row.ScopeKind != "solo" || row.ScopeID != c.Param("id") {
		if err == nil {
			err = gorm.ErrRecordNotFound
		}
		snapshotJobError(c, err)
		return
	}
	if reconcile {
		row, err = h.Jobs.Reconcile(c.Request.Context(), actor, row.ID)
		if err != nil {
			snapshotJobError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, snapshotJobView(row))
}

func (h *WorkspaceSnapshotJobHandler) Get(c *gin.Context)       { h.read(c, false) }
func (h *WorkspaceSnapshotJobHandler) Reconcile(c *gin.Context) { h.read(c, true) }
