package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type channelBody struct {
	Type               string         `json:"type"`
	Name               string         `json:"name"`
	Enabled            bool           `json:"enabled"`
	AgentName          string         `json:"agentName"`
	IsPrimary          bool           `json:"isPrimary"`
	EnabledMcps        []string       `json:"enabledMcps"`
	AppID              string         `json:"appId"`
	AppSecret          string         `json:"appSecret"`
	TurnTimeoutSeconds int            `json:"turnTimeoutSeconds"`
	CronDeliver        bool           `json:"cronDeliver"`
	CronDeliverTarget  string         `json:"cronDeliverTarget"`
	Config             map[string]any `json:"config"`
	// SyncPmLeader confirms updating Workspace.PmLeaderAgent on primary rebind.
	SyncPmLeader bool `json:"syncPmLeader"`
}

type channelDeleteBody struct {
	NewPrimaryID     string `json:"newPrimaryId"`
	ConfirmNoPrimary bool   `json:"confirmNoPrimary"`
	SyncPmLeader     bool   `json:"syncPmLeader"`
}

func (b channelBody) toInput() services.ChannelConfigInput {
	in := services.ChannelConfigInput{
		Type: strings.TrimSpace(strings.ToLower(b.Type)), Name: b.Name, Enabled: b.Enabled, IsPrimary: b.IsPrimary,
		AgentName: b.AgentName, EnabledMcps: b.EnabledMcps,
		AppID: b.AppID, AppSecret: b.AppSecret, TurnTimeoutSeconds: b.TurnTimeoutSeconds,
		CronDeliver: b.CronDeliver, CronDeliverTarget: b.CronDeliverTarget, Config: b.Config,
		SyncPmLeader: b.SyncPmLeader,
	}
	return in
}

// ListChannels handles GET /api/workspace/channels.
func (h *Handlers) ListChannels(c *gin.Context) {
	if h.Channels == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channels unavailable"})
		return
	}
	items, err := h.Channels.List()
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":                items,
		"secretsKeyConfigured": crypto.Available(),
		"freeAgents":           h.Channels.ListFreeAgents(""),
	})
}

// CreateChannel handles POST /api/workspace/channels.
func (h *Handlers) CreateChannel(c *gin.Context) {
	if h.Channels == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channels unavailable"})
		return
	}
	var b channelBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dto, err := h.Channels.Create(b.toInput())
	if err != nil {
		writeChannelErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionChannel,
		ResourceType: "channel",
		ResourceID:   dto.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "create channel",
		Payload: map[string]any{
			"type": dto.Type, "enabled": dto.Enabled, "appId": dto.AppID,
			"agentName": dto.AgentName, "isPrimary": dto.IsPrimary,
		},
	})
	c.JSON(http.StatusOK, dto)
}

// UpdateChannel handles PUT /api/workspace/channels/:channelId.
func (h *Handlers) UpdateChannel(c *gin.Context) {
	if h.Channels == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channels unavailable"})
		return
	}
	var b channelBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in := b.toInput()
	dto, err := h.Channels.Update(c.Param("channelId"), in)
	if err != nil {
		writeChannelErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionChannel,
		ResourceType: "channel",
		ResourceID:   dto.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update channel",
		Payload: map[string]any{
			"type": dto.Type, "enabled": dto.Enabled, "appId": dto.AppID,
			"agentName": dto.AgentName, "isPrimary": dto.IsPrimary,
		},
	})
	c.JSON(http.StatusOK, dto)
}

// DeleteChannel handles DELETE /api/workspace/channels/:channelId.
func (h *Handlers) DeleteChannel(c *gin.Context) {
	if h.Channels == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channels unavailable"})
		return
	}
	var body channelDeleteBody
	// Body is optional (secondary delete needs none); ignore bind errors for empty body.
	_ = c.ShouldBindJSON(&body)
	opts := services.ChannelDeleteOpts{
		NewPrimaryID:     strings.TrimSpace(body.NewPrimaryID),
		ConfirmNoPrimary: body.ConfirmNoPrimary,
		SyncPmLeader:     body.SyncPmLeader,
	}
	if err := h.Channels.Delete(c.Param("channelId"), opts); err != nil {
		writeChannelErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionChannel,
		ResourceType: "channel",
		ResourceID:   c.Param("channelId"),
		Outcome:      models.AuditOutcomeOK,
		Summary:      "delete channel",
		Payload: map[string]any{
			"deleted": true, "newPrimaryId": opts.NewPrimaryID,
			"confirmNoPrimary": opts.ConfirmNoPrimary,
		},
	})
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func writeChannelErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrChannelNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrChannelAppIDExists),
		errors.Is(err, services.ErrChannelBotIDExists),
		errors.Is(err, services.ErrChannelAgentTaken),
		errors.Is(err, services.ErrChannelDualPrimary):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrChannelAppIDRequired),
		errors.Is(err, services.ErrChannelSecretRequired),
		errors.Is(err, services.ErrChannelSecretKeyMissing),
		errors.Is(err, services.ErrChannelSecretKeyInvalid),
		errors.Is(err, services.ErrChannelTypeUnsupported),
		errors.Is(err, services.ErrChannelTypeRequired),
		errors.Is(err, services.ErrChannelTypeFrozen),
		errors.Is(err, services.ErrChannelBotIDFrozen),
		errors.Is(err, services.ErrChannelNameRequired),
		errors.Is(err, services.ErrChannelCronTargetRequired),
		errors.Is(err, services.ErrChannelCronTargetInvalid),
		errors.Is(err, services.ErrChannelAgentRequired),
		errors.Is(err, services.ErrChannelAgentNotFound),
		errors.Is(err, services.ErrChannelAgentUnavailable),
		errors.Is(err, services.ErrChannelPromoteForbidden),
		errors.Is(err, services.ErrChannelDeletePrimaryNeedsAck),
		errors.Is(err, services.ErrChannelNewPrimaryNotFound):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
