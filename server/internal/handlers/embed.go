package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// headerEmbedRequest must accompany ticket redemption; a cross-site form
// cannot set it, and a cross-origin fetch that sets it needs a preflight this
// server never grants.
const headerEmbedRequest = "X-Grasp-Embed"

type embedTicketResponse struct {
	Ticket    string    `json:"ticket"`
	RunID     string    `json:"runId"`
	NodeID    string    `json:"nodeId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// directPreviewOrigins lists the scheme://host:port of every direct preview
// registered for the node. Only these pages may frame the chat drawer.
func (h *Handlers) directPreviewOrigins(runID, nodeID string) []string {
	if h.MCP == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range h.MCP.ListPreviewPorts(runID, nodeID) {
		if p.Mode != "direct" {
			continue
		}
		o := originOf(p.DirectURL)
		if o == "" || seen[o] {
			continue
		}
		seen[o] = true
		out = append(out, o)
	}
	return out
}

func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// embedGraspOrigin is the browser-facing origin that minted the ticket. The
// browser sets Origin on this same-origin POST; fall back to Host otherwise.
func (h *Handlers) embedGraspOrigin(c *gin.Context) string {
	if o := originOf(c.GetHeader("Origin")); o != "" {
		return o
	}
	return h.shareOrigin(c)
}

func (h *Handlers) embedTargetReady(runID, nodeID string) (int, string) {
	run, ok := h.Runs.Get(runID)
	if !ok {
		return http.StatusNotFound, "run not found"
	}
	if terminalRunStatus(run.Status) {
		return http.StatusConflict, "run finished"
	}
	n := run.Graph.FindNode(nodeID)
	if n == nil || !mcp.SetPreviewAllowed(n.Type) || !services.IsShareableReviewSession(n) {
		return http.StatusBadRequest, "node has no preview chat"
	}
	if len(h.directPreviewOrigins(runID, nodeID)) == 0 {
		return http.StatusConflict, "no direct preview"
	}
	return 0, ""
}

func terminalRunStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// CreateEmbedTicket mints a drawer ticket for the logged-in user.
// POST /api/runs/:id/nodes/:nodeId/embed-ticket
func (h *Handlers) CreateEmbedTicket(c *gin.Context) {
	if h.Embed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embed unavailable"})
		return
	}
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	if code, msg := h.embedTargetReady(runID, nodeID); code != 0 {
		c.JSON(code, gin.H{"error": msg})
		return
	}
	user := strings.TrimSpace(c.GetString("auth_username"))
	if user == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ticket, exp, err := h.Embed.IssueTicket(embed.Claims{
		Kind:        models.EmbedKindSession,
		RunID:       runID,
		NodeID:      nodeID,
		Username:    user,
		GraspOrigin: h.embedGraspOrigin(c),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue failed"})
		return
	}
	c.JSON(http.StatusOK, embedTicketResponse{Ticket: ticket, RunID: runID, NodeID: nodeID, ExpiresAt: exp})
}

// PublicEmbedTicket mints a drawer ticket for a review share link.
// POST /public/gate-approvals/embed-ticket (share token in X-Gate-Share-Token)
func (h *Handlers) PublicEmbedTicket(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketTicket) {
		return
	}
	if h.GateShare == nil || h.Embed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf", "message": "请求未通过安全校验"})
		return
	}
	token := strings.TrimSpace(c.GetHeader(headerShareToken))
	// Share tokens only: a drawer token must not mint further credentials.
	if token == "" || !gateshare.ValidTokenShape(token) {
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
	if lookup.Kind != models.ShareLinkKindReview || lookup.Node == nil || !mcp.SetPreviewAllowed(lookup.Node.Type) {
		c.JSON(http.StatusForbidden, gin.H{"error": "unsupported", "message": "当前分享链不支持预览页对话"})
		return
	}
	if !gateshare.Allow(lookup.Link.PermissionPreset, gateshare.ActionReply) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission_denied", "message": "当前链接权限不允许回复"})
		return
	}
	runID, nodeID := lookup.Link.RunID, lookup.Link.NodeID
	if len(h.directPreviewOrigins(runID, nodeID)) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "no direct preview"})
		return
	}
	ticket, exp, err := h.Embed.IssueTicket(embed.Claims{
		Kind:           models.EmbedKindShare,
		RunID:          runID,
		NodeID:         nodeID,
		ShareTokenHash: lookup.Link.TokenHash,
		GraspOrigin:    h.embedGraspOrigin(c),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":    "active",
		"ticket":    ticket,
		"runId":     runID,
		"nodeId":    nodeID,
		"expiresAt": exp,
	})
}

// sameOriginRequest reports whether the browser says this request came from a
// page on this host. Origin is required; there is no Referer fallback.
func sameOriginRequest(c *gin.Context) bool {
	u, err := url.Parse(strings.TrimSpace(c.GetHeader("Origin")))
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, strings.TrimSpace(c.Request.Host))
}

type redeemEmbedBody struct {
	Ticket string `json:"ticket"`
}

// RedeemEmbedSession trades a ticket for a drawer bearer token.
// POST /embed-api/session
func (h *Handlers) RedeemEmbedSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.Embed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embed unavailable"})
		return
	}
	if strings.TrimSpace(c.GetHeader(headerEmbedRequest)) != "1" || !sameOriginRequest(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf"})
		return
	}
	var body redeemEmbedBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	claims, token, exp, err := h.Embed.ExchangeTicket(body.Ticket)
	if errors.Is(err, embed.ErrTicketSpent) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_ticket"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":     token,
		"kind":      claims.Kind,
		"runId":     claims.RunID,
		"nodeId":    claims.NodeID,
		"expiresAt": exp,
	})
}

// EmbedChatPage serves the SPA for the drawer. Only the node's registered
// direct preview origins may frame it.
// GET /embed/runs/:runId/nodes/:nodeId/chat
func (h *Handlers) EmbedChatPage(c *gin.Context) {
	ancestors := "'none'"
	if origins := h.directPreviewOrigins(c.Param("runId"), c.Param("nodeId")); len(origins) > 0 {
		ancestors = strings.Join(origins, " ")
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "frame-ancestors "+ancestors)
	b, err := os.ReadFile("./web/dist/index.html")
	if err != nil {
		c.String(http.StatusOK, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Grasp</title></head><body></body></html>")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", b)
}

// MCPEmbedOrigin lets the sandbox learn which Grasp origin serves the drawer
// for a ticket, without consuming it.
// GET /mcp/runs/:runId/embed-origin?ticket=&nodeId=
func (h *Handlers) MCPEmbedOrigin(c *gin.Context) {
	runID := c.Param("runId")
	if h.MCP == nil || !h.MCP.AuthorizeRun(runID, bearer(c.GetHeader("Authorization"))) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.Embed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embed unavailable"})
		return
	}
	claims, ok := h.Embed.PeekTicket(c.Query("ticket"))
	if !ok || claims.RunID != runID {
		c.JSON(http.StatusNotFound, gin.H{"error": "invalid_ticket"})
		return
	}
	if nodeID := strings.TrimSpace(c.Query("nodeId")); nodeID != "" && nodeID != claims.NodeID {
		c.JSON(http.StatusNotFound, gin.H{"error": "invalid_ticket"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"origin": claims.GraspOrigin,
		"runId":  claims.RunID,
		"nodeId": claims.NodeID,
	})
}

// MCPEmbedBoot mints a drawer ticket for the origin that last opened this
// preview from Grasp. The sandbox calls it when someone opens the bare
// preview address, which has no ticket in the URL.
// POST /mcp/runs/:runId/embed-boot?nodeId=
func (h *Handlers) MCPEmbedBoot(c *gin.Context) {
	runID := c.Param("runId")
	if h.MCP == nil || !h.MCP.AuthorizeRun(runID, bearer(c.GetHeader("Authorization"))) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.Embed == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embed unavailable"})
		return
	}
	nodeID := strings.TrimSpace(c.Query("nodeId"))
	if code, msg := h.embedTargetReady(runID, nodeID); code != 0 {
		c.JSON(code, gin.H{"error": msg})
		return
	}
	ticket, _, origin, err := h.Embed.BootTicket(runID, nodeID)
	if err != nil || strings.TrimSpace(ticket) == "" || strings.TrimSpace(origin) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no drawer"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"origin": origin,
		"runId":  runID,
		"nodeId": nodeID,
		"ticket": ticket,
	})
}
