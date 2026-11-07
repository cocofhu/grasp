package handlers

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"

	"github.com/gin-gonic/gin"
)

// GetBlob streams a stored attachment by id (blob:{id} without the prefix).
// Image responses sniff magic bytes for Content-Type (no charset) so nosniff
// browsers can decode mislabeled history blobs. Storage bytes/meta are not rewritten.
func (h *Handlers) GetBlob(c *gin.Context) {
	h.serveBlob(c, c.Param("id"))
}

// Do not depend on the host's MIME database: some systems label .webm audio/webm.
var attachmentMediaTypes = map[string]string{
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".oga": "audio/ogg",
	".m4a": "audio/mp4", ".aac": "audio/aac", ".flac": "audio/flac", ".opus": "audio/ogg",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg",
	".mov": "video/quicktime", ".avi": "video/x-msvideo", ".mkv": "video/x-matroska",
}

func (h *Handlers) serveBlob(c *gin.Context, id string) {
	if h.Blobs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "blob store unavailable"})
		return
	}
	id = strings.TrimSpace(id)
	ref, err := blob.ParseRef("blob:" + id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid blob id"})
		return
	}
	rc, meta, err := h.Blobs.Open(c.Request.Context(), ref)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "blob not found"})
		return
	}
	defer func() { _ = rc.Close() }()
	writeAttachmentContent(c, rc, meta)
}

// Called only after the route has checked its existing workspace/share scope.
// Seekable stores stream byte ranges directly so media controls can seek.
func writeAttachmentContent(c *gin.Context, rc io.Reader, meta blob.Meta) {
	// Generic downloads may contain active documents; opening their URL must not run scripts.
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
	c.Header("X-Content-Type-Options", "nosniff")

	header := make([]byte, 512)
	n, readErr := io.ReadFull(rc, header)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "blob read failed"})
		return
	}
	header = header[:n]

	mediaType := blob.StripContentTypeParams(meta.MimeType)
	if sniffed := blob.SniffSupportedImageMIME(header); sniffed != "" {
		mediaType = sniffed
	}
	if mediaType == "" || mediaType == "application/octet-stream" {
		if inferred := attachmentMediaTypes[strings.ToLower(filepath.Ext(meta.Name))]; inferred != "" {
			mediaType = inferred
		}
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	c.Header("Content-Type", mediaType)
	if name := strings.TrimSpace(meta.Name); name != "" {
		c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filepath.Base(name)}))
	}
	if seeker, ok := rc.(io.ReadSeeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "blob seek failed"})
			return
		}
		http.ServeContent(c.Writer, c.Request, meta.Name, time.Time{}, seeker)
		return
	}
	if meta.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", meta.Size))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.MultiReader(bytes.NewReader(header), rc))
}
