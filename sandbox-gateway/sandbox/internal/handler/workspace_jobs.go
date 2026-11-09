package handler

import (
	"errors"
	"net/http"
	"os"

	"backend/internal/workspacejob"
	"github.com/gin-gonic/gin"
)

// WorkspaceJobs is registered only behind bridge authentication by the owner
// of the runtime router. A nil service reports no capability, never a fake
// successful snapshot/restore. Construction never chooses a root from HTTP.
type WorkspaceJobs struct{ Service *workspacejob.Service }

func jobError(c *gin.Context, err error) {
	status, code := http.StatusBadGateway, "workspace_job_unavailable"
	switch {
	case errors.Is(err, workspacejob.ErrUnsupported):
		status, code = http.StatusNotImplemented, "workspace_job_unsupported"
	case errors.Is(err, workspacejob.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_workspace_job"
	case errors.Is(err, workspacejob.ErrLineage):
		status, code = http.StatusConflict, "workspace_lineage_mismatch"
	case errors.Is(err, workspacejob.ErrFence):
		status, code = http.StatusConflict, "workspace_job_owner_lost"
	case errors.Is(err, workspacejob.ErrConflict):
		status, code = http.StatusConflict, "workspace_job_identity_conflict"
	case errors.Is(err, workspacejob.ErrUnknown):
		status, code = http.StatusConflict, "workspace_job_reconcile_required"
	case errors.Is(err, workspacejob.ErrLimit):
		status, code = http.StatusRequestEntityTooLarge, "workspace_job_limit"
	case errors.Is(err, os.ErrNotExist):
		status, code = http.StatusNotFound, "workspace_job_not_found"
	}
	c.JSON(status, gin.H{"code": code, "error": code})
}

func (h *WorkspaceJobs) ready(c *gin.Context) bool {
	if h.Service != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_job_unavailable", "error": "workspace_job_unavailable"})
	return false
}
func (h *WorkspaceJobs) Capabilities(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	c.JSON(http.StatusOK, h.Service.Capabilities())
}
func (h *WorkspaceJobs) Create(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 96<<10)
	var request workspacejob.CreateRequest
	if c.ShouldBindJSON(&request) != nil {
		jobError(c, workspacejob.ErrInvalid)
		return
	}
	receipt, err := h.Service.Create(request)
	if err != nil {
		jobError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, receipt)
}
func (h *WorkspaceJobs) Get(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	receipt, err := h.Service.Get(c.Param("jobId"))
	if err != nil {
		jobError(c, err)
		return
	}
	c.JSON(http.StatusOK, receipt)
}
func (h *WorkspaceJobs) Renew(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var request workspacejob.RenewRequest
	if c.ShouldBindJSON(&request) != nil {
		jobError(c, workspacejob.ErrInvalid)
		return
	}
	receipt, err := h.Service.Renew(c.Param("jobId"), request)
	if err != nil {
		jobError(c, err)
		return
	}
	c.JSON(http.StatusOK, receipt)
}
func (h *WorkspaceJobs) Cancel(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var request workspacejob.RenewRequest
	if c.ShouldBindJSON(&request) != nil {
		jobError(c, workspacejob.ErrInvalid)
		return
	}
	receipt, err := h.Service.Cancel(c.Param("jobId"), request)
	if err != nil {
		jobError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, receipt)
}
