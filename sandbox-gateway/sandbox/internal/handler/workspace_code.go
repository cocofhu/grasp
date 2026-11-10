package handler

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"backend/internal/workspacebinding"
	"backend/internal/workspacegit"
	"github.com/gin-gonic/gin"
)

// WorkspaceCode only accepts workspace-relative repository selections. The
// runtime's authenticated server connection supplies its fixed workspace root.
type WorkspaceCode struct {
	service    *workspacegit.Service
	fenceMu    sync.Mutex
	generation uint64
	active     *workspaceCodeOperation
	lane       chan struct{}
	binding    *workspacebinding.Manager
}

func NewWorkspaceCode(root string) *WorkspaceCode {
	return &WorkspaceCode{service: &workspacegit.Service{Root: root}, lane: make(chan struct{}, 1)}
}

func NewBoundWorkspaceCode(binding *workspacebinding.Manager) *WorkspaceCode {
	return &WorkspaceCode{binding: binding, lane: make(chan struct{}, 1)}
}

func (h *WorkspaceCode) current(c *gin.Context) (*workspacegit.Service, func(), bool) {
	if h.binding == nil {
		return h.service, func() {}, true
	}
	view, release, err := h.binding.Acquire(nil, false)
	if err != nil {
		bindingError(c, err)
		return nil, nil, false
	}
	return &workspacegit.Service{Root: view.Path}, release, true
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
	service, release, ok := h.current(c)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Minute)
	defer cancel()
	value, err := service.Inventory(ctx)
	if err != nil {
		codeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceCode) Export(c *gin.Context) {
	service, release, rootOK := h.current(c)
	if !rootOK {
		return
	}
	defer release()
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
	if _, err = service.Export(ctx, body.Repositories, f); err != nil {
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
	service, release, rootOK := h.current(c)
	if !rootOK {
		return
	}
	defer release()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workspacegit.MaxTotalBytes+(1<<20))
	ctx, cancel, ok := h.operation(c)
	if !ok {
		return
	}
	defer cancel()
	value, err := service.Import(ctx, c.Param("importId"), c.GetHeader("X-Grasp-Archive-SHA256"), c.Request.Body)
	if err != nil {
		codeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceCode) Receipt(c *gin.Context) {
	service, release, ok := h.current(c)
	if !ok {
		return
	}
	defer release()
	value, err := service.Receipt(c.Param("importId"))
	if err != nil {
		codeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
