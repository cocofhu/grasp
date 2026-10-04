package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/blob"

	"github.com/gin-gonic/gin"
)

// GetBlob streams a stored attachment by id (blob:{id} without the prefix).
// Image responses sniff magic bytes for Content-Type (no charset) so nosniff
// browsers can decode mislabeled history blobs. Storage bytes/meta are not rewritten.
func (h *Handlers) GetBlob(c *gin.Context) {
	if h.Blobs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "blob store unavailable"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
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

	header := make([]byte, 512)
	n, readErr := io.ReadFull(rc, header)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "blob read failed"})
		return
	}
	header = header[:n]

	mime := blob.StripContentTypeParams(meta.MimeType)
	if sniffed := blob.SniffSupportedImageMIME(header); sniffed != "" {
		mime = sniffed
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	c.Header("Content-Type", mime)
	if name := strings.TrimSpace(meta.Name); name != "" {
		c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(name)))
	}
	if meta.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", meta.Size))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.MultiReader(bytes.NewReader(header), rc))
}
