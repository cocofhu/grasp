package handler

import (
	"errors"
	"net/http"
	"os"
	"strconv"

	"backend/internal/workspacebinding"
	"github.com/gin-gonic/gin"
)

type WorkspaceBinding struct{ Manager *workspacebinding.Manager }

func bindingError(c *gin.Context, err error) {
	status, code := http.StatusConflict, "workspace_binding_changed"
	switch {
	case errors.Is(err, os.ErrNotExist):
		status, code = http.StatusNotFound, "workspace_binding_not_found"
	case errors.Is(err, workspacebinding.ErrInvalid):
		status, code = http.StatusBadRequest, "workspace_binding_invalid"
	case errors.Is(err, workspacebinding.ErrBusy):
		code = "workspace_binding_busy"
	case errors.Is(err, workspacebinding.ErrUnknown):
		code = "workspace_binding_unknown"
	case errors.Is(err, workspacebinding.ErrUninstalled), errors.Is(err, workspacebinding.ErrClosed):
		status, code = http.StatusServiceUnavailable, "workspace_binding_unavailable"
	}
	c.JSON(status, gin.H{"code": code, "error": code})
}
func (h *WorkspaceBinding) Capabilities(c *gin.Context) {
	c.JSON(http.StatusOK, h.Manager.Capabilities())
}
func (h *WorkspaceBinding) Head(c *gin.Context) {
	value, err := h.Manager.Head()
	if err != nil {
		bindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceBinding) Operation(c *gin.Context) {
	value, err := h.Manager.Operation(c.Param("operationId"))
	if err != nil {
		bindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (h *WorkspaceBinding) Port(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		bindingError(c, workspacebinding.ErrInvalid)
		return
	}
	value, err := h.Manager.Port(c.Request.Context(), port)
	if err != nil {
		bindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
