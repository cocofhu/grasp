package handlers

import (
	"net/http"
	"strconv"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func previewAttachmentMetadata(images []models.PromptImage) []models.PromptImage {
	var out []models.PromptImage
	for _, image := range images {
		out = append(out, models.PromptImage{MimeType: image.MimeType, Name: image.Name, SizeBytes: image.SizeBytes})
	}
	return out
}

// PublicAppPreviewImage only streams attachments submitted through a main preview share.
func (h *Handlers) PublicAppPreviewImage(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	link, sb := h.publicAppShare(c)
	if link == nil {
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 || link.PreviewChatMode != models.PreviewChatMain {
		c.Status(http.StatusNotFound)
		return
	}
	id := c.Param("messageId")
	var images []models.PromptImage
	if sb.Purpose == services.SandboxPurposeSolo {
		if h.Solo != nil {
			images = h.Solo.PreviewMessageAttachments(sb.ThreadID, id)
		}
	} else if state, active := h.workflowPreviewDialogue(link.RunID, link.NodeID); active && state.Iteration == link.Iteration {
		for _, row := range state.Rows {
			if row.ID == id && row.PreviewShared {
				images = row.Images
				break
			}
		}
	}
	if index >= len(images) {
		c.Status(http.StatusNotFound)
		return
	}
	h.writePublicPromptImage(c, images[index])
}
