package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func (h *Handlers) workspaceImports(c *gin.Context) (*services.WorkspaceImportService, string, bool) {
	// These routes are registered under cookie-session APIs only. A preview
	// drawer bearer session must never become filesystem mutation authority.
	if c.GetString(soloEmbedUserKey) != "" {
		c.JSON(http.StatusForbidden, gin.H{"code": "workspace_import_forbidden", "error": "请在 Solo 会话中导入代码"})
		return nil, "", false
	}
	user, ok := h.soloReady(c)
	if !ok {
		return nil, "", false
	}
	if h.Sbx == nil || h.Runs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_import_unavailable", "error": "工作区导入暂不可用"})
		return nil, "", false
	}
	return services.NewWorkspaceImportService(h.Runs.DB(), h.Solo, h.Sbx), user, true
}

func (h *Handlers) WorkspaceImportSources(c *gin.Context) {
	s, user, ok := h.workspaceImports(c)
	if !ok {
		return
	}
	result, err := s.Sources(c.Request.Context(), c.Param("id"), user, c.Query("runId"))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) ListWorkspaceImports(c *gin.Context) {
	s, user, ok := h.workspaceImports(c)
	if !ok {
		return
	}
	result, err := s.List(c.Request.Context(), c.Param("id"), user, c.Query("runId"))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) WorkspaceImportInventory(c *gin.Context) {
	s, user, ok := h.workspaceImports(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var in services.WorkspaceImportSourceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace_source", "error": "工作区来源无效"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.Inventory(ctx, c.Param("id"), user, in)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) PreviewWorkspaceImport(c *gin.Context) {
	s, user, ok := h.workspaceImports(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var in services.WorkspaceImportPreviewInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace_import", "error": "导入预览内容无效"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()
	result, err := s.Preview(ctx, c.Param("id"), user, in)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) GetWorkspaceImport(c *gin.Context) {
	s, user, ok := h.workspaceImports(c)
	if !ok {
		return
	}
	result, err := s.Get(c.Request.Context(), c.Param("id"), user, c.Param("importId"))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) ConfirmWorkspaceImport(c *gin.Context) {
	s, user, ok := h.workspaceImports(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var in services.WorkspaceImportConfirmInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace_import", "error": "导入确认内容无效"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()
	result, err := s.Confirm(ctx, c.Param("id"), user, c.Param("importId"), in)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) ReconcileWorkspaceImport(c *gin.Context) {
	s, user, ok := h.workspaceImports(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.Reconcile(ctx, c.Param("id"), user, c.Param("importId"))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
