package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/gin-gonic/gin"
)

// LiveSessions lists a node's Live variant sessions (newest first) and
// whether Live is on for it.
func (h *Handlers) LiveSessions(c *gin.Context) {
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	c.JSON(http.StatusOK, liveSessionsBody(h, runID, nodeID))
}

// LiveDiscardAll queues a discard for every open Live session on the node.
func (h *Handlers) LiveDiscardAll(c *gin.Context) {
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	n, err := h.Eng.DiscardAllLiveAs(sessionTurnOwner(c), runID, nodeID)
	if err != nil {
		writeReactReplyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "discarding": n})
}

func liveSessionsBody(h *Handlers, runID, nodeID string) gin.H {
	sessions := h.Eng.LiveSessions(runID, nodeID, false)
	if sessions == nil {
		sessions = []models.LiveSession{}
	}
	return gin.H{"enabled": h.Eng.LiveEnabled(runID, nodeID), "sessions": sessions}
}

// publicLiveLookup resolves an active review share/embed credential with
// reply permission, writing the error response itself when it fails.
func (h *Handlers) publicLiveLookup(c *gin.Context, token string) (*gateshare.LookupResult, bool) {
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return nil, false
	}
	token = strings.TrimSpace(token)
	if token == "" || !gateshare.ValidCredentialShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return nil, false
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st != models.ShareLinkStateActive {
		if st == "" {
			st = "invalid"
		}
		c.JSON(http.StatusOK, gin.H{"status": st})
		return nil, false
	}
	if publicShareKind(lookup) != models.ShareLinkKindReview {
		c.JSON(http.StatusOK, gin.H{"status": "active", "enabled": false, "sessions": []models.LiveSession{}})
		return nil, false
	}
	return lookup, true
}

// PublicLiveSessions is LiveSessions for a share link or drawer credential.
func (h *Handlers) PublicLiveSessions(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	lookup, ok := h.publicLiveLookup(c, c.GetHeader(headerShareToken))
	if !ok {
		return
	}
	body := liveSessionsBody(h, lookup.Link.RunID, lookup.Link.NodeID)
	body["status"] = "active"
	c.JSON(http.StatusOK, body)
}

// PublicLiveDiscardAll is LiveDiscardAll for a share link or drawer credential.
func (h *Handlers) PublicLiveDiscardAll(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf", "message": "请求未通过安全校验"})
		return
	}
	var body publicCancelBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	lookup, ok := h.publicLiveLookup(c, body.Token)
	if !ok {
		return
	}
	if !gateshare.Allow(lookup.Link.PermissionPreset, gateshare.ActionReply) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission_denied", "message": "当前链接权限不允许回复"})
		return
	}
	n, err := h.Eng.DiscardAllLiveAs(h.publicTurnOwner(body.Token), lookup.Link.RunID, lookup.Link.NodeID)
	if err != nil {
		h.writePublicReactErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "discarding": n})
}
