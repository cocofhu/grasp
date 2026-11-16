package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

type createShareBody struct {
	TTLTier          string `json:"ttlTier"`
	PermissionPreset string `json:"permissionPreset"`
}

// shareOrigin mints gate/review share URL origins from this request's Host.
// Never prefer public_advertise (that is only for preview links) and
// never trust client X-Forwarded-Host. Scheme uses TLS / X-Forwarded-Proto.
func (h *Handlers) shareOrigin(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if fwd := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))); fwd == "http" || fwd == "https" {
		scheme = fwd
	}
	return gateshare.PublicOriginFromRequest(scheme, c.Request.Host)
}

// trustedPublicHost is the CSRF comparison host: this request's Host.
// X-Forwarded-Host is ignored (untrusted). public_advertise is never used
// for CSRF origin comparison.
func (h *Handlers) trustedPublicHost(c *gin.Context) string {
	return strings.TrimSpace(c.Request.Host)
}

func (h *Handlers) shareActor(c *gin.Context) string {
	a := h.auditActorFromContext(c)
	if a.Unattributable {
		return ""
	}
	return a.Username
}

func (h *Handlers) CreateGateShareLink(c *gin.Context) {
	if h.GateShare == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "share unavailable"})
		return
	}
	var body createShareBody
	_ = c.ShouldBindJSON(&body)
	res, err := h.GateShare.Create(c.Param("id"), c.Param("nodeId"), body.TTLTier, body.PermissionPreset, h.shareActor(c), h.shareOrigin(c))
	if err != nil {
		writeShareErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handlers) GetGateShareLink(c *gin.Context) {
	if h.GateShare == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "share unavailable"})
		return
	}
	st, err := h.GateShare.Status(c.Param("id"), c.Param("nodeId"))
	if err != nil && !errors.Is(err, gateshare.ErrGateNotPending) && !errors.Is(err, gateshare.ErrRunEnded) && !errors.Is(err, gateshare.ErrUsedReadonly) {
		writeShareErr(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *Handlers) RegenGateShareLink(c *gin.Context) {
	if h.GateShare == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "share unavailable"})
		return
	}
	res, err := h.GateShare.Regenerate(c.Param("id"), c.Param("nodeId"), h.shareActor(c), h.shareOrigin(c))
	if err != nil {
		writeShareErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handlers) RevokeGateShareLink(c *gin.Context) {
	if h.GateShare == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "share unavailable"})
		return
	}
	if err := h.GateShare.Revoke(c.Param("id"), c.Param("nodeId"), h.shareActor(c)); err != nil {
		writeShareErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func writeShareErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gateshare.ErrNoStandardAction):
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_standard_action"})
	case errors.Is(err, gateshare.ErrNotHumanGate):
		c.JSON(http.StatusBadRequest, gin.H{"error": "not_human_gate"})
	case errors.Is(err, gateshare.ErrUsedReadonly):
		c.JSON(http.StatusConflict, gin.H{"error": "used_readonly", "state": models.ShareLinkStateUsed})
	case errors.Is(err, gateshare.ErrRunEnded):
		c.JSON(http.StatusConflict, gin.H{"error": "run_ended"})
	case errors.Is(err, gateshare.ErrGateNotPending):
		c.JSON(http.StatusNotFound, gin.H{"error": "gate_not_pending"})
	case errors.Is(err, gateshare.ErrInvalidTTL):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_ttl"})
	case errors.Is(err, gateshare.ErrInvalidPermissionPreset):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_permission_preset"})
	case errors.Is(err, gateshare.ErrPermissionDenied):
		c.JSON(http.StatusForbidden, gin.H{"error": "permission_denied"})
	case errors.Is(err, gateshare.ErrNotActive):
		c.JSON(http.StatusConflict, gin.H{"error": "not_active"})
	case errors.Is(err, gateshare.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, gateshare.ErrNotReviewSession):
		c.JSON(http.StatusBadRequest, gin.H{"error": "not_review_session"})
	case errors.Is(err, gateshare.ErrReviewNotPending):
		c.JSON(http.StatusNotFound, gin.H{"error": "review_not_pending"})
	case errors.Is(err, gateshare.ErrReviewBusy):
		c.JSON(http.StatusConflict, gin.H{"error": "review_busy"})
	case errors.Is(err, gateshare.ErrReviewValidation):
		c.JSON(http.StatusConflict, gin.H{"error": "review_validation_failed"})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "share_failed"})
	}
}
