package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pagebridge"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

const maxPreviewChatText = 8 << 10

// ManageAppPreviewShare mints or revokes a link to one registered app. Solo
// scope comes from the owned session, never from a caller-supplied run ID.
func (h *Handlers) ManageAppPreviewShare(c *gin.Context) {
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	if h.GateShare == nil || h.Preview == nil || h.MCP == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "preview unavailable"})
		return
	}
	var body struct {
		Port     int    `json:"port"`
		ChatMode string `json:"chatMode"`
	}
	if c.ShouldBindJSON(&body) != nil || body.Port <= 0 || body.Port > 65535 ||
		(body.ChatMode != models.PreviewChatIsolated && body.ChatMode != models.PreviewChatMain) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	if nodeID == "" {
		if h.Solo == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "solo unavailable"})
			return
		}
		sb, err := h.Solo.ResourceSandbox(runID, user)
		if err != nil {
			writeSoloErr(c, err)
			return
		}
		runID, nodeID = sb.RunID, "solo"
	} else if _, ok := h.Runs.Get(runID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	sb, ok := h.appPreviewSandbox(runID, nodeID, body.Port)
	if !ok || (sb.Purpose == services.SandboxPurposeSolo && c.Param("nodeId") != "") {
		c.JSON(http.StatusNotFound, gin.H{"error": "preview not registered"})
		return
	}
	if c.Request.Method == http.MethodDelete {
		if err := h.GateShare.RevokeAppPreview(runID, nodeID, sb.ID, body.Port, body.ChatMode); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "revoke failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if h.directAppPreviewURL(runID, nodeID, body.Port) == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "no direct preview"})
		return
	}
	iteration := 0
	if body.ChatMode == models.PreviewChatMain && sb.Purpose != services.SandboxPurposeSolo {
		state, active := h.workflowPreviewDialogue(runID, nodeID)
		if !active {
			c.JSON(http.StatusConflict, gin.H{"code": "main_chat_unavailable", "error": "no active main conversation"})
			return
		}
		iteration = state.Iteration
	}
	result, err := h.GateShare.CreateAppPreview(runID, nodeID, sb.ID, body.Port, iteration, body.ChatMode, user, h.shareOrigin(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) directAppPreviewURL(runID, nodeID string, port int) string {
	for _, reg := range h.MCP.ListPreviewPorts(runID, nodeID) {
		if reg.Port == port && reg.Mode == "direct" && originOf(reg.DirectURL) != "" {
			return reg.DirectURL
		}
	}
	return ""
}

func (h *Handlers) appPreviewSandbox(runID, nodeID string, port int) (models.Sandbox, bool) {
	if h.Preview == nil || h.MCP == nil {
		return models.Sandbox{}, false
	}
	sb, ok := h.Preview.PreviewSandbox(runID, nodeID)
	if !ok || sb.Status != "running" {
		return models.Sandbox{}, false
	}
	for _, reg := range h.MCP.ListPreviewPorts(runID, nodeID) {
		if reg.Port == port && reg.Kind != "url" && (reg.SandboxName == "" || reg.SandboxName == sb.Name) &&
			(reg.Host != "" || reg.SandboxName != "") {
			return sb, true
		}
	}
	return models.Sandbox{}, false
}

func (h *Handlers) sharedAppPreviewSandbox(link *models.GateShareLink) (models.Sandbox, bool) {
	sb, ok := h.appPreviewSandbox(link.RunID, link.NodeID, link.PreviewPort)
	if !ok || sb.ID != link.PreviewSandboxID {
		return models.Sandbox{}, false
	}
	if sb.Purpose == services.SandboxPurposeSolo {
		_, ok = h.Preview.SoloPreviewSession(link.RunID, link.CreatedBy)
	}
	return sb, ok
}

func (h *Handlers) publicAppShare(c *gin.Context) (*models.GateShareLink, models.Sandbox) {
	if h.GateShare == nil || !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "unavailable"})
		return nil, models.Sandbox{}
	}
	token := c.GetHeader(headerShareToken)
	link, ok := h.GateShare.LookupAppPreview(token)
	if !ok && h.Embed != nil {
		if claims, found := h.Embed.LookupSession(token); found && claims.Kind == models.EmbedKindAppPreview {
			link, ok = h.GateShare.LookupAppPreviewByHash(claims.ShareTokenHash)
			ok = ok && link.RunID == claims.RunID && link.NodeID == claims.NodeID
		}
	}
	if ok {
		if sb, active := h.sharedAppPreviewSandbox(link); active {
			return link, sb
		}
	}
	c.JSON(http.StatusGone, gin.H{"error": "share link inactive"})
	return nil, models.Sandbox{}
}

