package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"

	"github.com/gin-gonic/gin"
)

const (
	headerShareToken   = "X-Gate-Share-Token"
	headerShareRequest = "X-Gate-Share-Requested"
)

type publicDecideBody struct {
	Token   string `json:"token"`
	Action  string `json:"action"`
	Comment string `json:"comment"`
	Name    string `json:"name"`
	Nonce   string `json:"nonce"`
}

type publicReplyBody struct {
	Token       string                   `json:"token"`
	Text        string                   `json:"text"`
	Annotations []models.ReactAnnotation `json:"annotations"`
	Images      []models.PromptImage     `json:"images"`
}

type publicCancelBody struct {
	Token string `json:"token"`
}

type publicQueueItemBody struct {
	Token  string `json:"token"`
	ItemID string `json:"itemId"`
}

type publicQueueReorderBody struct {
	Token   string   `json:"token"`
	ItemIDs []string `json:"itemIds"`
}

func (h *Handlers) publicRateLimit(c *gin.Context, bucket string) bool {
	if h.GateShareLimiter == nil {
		return true
	}
	if h.GateShareLimiter.AllowBucket(c.ClientIP(), bucket) {
		return true
	}
	c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate_limited", "message": "请求过于频繁，请稍后再试"})
	return false
}

func (h *Handlers) PublicGatePreview(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	token := strings.TrimSpace(c.GetHeader(headerShareToken))
	if token == "" || !gateshare.ValidTokenShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st == models.ShareLinkStateNone {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	silentPoll := strings.EqualFold(strings.TrimSpace(c.GetHeader(gateshare.HeaderSilentPoll)), "1")
	issueNonce := strings.EqualFold(strings.TrimSpace(c.GetHeader(gateshare.HeaderIssueNonce)), "1")
	nonce := ""
	if gateshare.WantPreviewNonce(silentPoll, issueNonce) {
		var err error
		nonce, err = h.GateShareNonces.Issue(lookup.Link.TokenHash)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "unavailable"})
			return
		}
	}
	kind := publicShareKind(lookup)
	if st != models.ShareLinkStateActive {
		out := gin.H{"status": st, "kind": kind}
		if nonce != "" {
			out["nonce"] = nonce
		}
		c.JSON(http.StatusOK, out)
		return
	}
	known := gateshare.SparsePreviewKnown{
		VisualHTML: strings.TrimSpace(c.GetHeader(gateshare.HeaderKnownVisualHTMLHash)),
		Upstream:   strings.TrimSpace(c.GetHeader(gateshare.HeaderKnownUpstreamHash)),
		Structured: strings.TrimSpace(c.GetHeader(gateshare.HeaderKnownStructuredHash)),
		Turns:      strings.TrimSpace(c.GetHeader(gateshare.HeaderKnownTurnsHash)),
	}
	if kind == models.ShareLinkKindReview {
		visual, structName, structContent := h.publicReviewArtifacts(lookup)
		extras := h.publicReviewExtras(lookup, visual, structName)
		dto := gateshare.BuildReviewPreviewDTO(st, lookup, visual, structName, structContent, nonce, extras)
		gateshare.ApplySparsePreview(&dto, known)
		c.JSON(http.StatusOK, dto)
		return
	}
	visual, structName, structContent := h.publicGateArtifacts(lookup)
	extras := h.publicGateExtras(lookup, visual, structName)
	dto := gateshare.BuildPreviewDTO(st, lookup, visual, structName, structContent, nonce, extras)
	gateshare.ApplySparsePreview(&dto, known)
	c.JSON(http.StatusOK, dto)
}

