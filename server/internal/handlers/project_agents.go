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

// projectForAgents resolves :id for the project-level Agent endpoints.
func (h *Handlers) projectForAgents(c *gin.Context) (models.Project, bool) {
	if h.Projects == nil || h.Agents == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "agent service unavailable"})
		return models.Project{}, false
	}
	p, ok := h.Projects.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return models.Project{}, false
	}
	return p, true
}

// ExportProjectAgents streams a ZIP with every Agent of the project.
func (h *Handlers) ExportProjectAgents(c *gin.Context) {
	p, ok := h.projectForAgents(c)
	if !ok {
		return
	}
	raw, err := h.Agents.ExportProjectAgentsZIP(p.ID)
	if err != nil {
		writeProjectAgentsError(c, err)
		return
	}
	name := services.SanitizeDownloadFilename(p.Name)
	if name == "" {
		name = services.SanitizeDownloadFilename(p.ID)
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", contentDispositionAttachment(name+"-agents.zip"))
	c.Data(http.StatusOK, "application/zip", raw)
}

// ImportProjectAgents accepts a multipart project package and imports it atomically.
func (h *Handlers) ImportProjectAgents(c *gin.Context) {
	p, ok := h.projectForAgents(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	mode := services.ImportProjectAgentsMode(strings.TrimSpace(c.PostForm("mode")))
	if mode == "" {
		mode = services.ImportProjectAgentsRename
	}
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, services.ProjectAgentsBundleMaxBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if int64(len(raw)) > services.ProjectAgentsBundleMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": services.ErrProjectBundleTooLarge.Error()})
		return
	}
	result, err := h.Agents.ImportProjectAgentsZIP(raw, p.ID, mode)
	if err != nil {
		writeProjectAgentsError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func writeProjectAgentsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrAgentProjectRequired),
		errors.Is(err, services.ErrProjectBundleTooLarge),
		errors.Is(err, services.ErrProjectBundleMissingManifest),
		errors.Is(err, services.ErrProjectBundleSingleAgent),
		errors.Is(err, services.ErrProjectBundleInvalidKind),
		errors.Is(err, services.ErrProjectBundleBadSchema),
		errors.Is(err, services.ErrProjectBundleInvalidZip),
		errors.Is(err, services.ErrProjectBundleRootAgentJSON),
		errors.Is(err, services.ErrProjectBundleNestedZip),
		errors.Is(err, services.ErrProjectBundleForeignAgent),
		errors.Is(err, services.ErrInvalidAgentName),
		errors.Is(err, services.ErrInvalidAcpBackend),
		errors.Is(err, services.ErrSecretEnvKey):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		msg := err.Error()
		if strings.Contains(msg, "导入失败，已整次回滚") ||
			strings.Contains(msg, "project.json") ||
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
