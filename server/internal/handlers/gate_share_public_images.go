package handlers

import (
	"bytes"
	"encoding/base64"
	"net/http"
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
		defer func() { _ = rc.Close() }()

		if meta.MimeType == "" {
			meta.MimeType = img.MimeType
		}
		if meta.Name == "" {
			meta.Name = img.Name
		}
		c.Header("Cache-Control", "private, max-age=60")
		writeAttachmentContent(c, rc, meta)
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
	c.Header("Cache-Control", "private, max-age=60")
	writeAttachmentContent(c, bytes.NewReader(raw), blob.Meta{MimeType: img.MimeType, Name: img.Name, Size: int64(len(raw))})
}
