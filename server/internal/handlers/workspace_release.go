package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WorkspaceReleaseAPI interface {
	List(context.Context, string, string) ([]services.WorkspaceReleaseView, error)
	Preview(context.Context, string, string, services.WorkspaceReleasePreviewInput) (services.WorkspaceReleaseView, error)
	Get(context.Context, string, string, string) (services.WorkspaceReleaseView, error)
	Confirm(context.Context, string, string, string, services.WorkspaceReleaseConfirmInput) (services.WorkspaceReleaseView, error)
	Reconcile(context.Context, string, string, string) (services.WorkspaceReleaseView, error)
}
type WorkspaceReleaseHandler struct {
	Service  WorkspaceReleaseAPI
	Identity func(*gin.Context) (string, bool)
}

func (h *WorkspaceReleaseHandler) ready(c *gin.Context) (string, bool) {
	if c.GetString(soloEmbedUserKey) != "" {
		c.JSON(http.StatusForbidden, gin.H{"code": "workspace_release_forbidden"})
		return "", false
	}
	if h == nil || h.Service == nil || h.Identity == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"code": "workspace_release_unsupported", "reason": "configuration_unavailable"})
		return "", false
	}
	actor, ok := h.Identity(c)
	if !ok || actor == "" {
		if !c.Writer.Written() {
			c.JSON(http.StatusUnauthorized, gin.H{"code": "workspace_release_unauthorized"})
		}
		return "", false
	}
	return actor, true
}
func workspaceReleaseError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "workspace_release_unavailable"
	switch {
	case errors.Is(err, services.ErrWorkspaceReleaseUnsupported):
		status, code = http.StatusNotImplemented, "workspace_release_unsupported"
	case errors.Is(err, services.ErrWorkspaceReleaseInvalid):
		status, code = http.StatusBadRequest, "invalid_workspace_release"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "workspace_release_not_found"
	case errors.Is(err, services.ErrWorkspaceReleaseConflict):
		status, code = http.StatusConflict, "workspace_release_scope_changed"
	case errors.Is(err, services.ErrWorkspaceReleaseBlocked):
		status, code = http.StatusConflict, "workspace_release_blocked"
	case errors.Is(err, services.ErrWorkspaceReleaseUnknown):
		status, code = http.StatusConflict, "workspace_release_reconcile_required"
	case errors.Is(err, executionlineage.ErrInactive), errors.Is(err, executionlineage.ErrConflict), errors.Is(err, executionlineage.ErrQuarantined):
		status, code = http.StatusConflict, "workspace_release_execution_blocked"
	}
	response := gin.H{"code": code}
	if status == http.StatusNotImplemented {
		response["reason"] = "configuration_unavailable"
		var capability *services.WorkspaceReleaseUnsupportedError
		if errors.As(err, &capability) {
			response["reason"] = capability.Reason
		}
	}
	c.JSON(status, response)
}
func decodeWorkspaceRelease(c *gin.Context, input any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workspaceControlBody)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(input)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = services.ErrWorkspaceReleaseInvalid
		}
	}
	if err != nil {
		workspaceReleaseError(c, services.ErrWorkspaceReleaseInvalid)
		return false
	}
	return true
}
func (h *WorkspaceReleaseHandler) List(c *gin.Context) {
	actor, ok := h.ready(c)
	if !ok {
		return
	}
	items, err := h.Service.List(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		workspaceReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
func (h *WorkspaceReleaseHandler) Preview(c *gin.Context) {
	actor, ok := h.ready(c)
	if !ok {
		return
	}
	var in services.WorkspaceReleasePreviewInput
	if !decodeWorkspaceRelease(c, &in) {
		return
	}
	view, err := h.Service.Preview(c.Request.Context(), actor, c.Param("id"), in)
	if err != nil {
		workspaceReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
func (h *WorkspaceReleaseHandler) Get(c *gin.Context) {
	actor, ok := h.ready(c)
	if !ok {
		return
	}
	view, err := h.Service.Get(c.Request.Context(), actor, c.Param("id"), c.Param("releaseId"))
	if err != nil {
		workspaceReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
func (h *WorkspaceReleaseHandler) Confirm(c *gin.Context) {
	actor, ok := h.ready(c)
	if !ok {
		return
	}
	var in services.WorkspaceReleaseConfirmInput
	if !decodeWorkspaceRelease(c, &in) {
		return
	}
	view, err := h.Service.Confirm(c.Request.Context(), actor, c.Param("id"), c.Param("releaseId"), in)
	if err != nil {
		workspaceReleaseError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, view)
}
func (h *WorkspaceReleaseHandler) Reconcile(c *gin.Context) {
	actor, ok := h.ready(c)
	if !ok {
		return
	}
	view, err := h.Service.Reconcile(c.Request.Context(), actor, c.Param("id"), c.Param("releaseId"))
	if err != nil {
		workspaceReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handlers) workspaceReleaseHandler() *WorkspaceReleaseHandler {
	return &WorkspaceReleaseHandler{Service: h.WorkspaceReleases, Identity: h.sessionUser}
}
func (h *Handlers) ListWorkspaceReleases(c *gin.Context)   { h.workspaceReleaseHandler().List(c) }
func (h *Handlers) PreviewWorkspaceRelease(c *gin.Context) { h.workspaceReleaseHandler().Preview(c) }
func (h *Handlers) GetWorkspaceRelease(c *gin.Context)     { h.workspaceReleaseHandler().Get(c) }
func (h *Handlers) ConfirmWorkspaceRelease(c *gin.Context) { h.workspaceReleaseHandler().Confirm(c) }
func (h *Handlers) ReconcileWorkspaceRelease(c *gin.Context) {
	h.workspaceReleaseHandler().Reconcile(c)
}
