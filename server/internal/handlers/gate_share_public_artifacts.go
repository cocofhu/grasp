package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/mcp"
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
			if mcp.IsFeedbackArtifactName(a.Name) {
				continue
			}
			arts = append(arts, publicArtifactMetaDTO(a))
		}
	}
	out := gin.H{"status": st, "artifacts": arts}
	if lookup.Run.Graph.Nodes != nil {
		out["nodes"] = graphNodesDTO(lookup.Run.Graph)
	}
	// The stage draws a read-only workflow for this conversation's node.
	// Status is the latest execution only — never outputs, events, or tokens.
	if id := strings.TrimSpace(lookup.Link.NodeID); id != "" {
		out["nodeId"] = id
	}
	if h.Runs != nil {
		out["nodeRuns"] = publicNodeStatusDTO(h.Runs.States(lookup.Link.RunID))
	}
	c.JSON(http.StatusOK, out)
}

// publicNodeStatusDTO keeps the latest execution status per node. States are
// ordered oldest→newest, so the last row for a node wins. Nodes with no row
// are omitted; the client must not invent "pending" for a missing map.
func publicNodeStatusDTO(states []models.StateRun) gin.H {
	out := gin.H{}
	for _, s := range states {
		id := strings.TrimSpace(s.NodeID)
		status := strings.TrimSpace(s.Status)
		if id == "" || !publicNodeStatusOK(status) {
			continue
		}
		row := gin.H{"nodeId": id, "status": status}
		if s.Iteration > 0 {
			row["iteration"] = s.Iteration
		}
		out[id] = row
	}
	return out
}

func publicNodeStatusOK(status string) bool {
	switch status {
	case "pending", "running", "waiting_human", "completed", "failed", "skipped", "cancelled":
		return true
	default:
		return false
	}
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
	// The feedback ledger stays internal; answer as if it did not exist.
	a, ok := h.Arts.GetRecord(lookup.Link.RunID, name)
	if !ok || mcp.IsFeedbackArtifactName(a.Name) {
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
