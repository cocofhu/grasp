package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// workspaceForAgents loads the workspace for the workspace-level Agent endpoints.
func (h *Handlers) workspaceForAgents(c *gin.Context) (models.Workspace, bool) {
	if h.Workspace == nil || h.Agents == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "agent service unavailable"})
		return models.Workspace{}, false
	}
	p, err := h.Workspace.Workspace()
	if err != nil {
		writeWorkspaceErr(c, err)
		return models.Workspace{}, false
	}
	return p, true
}

// ExportWorkspaceAgents streams a ZIP with every Agent.
func (h *Handlers) ExportWorkspaceAgents(c *gin.Context) {
	p, ok := h.workspaceForAgents(c)
	if !ok {
		return
	}
	raw, err := h.Agents.ExportWorkspaceAgentsZIP()
	if err != nil {
		writeWorkspaceAgentsError(c, err)
		return
	}
	name := services.SanitizeDownloadFilename(p.Name)
	if name == "" {
		name = "workspace"
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", contentDispositionAttachment(name+"-agents.zip"))
	c.Data(http.StatusOK, "application/zip", raw)
}

// ImportWorkspaceAgents accepts a multipart workspace package and imports it atomically.
func (h *Handlers) ImportWorkspaceAgents(c *gin.Context) {
	if _, ok := h.workspaceForAgents(c); !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	mode := services.ImportWorkspaceAgentsMode(strings.TrimSpace(c.PostForm("mode")))
	if mode == "" {
		mode = services.ImportWorkspaceAgentsRename
	}
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, services.WorkspaceAgentsBundleMaxBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if int64(len(raw)) > services.WorkspaceAgentsBundleMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": services.ErrWorkspaceBundleTooLarge.Error()})
		return
	}
	result, err := h.Agents.ImportWorkspaceAgentsZIP(raw, mode)
	if err != nil {
		writeWorkspaceAgentsError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func writeWorkspaceAgentsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrWorkspaceBundleTooLarge),
		errors.Is(err, services.ErrWorkspaceBundleMissingManifest),
		errors.Is(err, services.ErrWorkspaceBundleSingleAgent),
		errors.Is(err, services.ErrWorkspaceBundleInvalidKind),
		errors.Is(err, services.ErrWorkspaceBundleBadSchema),
		errors.Is(err, services.ErrWorkspaceBundleInvalidZip),
		errors.Is(err, services.ErrWorkspaceBundleRootAgentJSON),
		errors.Is(err, services.ErrWorkspaceBundleNestedZip),
		errors.Is(err, services.ErrInvalidAgentName),
		errors.Is(err, services.ErrInvalidAcpBackend),
		errors.Is(err, services.ErrSecretEnvKey):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		msg := err.Error()
		if strings.Contains(msg, "导入失败，已整次回滚") ||
			strings.Contains(msg, "workspace.json") ||
			strings.Contains(msg, "1MiB") ||
			strings.Contains(msg, "invalid import mode") {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
	}
}

// contentDispositionAttachment quotes filename and adds RFC 5987 filename*.
func contentDispositionAttachment(filename string) string {
	escaped := strings.ReplaceAll(filename, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	var b strings.Builder
	for i := 0; i < len(filename); i++ {
		c := filename[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, escaped, b.String())
}