// PublicGateUpstream returns the sanitized full upstream doc (≤64KiB) on demand.
// Open/poll preview never embeds upstream.doc; enlarge/expand uses this route.
func (h *Handlers) PublicGateUpstream(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	token := strings.TrimSpace(c.GetHeader(headerShareToken))
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
	kind := publicShareKind(lookup)
	var structName string
	if kind == models.ShareLinkKindReview {
		_, structName, _ = h.publicReviewArtifacts(lookup)
	} else {
		_, structName, _ = h.publicGateArtifacts(lookup)
	}
	upName, upContent := h.publicUpstreamArtifact(lookup.Link.RunID, structName)
	up := gateshare.SanitizeUpstream(upName, upContent)
	if up == nil {
		c.JSON(http.StatusOK, gin.H{"status": "active", "upstream": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "active", "upstream": up})
}

func (h *Handlers) PublicGateReply(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf", "message": "请求未通过安全校验"})
		return
	}
	var body publicReplyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" || !gateshare.ValidTokenShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st != models.ShareLinkStateActive {
		if st == "" {
			st = "invalid"
		}
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	if !gateshare.Allow(lookup.Link.PermissionPreset, gateshare.ActionReply) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission_denied", "message": "当前链接权限不允许回复"})
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" && len(body.Annotations) == 0 && len(body.Images) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty_reply", "message": "请填写修订意见或标注后再发送"})
		return
	}
	kind := publicShareKind(lookup)
	if kind == models.ShareLinkKindReview {
		if err := h.Eng.ReactReply(lookup.Link.RunID, lookup.Link.NodeID, text, body.Images, body.Annotations, false); err != nil {
			h.writePublicReactErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "accepted", "kind": models.ShareLinkKindReview})
		return
	}
	if err := h.Eng.GateReactRevise(lookup.Link.RunID, lookup.Link.NodeID, text, body.Images, body.Annotations); err != nil {
		h.writePublicReactErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "accepted", "kind": models.ShareLinkKindHumanGate})
}

func (h *Handlers) PublicGateCancel(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
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
	token := strings.TrimSpace(body.Token)
	if token == "" || !gateshare.ValidTokenShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st != models.ShareLinkStateActive {
		if st == "" {
			st = "invalid"
		}
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	if !gateshare.Allow(lookup.Link.PermissionPreset, gateshare.ActionCancel) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission_denied", "message": "当前链接权限不允许取消会话"})
		return
	}
	kind := publicShareKind(lookup)
	if kind == models.ShareLinkKindReview {
		var err error
		if lookup.Node != nil && nodereg.ClarifyInteractive(lookup.Node.Type) {
			err = h.Eng.CancelClarifyTurn(lookup.Link.RunID, lookup.Link.NodeID)
		} else {
			err = h.Eng.CancelReviewSession(lookup.Link.RunID, lookup.Link.NodeID)
		}
		if err != nil {
			h.writePublicReactErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "kind": models.ShareLinkKindReview})
		return
	}
	producerID, alive := h.Eng.GateReactInfo(lookup.Link.RunID, lookup.Link.NodeID)
	if producerID == "" || !alive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_cold", "message": "上游会话已结束，无法取消"})
		return
	}
	if err := h.Eng.CancelReviewSession(lookup.Link.RunID, producerID); err != nil {
		h.writePublicReactErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "kind": models.ShareLinkKindHumanGate})
}

func (h *Handlers) publicShareQueueTarget(lookup *gateshare.LookupResult) (runID, nodeID string, err error) {
	kind := publicShareKind(lookup)
	if kind == models.ShareLinkKindReview {
		return lookup.Link.RunID, lookup.Link.NodeID, nil
	}
	producerID, alive := h.Eng.GateReactInfo(lookup.Link.RunID, lookup.Link.NodeID)
	if producerID == "" || !alive {
		return "", "", errors.New("上游复审会话不可用")
	}
	return lookup.Link.RunID, producerID, nil
}

func (h *Handlers) PublicGateQueueRemove(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf", "message": "请求未通过安全校验"})
		return
	}
	var body publicQueueItemBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" || !gateshare.ValidTokenShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st != models.ShareLinkStateActive {
		if st == "" {
			st = "invalid"
		}
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	if !gateshare.Allow(lookup.Link.PermissionPreset, gateshare.ActionReply) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission_denied", "message": "当前链接权限不允许回复"})
		return
	}
	runID, nodeID, err := h.publicShareQueueTarget(lookup)
	if err != nil {
		h.writePublicReactErr(c, err)
		return
	}
	if err := h.Eng.RemoveQueuedItem(runID, nodeID, body.ItemID); err != nil {
		h.writePublicReactErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "kind": publicShareKind(lookup)})
}

