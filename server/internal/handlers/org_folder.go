package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// ExportOrgFolder streams a folder ZIP for the given virtual group subtree.
func (h *Handlers) ExportOrgFolder(c *gin.Context) {
	if h.Org == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "org service unavailable"})
		return
	}
	groupID := strings.TrimSpace(c.Query("groupId"))
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "groupId is required"})
		return
	}
	raw, filename, err := h.Org.ExportFolderZIP(groupID)
	if err != nil {
		writeOrgFolderError(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", contentDispositionAttachment(filename))
	c.Data(http.StatusOK, "application/zip", raw)
}

// ImportOrgFolder accepts a multipart folder ZIP and imports it atomically.
func (h *Handlers) ImportOrgFolder(c *gin.Context) {
	if h.Org == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "org service unavailable"})
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	targetGroupID := strings.TrimSpace(c.PostForm("targetGroupId"))
	mode := services.ImportFolderMode(strings.TrimSpace(c.PostForm("mode")))
	if mode == "" {
		mode = services.ImportFolderRename
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = f.Close() }()

	limited := io.LimitReader(f, services.OrgFolderMaxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if int64(len(raw)) > services.OrgFolderMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": services.ErrOrgFolderTooLarge.Error()})
		return
	}

	result, err := h.Org.ImportFolderZIP(raw, targetGroupID, mode)
	if err != nil {
		writeOrgFolderError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ScanOrgSensitiveKeys lists Token-class env key names in a group subtree (no values).
func (h *Handlers) ScanOrgSensitiveKeys(c *gin.Context) {
	if h.Org == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "org service unavailable"})
		return
	}
	groupID := strings.TrimSpace(c.Query("groupId"))
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "groupId is required"})
		return
	}
	hits, err := h.Org.ScanGroupSensitiveKeys(groupID)
	if err != nil {
		writeOrgFolderError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"keys": hits})
}

type stripOrgSensitiveBody struct {
	GroupID string   `json:"groupId"`
	Keys    []string `json:"keys"`
}

// StripOrgSensitiveKeys removes selected Token-class keys from agents in a group subtree.
func (h *Handlers) StripOrgSensitiveKeys(c *gin.Context) {
	if h.Org == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "org service unavailable"})
		return
	}
	var body stripOrgSensitiveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(body.GroupID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "groupId is required"})
		return
	}
	result, err := h.Org.StripGroupSensitiveKeys(body.GroupID, body.Keys)
	if err != nil {
		writeOrgFolderError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func writeOrgFolderError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrOrgFolderGroupNotFound),
		errors.Is(err, services.ErrOrgFolderTargetNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrOrgConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrOrgValidation),
		errors.Is(err, services.ErrOrgFolderTooLarge),
		errors.Is(err, services.ErrOrgFolderMissingManifest),
		errors.Is(err, services.ErrOrgFolderSingleAgent),
		errors.Is(err, services.ErrOrgFolderInvalidKind),
		errors.Is(err, services.ErrOrgFolderBadSchema),
		errors.Is(err, services.ErrOrgFolderInvalidZip),
		errors.Is(err, services.ErrOrgFolderRootAgentJSON),
		errors.Is(err, services.ErrOrgFolderNestedZip),
		errors.Is(err, services.ErrInvalidAgentName):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		msg := err.Error()
		if strings.Contains(msg, "导入失败，已整次回滚") ||
			strings.Contains(msg, "64MiB") ||
			strings.Contains(msg, "folder.json") ||
			strings.Contains(msg, "单 Agent ZIP") ||
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
