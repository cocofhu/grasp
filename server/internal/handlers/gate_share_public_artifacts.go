package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

// PublicGateArtifacts lists Run-level artifact metadata for ShareLinkKindReview
// holders. Content is loaded on demand via PublicGateArtifactContent.
func (h *Handlers) PublicGateArtifacts(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	lookup, st, ok := h.publicReviewShareLookup(c)
	if !ok {
		return
	}
	if st != models.ShareLinkStateActive {
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	arts := make([]gin.H, 0)
	if h.Arts != nil {
		for _, a := range h.Arts.ByRun(lookup.Link.RunID) {
			arts = append(arts, publicArtifactMetaDTO(a))
		}
	}
	out := gin.H{"status": st, "artifacts": arts}
	if lookup.Run.Graph.Nodes != nil {
		out["nodes"] = graphNodesDTO(lookup.Run.Graph)
	}
	c.JSON(http.StatusOK, out)
}

// PublicGateArtifactContent returns one artifact's full content for a valid
// ShareLinkKindReview token (name path segment, not internal id).
func (h *Handlers) PublicGateArtifactContent(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	lookup, st, ok := h.publicReviewShareLookup(c)
	if !ok {
		return
	}
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_name"})
		return
	}
	if st != models.ShareLinkStateActive {
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	if h.Arts == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	a, ok := h.Arts.GetRecord(lookup.Link.RunID, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	content := a.Content
	etag := engine.ArtifactETag(content, a.SizeBytes, a.UpdatedAt)
	rev := a.Revision
	if rev < 1 {
		rev = 1
	}
	out := gin.H{
		"id": a.ID, "name": a.Name, "kind": a.Kind, "nodeId": a.NodeID,
		"sizeBytes": a.SizeBytes, "createdAt": a.CreatedAt, "content": content,
		"etag": etag, "revision": rev,
	}
	if !a.UpdatedAt.IsZero() {
		out["updatedAt"] = a.UpdatedAt
	}
	c.Header("ETag", etag)
	c.JSON(http.StatusOK, out)
}

// publicReviewShareLookup validates a review share or drawer bearer token for
// artifact routes. human_gate and other kinds are rejected without leaking
// artifact scope. Drawer tokens are read-only here (decide / ticket keep
// ValidTokenShape).
func (h *Handlers) publicReviewShareLookup(c *gin.Context) (*gateshare.LookupResult, string, bool) {
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return nil, "", false
	}
	token := strings.TrimSpace(c.GetHeader(headerShareToken))
	if token == "" || !gateshare.ValidCredentialShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return nil, "", false
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st == models.ShareLinkStateNone {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return nil, "", false
	}
	if publicShareKind(lookup) != models.ShareLinkKindReview {
		c.JSON(http.StatusForbidden, gin.H{"error": "not_review_share"})
		return nil, "", false
	}
	return lookup, st, true
}

// publicArtifactMetaDTO shapes artifact metadata for public review share lists.
// Content and run-scoped identifiers are omitted (aligned with preview DTO).
func publicArtifactMetaDTO(a models.Artifact) gin.H {
	rev := a.Revision
	if rev < 1 {
		rev = 1
	}
	out := gin.H{
		"id": a.ID, "name": a.Name, "kind": a.Kind, "nodeId": a.NodeID,
		"sizeBytes": a.SizeBytes, "createdAt": a.CreatedAt, "revision": rev,
	}
	if !a.UpdatedAt.IsZero() {
		out["updatedAt"] = a.UpdatedAt
	}
	return out
}