func (h *Handlers) PublicGateQueueReorder(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf", "message": "请求未通过安全校验"})
		return
	}
	var body publicQueueReorderBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" || !gateshare.ValidTokenShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st != models.ShareLinkStateActive {
		if st == "" {
			st = "invalid"
		}
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	if !gateshare.Allow(lookup.Link.PermissionPreset, gateshare.ActionReply) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission_denied", "message": "当前链接权限不允许回复"})
		return
	}
	runID, nodeID, err := h.publicShareQueueTarget(lookup)
	if err != nil {
		h.writePublicReactErr(c, err)
		return
	}
	if err := h.Eng.ReorderQueuedItems(runID, nodeID, body.ItemIDs); err != nil {
		h.writePublicReactErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "kind": publicShareKind(lookup)})
}

func (h *Handlers) writePublicReactErr(c *gin.Context, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "already done") || strings.Contains(msg, "react already done") {
		c.JSON(http.StatusOK, gin.H{"status": "used", "error": "already_done", "message": "会话已结束"})
		return
	}
	if strings.Contains(msg, "上游会话已不存在") || strings.Contains(msg, "cold") {
		c.JSON(http.StatusOK, gin.H{"status": "cold", "error": "session_cold", "message": "会话已结束，仅可确认并流转"})
		return
	}
	_ = c.Error(err)
	c.JSON(http.StatusBadRequest, gin.H{"error": "react_failed", "message": msg})
}

