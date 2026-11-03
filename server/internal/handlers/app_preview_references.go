package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func (h *Handlers) appPreviewReferenceScope(link *models.GateShareLink, sb models.Sandbox) (*models.SoloSession, bool) {
	if h.Solo == nil {
		return nil, false
	}
	if sb.Purpose == services.SandboxPurposeSolo {
		row, err := h.Solo.Require(sb.ThreadID, link.CreatedBy)
		if err != nil {
			return nil, false
		}
		row.AgentName = "" // Anonymous lookups use public GitHub access.
		return row, true
	}
	scope, ok := h.runReferenceScope("", link.RunID, link.NodeID)
	if ok {
		scope.AgentName = ""
	}
	return scope, ok
}

func (h *Handlers) PublicAppPreviewReferences(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	link, sb := h.publicAppShare(c)
	if link == nil {
		return
	}
	q := referenceQuery(c)
	scope, ok := h.appPreviewReferenceScope(link, sb)
	if !ok || q.Lane != "github" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reference scope unavailable"})
		return
	}
	res, err := h.Solo.SearchScopedReferences(c.Request.Context(), scope, q)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handlers) prepareAppPreviewReferences(ctx context.Context, link *models.GateShareLink, sb models.Sandbox, inputs []services.ReferenceInput) ([]models.MessageReference, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	scope, ok := h.appPreviewReferenceScope(link, sb)
	if !ok {
		return nil, errors.New("reference scope unavailable")
	}
	for _, input := range inputs {
		if input.Kind != models.ReferenceKindGitHubIssue && input.Kind != models.ReferenceKindGitHubPR {
			return nil, errors.New("preview shares can reference public GitHub issues and pull requests")
		}
	}
	refs, err := h.Solo.PrepareScopedReferences(ctx, scope, inputs)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		if ref.Access != models.ReferenceAccessOK {
			return nil, errors.New("GitHub reference is not publicly accessible")
		}
	}
	return refs, nil
}