// PublicAppPreviewOpen exchanges the share link for a one-shot drawer ticket.
// Only the short-lived ticket crosses to the app origin.
func (h *Handlers) PublicAppPreviewOpen(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketTicket) {
		return
	}
	if h.GateShare == nil || h.Embed == nil || !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "unavailable"})
		return
	}
	link, ok := h.GateShare.LookupAppPreview(c.GetHeader(headerShareToken))
	if !ok {
		c.JSON(http.StatusGone, gin.H{"error": "share link inactive"})
		return
	}
	if _, active := h.sharedAppPreviewSandbox(link); !active {
		c.JSON(http.StatusGone, gin.H{"error": "share link inactive"})
		return
	}
	direct := h.directAppPreviewURL(link.RunID, link.NodeID, link.PreviewPort)
	if direct == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "no direct preview"})
		return
	}
	claims := embed.Claims{
		Kind: models.EmbedKindAppPreview, RunID: link.RunID, NodeID: link.NodeID,
		ShareTokenHash: link.TokenHash, GraspOrigin: h.embedGraspOrigin(c),
	}
	ticket, exp, err := h.Embed.IssueTicket(claims)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue failed"})
		return
	}
	c.JSON(http.StatusOK, struct {
		embedTicketResponse
		DirectURL string `json:"directUrl"`
	}{embedTicketResponse{Ticket: ticket, RunID: claims.RunID, NodeID: claims.NodeID, ExpiresAt: exp}, direct})
}

func (h *Handlers) PublicAppPreviewSession(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketTicket) {
		return
	}
	link, _ := h.publicAppShare(c)
	if link == nil {
		return
	}
	if h.GateShareTickets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	if c.ShouldBindJSON(&body) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	ticket := body.Ticket
	if !h.GateShareTickets.Refresh(ticket, link.TokenHash) {
		issued, _, err := h.GateShareTickets.Issue(link.TokenHash, link.RunID, link.NodeID, link.PreviewPort, gateshare.PreviewPurposeAPI)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "unavailable"})
			return
		}
		ticket = issued
	}
	c.JSON(http.StatusOK, gin.H{
		"ticket": ticket, "chatMode": link.PreviewChatMode,
		"iframePath": fmt.Sprintf("/public/approvals/preview-api/%s/", ticket),
	})
}

func (h *Handlers) workflowPreviewDialogue(runID, nodeID string) (engine.PublicDialogueState, bool) {
	run, ok := h.Runs.Get(runID)
	if h.Eng == nil || !ok || run.Status != "waiting_human" || !services.IsShareableReviewSession(h.Runs.EffectiveNode(run, nodeID)) {
		return engine.PublicDialogueState{}, false
	}
	state, ok := h.Eng.PublicDialogueSnapshot(runID, nodeID)
	return state, ok && !state.Done && !state.Observe && state.Iteration > 0
}

type sharedPreviewRow struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Text string `json:"text"`
}

// PublicAppPreviewMainChat shares the original transcript and turn queue.
// Its credential cannot confirm a workflow, edit settings or access other APIs.
func (h *Handlers) PublicAppPreviewMainChat(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	link, sb := h.publicAppShare(c)
	if link == nil {
		return
	}
	if link.PreviewChatMode != models.PreviewChatMain {
		c.JSON(http.StatusForbidden, gin.H{"error": "main conversation is private"})
		return
	}
	solo := sb.Purpose == services.SandboxPurposeSolo
	var state engine.PublicDialogueState
	if !solo {
		var active bool
		state, active = h.workflowPreviewDialogue(link.RunID, link.NodeID)
		if !active || state.Iteration != link.Iteration {
			c.JSON(http.StatusConflict, gin.H{"error": "main conversation unavailable"})
			return
		}
	} else if h.Solo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if c.Request.Method == http.MethodGet {
		rows := []sharedPreviewRow{}
		var busy bool
		if solo {
			messages, active, err := h.Solo.PreviewTranscript(sb.ThreadID, link.CreatedBy)
			if err != nil {
				writeSoloErr(c, err)
				return
			}
			for _, row := range messages {
				rows = append(rows, sharedPreviewRow{ID: row.ID, Role: row.Role, Text: row.Content})
			}
			busy = active
		} else {
			for _, row := range state.Rows {
				rows = append(rows, sharedPreviewRow{ID: row.ID, Role: row.Role, Text: row.Text})
			}
			busy = state.Busy
		}
		c.JSON(http.StatusOK, gin.H{"rows": rows, "busy": busy})
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if c.ShouldBindJSON(&body) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || len(text) > maxPreviewChatText {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_message"})
		return
	}
	var err error
	if h.Shutdown != nil && h.Shutdown.IsDraining() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if solo {
		_, err = h.Solo.Send(sb.ThreadID, link.CreatedBy, text, nil)
	} else {
		owner := pagebridge.TokenOwner("preview", link.TokenHash)
		err = h.Eng.EnqueuePreviewShareTurn(owner, link.RunID, link.NodeID, text, link.Iteration)
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "message not queued"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handlers) activeAppPreviewTicket(claims *gateshare.PreviewTicketClaims) (*models.GateShareLink, models.Sandbox) {
	if claims == nil || claims.Purpose != gateshare.PreviewPurposeAPI || h.GateShare == nil {
		return nil, models.Sandbox{}
	}
	link, err := h.GateShare.LoadLinkByTokenHash(claims.TokenHash)
	if err != nil || link == nil || link.Kind != models.ShareLinkKindAppPreview ||
		link.RevokedAt != nil || link.UsedAt != nil || !time.Now().Before(link.ExpiresAt) ||
		link.RunID != claims.RunID || link.NodeID != claims.NodeID || link.PreviewPort != claims.Port {
		return nil, models.Sandbox{}
	}
	if sb, ok := h.sharedAppPreviewSandbox(link); ok {
		return link, sb
	}
	return nil, models.Sandbox{}
}