func (h *Handlers) PublicGateDecide(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketDecide) {
		return
	}
	if h.GateShare == nil || h.Eng == nil || h.GateShareNonces == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf", "message": "请求未通过安全校验"})
		return
	}
	var body publicDecideBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" || !gateshare.ValidTokenShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	if st != models.ShareLinkStateActive && st != models.ShareLinkStateUsed {
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	// Enforce link preset before nonce CAS / ConsumeCAS so react_only cannot
	// burn the one-shot token or advance the gate via direct decide.
	if st == models.ShareLinkStateActive && !gateshare.Allow(lookup.Link.PermissionPreset, gateshare.ActionDecide) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "permission_denied",
			"message": "当前链接权限为仅 ReAct，禁止确认或驳回流转",
		})
		return
	}
	kind := publicShareKind(lookup)
	comment, name := "", ""
	if kind != models.ShareLinkKindReview && st == models.ShareLinkStateActive {
		var ok bool
		comment, ok = gateshare.ClampComment(body.Comment)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "comment_too_long"})
			return
		}
		name, ok = gateshare.ClampExternalName(body.Name)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name_too_long"})
			return
		}
		if comment == "" || name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "audit_required", "message": "请填写姓名与意见后再提交"})
			return
		}
	}
	if !h.GateShareNonces.Consume(lookup.Link.TokenHash, strings.TrimSpace(body.Nonce)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "nonce", "message": "请求未通过安全校验"})
		return
	}
	if kind == models.ShareLinkKindReview {
		h.publicReviewDecide(c, lookup, token, strings.TrimSpace(body.Action))
		return
	}
	action := strings.TrimSpace(body.Action)
	res, err := h.Eng.ResumeGateExternal(h.GateShare, token, action, comment, name)
	if err != nil {
		if errors.Is(err, gateshare.ErrAuditRequired) || errors.Is(err, gateshare.ErrCommentRequired) || errors.Is(err, gateshare.ErrNameRequired) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "audit_required", "message": "请填写姓名与意见后再提交"})
			return
		}
		if errors.Is(err, gateshare.ErrActionConflict) && res != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "conflict", "status": "used", "action": res.Action})
			return
		}
		if errors.Is(err, gateshare.ErrNotActive) && res != nil {
			c.JSON(http.StatusOK, gin.H{"status": res.Status})
			return
		}
		if errors.Is(err, gateshare.ErrTokenInvalid) {
			c.JSON(http.StatusOK, gin.H{"status": "invalid"})
			return
		}
		if errors.Is(err, gateshare.ErrNoStandardAction) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_action"})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "submit_failed"})
		return
	}
	if res.Link != nil {
		h.GateShare.RecordUseAudit(
			res.Link.RunID, res.Link.NodeID, res.Action, name,
			gateshare.MaskIP(c.ClientIP()), gateshare.SummarizeUA(c.Request.UserAgent()),
			res.Link.CreatedAt, res.Link.ExpiresAt, res.Link.RevokedAt, res.Link.UsedAt,
		)
	}
	out := gin.H{"status": res.Status, "action": res.Action}
	if res.AlreadyProcessed {
		out["alreadyProcessed"] = true
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) PublicGateApprovalPage(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	c.Header("Content-Type", "text/html; charset=utf-8")
	b, err := os.ReadFile("./web/dist/index.html")
	if err != nil {
		// Missing dist (tests / fresh checkout): still emit security headers.
		c.String(http.StatusOK, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Grasp</title></head><body></body></html>")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", b)
}

func publicShareKind(lookup *gateshare.LookupResult) string {
	if lookup == nil {
		return models.ShareLinkKindHumanGate
	}
	if strings.TrimSpace(lookup.Kind) == models.ShareLinkKindReview {
		return models.ShareLinkKindReview
	}
	if strings.TrimSpace(lookup.Link.Kind) == models.ShareLinkKindReview {
		return models.ShareLinkKindReview
	}
	return models.ShareLinkKindHumanGate
}

func (h *Handlers) publicReviewDecide(c *gin.Context, lookup *gateshare.LookupResult, token, action string) {
	if action == "" {
		action = "confirm"
	}
	if action != "confirm" && action != "pass" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_action", "message": "复审公开页仅支持确认并流转"})
		return
	}
	res, err := h.Eng.ResumeReviewExternal(h.GateShare, token, action)
	if err != nil {
		if errors.Is(err, gateshare.ErrReviewBusy) {
			c.JSON(http.StatusOK, gin.H{"status": "busy", "error": "review_busy", "message": "复审进行中，请稍后再试"})
			return
		}
		if errors.Is(err, gateshare.ErrReviewValidation) {
			c.JSON(http.StatusOK, gin.H{"status": "validation_failed", "error": "review_validation_failed", "message": "产物校验未通过，链接仍有效，请稍后重试"})
			return
		}
		if errors.Is(err, gateshare.ErrActionConflict) && res != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "conflict", "status": "used", "action": res.Action})
			return
		}
		if errors.Is(err, gateshare.ErrNotActive) && res != nil {
			c.JSON(http.StatusOK, gin.H{"status": res.Status})
			return
		}
		if errors.Is(err, gateshare.ErrTokenInvalid) {
			c.JSON(http.StatusOK, gin.H{"status": "invalid"})
			return
		}
		if errors.Is(err, gateshare.ErrNoStandardAction) || errors.Is(err, gateshare.ErrNotReviewSession) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_action"})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "submit_failed"})
		return
	}
	if res.Link != nil {
		h.GateShare.RecordUseAudit(
			res.Link.RunID, res.Link.NodeID, res.Action, "",
			gateshare.MaskIP(c.ClientIP()), gateshare.SummarizeUA(c.Request.UserAgent()),
			res.Link.CreatedAt, res.Link.ExpiresAt, res.Link.RevokedAt, res.Link.UsedAt,
		)
	}
	out := gin.H{"status": res.Status, "action": res.Action, "kind": models.ShareLinkKindReview}
	if res.AlreadyProcessed {
		out["alreadyProcessed"] = true
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) publicReviewArtifacts(lookup *gateshare.LookupResult) (visualHTML, structName, structContent string) {
	if lookup == nil || h.Arts == nil || lookup.Node == nil {
		return "", "", ""
	}
	spec, ok := nodereg.Get(lookup.Node.Type)
	name := strings.TrimSpace(spec.ArtifactName)
	if !ok || name == "" {
		return "", "", ""
	}
	a, ok := h.Arts.GetRecord(lookup.Link.RunID, name)
	if !ok {
		return "", "", ""
	}
	lower := strings.ToLower(name)
	if lower == "page.html" || strings.HasSuffix(lower, ".html") {
		return a.Content, "", ""
	}
	return "", name, a.Content
}

