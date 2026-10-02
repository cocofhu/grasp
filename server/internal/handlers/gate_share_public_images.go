package handlers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

// PublicGateImage streams one dialogue image by opaque index for a valid share
// or embed credential. Indexes only resolve inside this dialogue's turns +
// in-flight session images (never arbitrary blob ids).
func (h *Handlers) PublicGateImage(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	token := strings.TrimSpace(c.GetHeader(headerShareToken))
	if token == "" {
		token = strings.TrimSpace(c.Query("token"))
	}
	if token == "" || !gateshare.ValidCredentialShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st == models.ShareLinkStateNone {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	if st != models.ShareLinkStateActive {
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	idx, err := strconv.Atoi(strings.TrimSpace(c.Param("index")))
	if err != nil || idx < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_index"})
		return
	}
	visitor := strings.TrimSpace(c.GetHeader(headerShareVisitor))
	if visitor == "" {
		visitor = strings.TrimSpace(c.Query("v"))
	}
	lane, ok := h.publicLane(token, lookup, visitor)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "visitor_required"})
		return
	}
	img, ok := h.resolvePublicDialogueImage(lookup, lane, idx)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	h.writePublicPromptImage(c, img)
}

func (h *Handlers) resolvePublicDialogueImage(lookup *gateshare.LookupResult, lane string, index int) (models.PromptImage, bool) {
	if lookup == nil || index < 0 {
		return models.PromptImage{}, false
	}
	producerID := h.publicDialogueProducerID(lookup)
	if producerID == "" {
		producerID = strings.TrimSpace(lookup.Link.NodeID)
	}
	turns := h.publicLaneTurns(lookup, producerID, lane)
	active, queue := h.publicLaneQueue(lookup.Link.RunID, producerID, lane)
	catalog := gateshare.DialogueImageCatalog(turns, active, queue)
	if index >= len(catalog) {
		return models.PromptImage{}, false
	}
	return catalog[index], true
}

func (h *Handlers) writePublicPromptImage(c *gin.Context, img models.PromptImage) {
	ref := strings.TrimSpace(img.Ref)
	if ref != "" {
		parsed, err := blob.ParseRef(ref)
		if err != nil || h.Blobs == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		rc, meta, err := h.Blobs.Open(c.Request.Context(), parsed)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		defer rc.Close()

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
			mime = strings.TrimSpace(img.MimeType)
		}
		if mime == "" {
			mime = "application/octet-stream"
		}
		c.Header("Content-Type", mime)
		c.Header("Cache-Control", "private, max-age=60")
		name := strings.TrimSpace(meta.Name)
		if name == "" {
			name = strings.TrimSpace(img.Name)
		}
		if name != "" {
			c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(name)))
		}
		if meta.Size > 0 {
			c.Header("Content-Length", fmt.Sprintf("%d", meta.Size))
		}
		c.Status(http.StatusOK)
		_, _ = io.Copy(c.Writer, io.MultiReader(bytes.NewReader(header), rc))
		return
	}

	data := strings.TrimSpace(img.Data)
	if data == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	mime := strings.TrimSpace(img.MimeType)
	if sniffed := blob.SniffSupportedImageMIME(raw); sniffed != "" {
		mime = sniffed
	}
	if mime == "" {
		mime = "image/png"
	}
	c.Header("Content-Type", mime)
	c.Header("Cache-Control", "private, max-age=60")
	if name := strings.TrimSpace(img.Name); name != "" {
		c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(name)))
	}
	c.Header("Content-Length", fmt.Sprintf("%d", len(raw)))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(raw)
}
