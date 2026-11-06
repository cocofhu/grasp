package handlers

import (
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func artifactRevisionQuery(c *gin.Context) (int, bool) {
	raw := c.Query("revision")
	if raw == "" {
		return 0, true
	}
	rev, err := strconv.Atoi(raw)
	if err != nil || rev < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_revision"})
		return 0, false
	}
	return rev, true
}

// ArtifactRaw serves browser media with native Range/HEAD support. Only safe
// preview types are inline; active documents use the existing isolated preview.
func (h *Handlers) ArtifactRaw(c *gin.Context) { h.serveArtifactFile(c, false) }

func (h *Handlers) serveArtifactFile(c *gin.Context, download bool) {
	rev, ok := artifactRevisionQuery(c)
	if !ok {
		return
	}
	file, err := h.Arts.OpenFile(c.Request.Context(), c.Param("id"), rev)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "artifact_not_found"})
		} else {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "artifact_file_unavailable"})
		}
		return
	}
	defer func() { _ = file.Reader.Close() }()
	serveOpenedArtifact(c, file, download)
}

func serveOpenedArtifact(c *gin.Context, file services.ArtifactFile, download bool) {
	disposition := "inline"
	media := file.MimeType
	if download && file.Artifact.MimeType == "" && file.Artifact.Kind != "image" {
		media = "application/octet-stream"
	}
	safeInline := media == "image/png" || media == "image/jpeg" || media == "image/webp" || media == "image/gif" || media == "application/pdf" || strings.HasPrefix(media, "video/") || strings.HasPrefix(media, "audio/") || media == "text/plain"
	if download || !safeInline {
		disposition = "attachment"
	}
	name := filepath.Base(strings.ReplaceAll(file.Artifact.Name, "\\", "/"))
	c.Header("Content-Type", media)
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	c.Header("X-Content-Type-Options", "nosniff")
	if media == "application/pdf" && !download {
		c.Header("Content-Security-Policy", "default-src 'none'; object-src 'self'; frame-ancestors 'self'")
	} else {
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
	}
	c.Header("Cache-Control", "private, no-cache")
	if file.SHA256 != "" {
		c.Header("ETag", "\""+file.SHA256+"\"")
	}
	http.ServeContent(c.Writer, c.Request, name, file.Artifact.UpdatedAt, file.Reader)
}