func (h *Handlers) publicReviewExtras(lookup *gateshare.LookupResult, visualHTML, structName string) gateshare.PreviewExtras {
	ex := gateshare.PreviewExtras{}
	if lookup == nil {
		return ex
	}
	runID, nodeID := lookup.Link.RunID, lookup.Link.NodeID
	if lookup.Node != nil && lookup.Node.Type == "app_preview" {
		ex.ProductKind = gateshare.ProductKindAppPreview
		ex.ProductName = "app_preview"
		ex.Ports = h.publicAppPreviewPorts(runID, nodeID)
	} else if strings.TrimSpace(visualHTML) != "" {
		ex.ProductKind = gateshare.ProductKindVisual
		ex.ProductName = "page.html"
	} else if strings.TrimSpace(structName) != "" {
		ex.ProductKind = gateshare.ProductKindStructured
		ex.ProductName = structName
	}
	if lookup.Node != nil && nodereg.IsGrasp(lookup.Node.Type) {
		ex.Ports = h.publicAppPreviewPorts(runID, nodeID)
	}
	if conv := h.publicConversation(runID, nodeID); conv != nil {
		ex.Turns = conv.Turns()
	}
	ex.ReactSessionAlive = h.Eng != nil && h.Eng.HasLiveReviewSession(runID, nodeID)
	if ex.ReactSessionAlive && h.Eng != nil {
		if snap, ok := h.Eng.ReviewSessionSnapshotFor(runID, nodeID); ok {
			ex.Waiting = snap.Waiting
			ex.QueueItems = snap.Items
			ex.ActiveItem = snap.ActiveItem
			ex.SessionBusy = snap.Busy || snap.Waiting > 0 || !h.Eng.ReviewSessionReady(runID, nodeID)
		} else {
			waiting, thinking := h.Eng.ReviewSessionState(runID, nodeID)
			ex.Waiting = waiting
			ex.SessionBusy = thinking || waiting > 0 || !h.Eng.ReviewSessionReady(runID, nodeID)
		}
		if ex.SessionBusy {
			ex.LiveEvents = h.publicLiveACP(runID, nodeID)
		}
	}
	ex.UpstreamName, ex.UpstreamContent = h.publicUpstreamArtifact(runID, structName)
	return ex
}

func (h *Handlers) publicGateExtras(lookup *gateshare.LookupResult, visualHTML, structName string) gateshare.PreviewExtras {
	ex := gateshare.PreviewExtras{}
	if lookup == nil {
		return ex
	}
	runID, gateID := lookup.Link.RunID, lookup.Link.NodeID
	if strings.TrimSpace(visualHTML) != "" {
		ex.ProductKind = gateshare.ProductKindVisual
		ex.ProductName = "page.html"
	} else if strings.TrimSpace(structName) != "" {
		ex.ProductKind = gateshare.ProductKindStructured
		ex.ProductName = structName
	}
	producerID, alive := "", false
	if h.Eng != nil {
		producerID, alive = h.Eng.GateReactInfo(runID, gateID)
	}
	if producerID == "" {
		producerID = h.publicGateProducerID(lookup)
	}
	if producerID != "" {
		if conv := h.publicConversation(runID, producerID); conv != nil {
			ex.Turns = conv.Turns()
		}
		if lookup.Run.Graph.FindNode(producerID) != nil && lookup.Run.Graph.FindNode(producerID).Type == "app_preview" {
			ex.ProductKind = gateshare.ProductKindAppPreview
			if ex.ProductName == "" {
				ex.ProductName = "app_preview"
			}
		}
	}
	ex.ReactSessionAlive = alive
	if alive && h.Eng != nil && producerID != "" {
		if snap, ok := h.Eng.ReviewSessionSnapshotFor(runID, producerID); ok {
			ex.Waiting = snap.Waiting
			ex.QueueItems = snap.Items
			ex.ActiveItem = snap.ActiveItem
			ex.SessionBusy = snap.Busy || snap.Waiting > 0 || !h.Eng.ReviewSessionReady(runID, producerID)
		} else {
			waiting, thinking := h.Eng.ReviewSessionState(runID, producerID)
			ex.Waiting = waiting
			ex.SessionBusy = thinking || waiting > 0 || !h.Eng.ReviewSessionReady(runID, producerID)
		}
		if ex.SessionBusy {
			ex.LiveEvents = h.publicLiveACP(runID, producerID)
		}
	}
	ex.UpstreamName, ex.UpstreamContent = h.publicUpstreamArtifact(runID, structName)
	return ex
}

