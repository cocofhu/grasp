package handler

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"backend/internal/workspacegit"
	"github.com/gin-gonic/gin"
)

// WorkspaceCode only accepts workspace-relative repository selections. The
// runtime's authenticated server connection supplies its fixed workspace root.
type WorkspaceCode struct {
	service    *workspacegit.Service
	fenceMu    sync.Mutex
	generation uint64
	active     context.CancelFunc
}

func NewWorkspaceCode(root string) *WorkspaceCode {
	return &WorkspaceCode{service: &workspacegit.Service{Root: root}}
}

// The bridge accepts only a live acknowledged server lease. The absolute
// deadline bounds subprocesses even if a disconnected server can no longer
// cancel its HTTP request. A newer generation cancels an earlier request before
// it is allowed to touch the isolated import directory.
func (h *WorkspaceCode) operation(c *gin.Context) (context.Context, context.CancelFunc, bool) {
	generation, err := strconv.ParseUint(c.GetHeader("X-Grasp-Operation-Generation"), 10, 64)
	expires, dateErr := time.Parse(time.RFC3339Nano, c.GetHeader("X-Grasp-Operation-Expires-At"))
	now := time.Now()
	if err != nil || dateErr != nil || generation == 0 || !expires.After(now) || expires.After(now.Add(time.Minute)) {
		c.JSON(http.StatusConflict, gin.H{"code": "workspace_operation_expired", "error": "workspace_operation_expired"})
		return nil, nil, false
	}
	h.fenceMu.Lock()
	defer h.fenceMu.Unlock()
	if generation <= h.generation {
		c.JSON(http.StatusConflict, gin.H{"code": "workspace_operation_superseded", "error": "workspace_operation_superseded"})
		return nil, nil, false
	}
	if h.active != nil {
		h.active()
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), expires)
	h.generation, h.active = generation, cancel
	return ctx, cancel, true
}
func codeError(c *gin.Context, err error) {
	status, code := http.StatusBadGateway, "workspace_transfer_failed"
	switch {
	case errors.Is(err, workspacegit.ErrUnsafe):
		status, code = http.StatusBadRequest, "unsafe_archive"
	case errors.Is(err, workspacegit.ErrUnsupported):
		status, code = http.StatusUnprocessableEntity, "snapshot_required"
	case errors.Is(err, workspacegit.ErrLimit):
		status, code = http.StatusRequestEntityTooLarge, "workspace_transfer_limit"
	case errors.Is(err, workspacegit.ErrConflict):
		status, code = http.StatusConflict, "workspace_import_uncertain"
	case errors.Is(err, os.ErrNotExist):
		status, code = http.StatusNotFound, "workspace_import_not_found"
	}
	c.JSON(status, gin.H{"code": code, "error": code})
}
func (h *WorkspaceCode) Inventory(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Minute)
	defer cancel()
	value, err := h.service.Inventory(ctx)
	if err != nil {
		codeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceCode) Export(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var body struct {
		Repositories []workspacegit.Selection `json:"repositories"`
	}
	if c.ShouldBindJSON(&body) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid selection"})
		return
	}
	ctx, cancel, ok := h.operation(c)
	if !ok {
		return
	}
	defer cancel()
	f, err := os.CreateTemp("", "grasp-code-response-*")
	if err != nil {
		codeError(c, err)
		return
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = h.service.Export(ctx, body.Repositories, f); err != nil {
		codeError(c, err)
		return
	}
	info, err := f.Stat()
	if err != nil {
		codeError(c, err)
		return
	}
	if _, err = f.Seek(0, 0); err != nil {
		codeError(c, err)
		return
	}
	c.DataFromReader(http.StatusOK, info.Size(), "application/gzip", f, nil)
}
func (h *WorkspaceCode) Import(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workspacegit.MaxTotalBytes+(1<<20))
	ctx, cancel, ok := h.operation(c)
	if !ok {
		return
	}
	defer cancel()
	value, err := h.service.Import(ctx, c.Param("importId"), c.GetHeader("X-Grasp-Archive-SHA256"), c.Request.Body)
	if err != nil {
		codeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceCode) Receipt(c *gin.Context) {
	value, err := h.service.Receipt(c.Param("importId"))
	if err != nil {
		codeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