func (h *Handlers) publicGateProducerID(lookup *gateshare.LookupResult) string {
	if lookup == nil || lookup.Node == nil {
		return ""
	}
	up := strings.TrimSpace(lookup.Gate.UpstreamNodeID)
	if up != "" {
		return up
	}
	return ""
}

func (h *Handlers) publicDialogueProducerID(lookup *gateshare.LookupResult) string {
	if lookup == nil {
		return ""
	}
	if publicShareKind(lookup) == models.ShareLinkKindReview {
		return strings.TrimSpace(lookup.Link.NodeID)
	}
	if h.Eng != nil {
		if id, _ := h.Eng.GateReactInfo(lookup.Link.RunID, lookup.Link.NodeID); strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	return h.publicGateProducerID(lookup)
}

func (h *Handlers) publicLiveACP(runID, nodeID string) []models.AcpEvent {
	if h.Eng == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	ev, ok, err := h.Eng.LiveNodeEvents(context.Background(), runID, nodeID)
	if err != nil || !ok || len(ev) == 0 {
		return nil
	}
	return ev
}

func (h *Handlers) publicConversation(runID, nodeID string) *models.ReactConversation {
	if h.Runs == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	var conv models.ReactConversation
	if err := h.Runs.DB().Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return nil
	}
	return &conv
}

func (h *Handlers) publicUpstreamArtifact(runID, primaryName string) (name, content string) {
	const upstreamName = "clarified_requirement.json"
	if h.Arts == nil || strings.TrimSpace(runID) == "" {
		return "", ""
	}
	if strings.EqualFold(strings.TrimSpace(primaryName), upstreamName) {
		return "", ""
	}
	a, ok := h.Arts.GetRecord(runID, upstreamName)
	if !ok || strings.TrimSpace(a.Content) == "" {
		return "", ""
	}
	return upstreamName, a.Content
}

func (h *Handlers) publicGateArtifacts(lookup *gateshare.LookupResult) (visualHTML, structName, structContent string) {
	if lookup == nil || h.Arts == nil || h.Eng == nil {
		return "", "", ""
	}
	// Only this human_gate's primary products (body_template / upstream produces
	// whitelist). Never scan the whole Run — other nodes' page.html / research.json
	// must not leak to an unauthenticated holder.
	items, err := h.Eng.ListGatePrimaryProducts(lookup.Link.RunID, lookup.Link.NodeID)
	if err != nil {
		return "", "", ""
	}
	for _, it := range items {
		a, ok := h.Arts.GetRecord(lookup.Link.RunID, it.Name)
		if !ok {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(it.Kind))
		name := strings.ToLower(it.Name)
		if kind == "html" || name == "page.html" || strings.HasSuffix(name, ".html") {
			if visualHTML == "" {
				visualHTML = a.Content
			}
			continue
		}
		if structContent == "" && (kind == "json" || strings.HasSuffix(name, ".json")) {
			structName = it.Name
			structContent = a.Content
		}
	}
	return visualHTML, structName, structContent
}

func (h *Handlers) checkPublicCSRF(c *gin.Context) bool {
	if strings.TrimSpace(c.GetHeader(headerShareRequest)) == "" {
		return false
	}
	host := h.trustedPublicHost(c)
	if host == "" {
		return false
	}
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, host)
	}
	ref := strings.TrimSpace(c.GetHeader("Referer"))
	if ref != "" {
		u, err := url.Parse(ref)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, host)
	}
	// Embedded WebView may omit Origin/Referer; accept same-origin Fetch Metadata only.
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("Sec-Fetch-Site")), "same-origin") {
		return true
	}
	return false
}

func applyPublicSecurityHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Content-Security-Policy", publicGateCSP)
	c.Writer.Header().Del("Access-Control-Allow-Origin")
	c.Writer.Header().Del("Access-Control-Allow-Methods")
	c.Writer.Header().Del("Access-Control-Allow-Headers")
}

const publicGateCSP = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
