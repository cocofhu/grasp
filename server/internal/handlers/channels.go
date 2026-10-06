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
	// SyncPmLeader confirms updating Project.PmLeaderAgent on primary rebind.
	SyncPmLeader bool `json:"syncPmLeader"`
}

type channelDeleteBody struct {
	NewPrimaryID     string `json:"newPrimaryId"`
	ConfirmNoPrimary bool   `json:"confirmNoPrimary"`
	SyncPmLeader     bool   `json:"syncPmLeader"`
}

func (b channelBody) toInput(projectID string) services.ChannelConfigInput {
	in := services.ChannelConfigInput{
		Type: strings.TrimSpace(strings.ToLower(b.Type)), Name: b.Name, Enabled: b.Enabled, ProjectID: projectID,
		IsPrimary: b.IsPrimary,
		AgentName: b.AgentName, EnabledMcps: b.EnabledMcps,
		AppID: b.AppID, AppSecret: b.AppSecret, TurnTimeoutSeconds: b.TurnTimeoutSeconds,
		CronDeliver: b.CronDeliver, CronDeliverTarget: b.CronDeliverTarget, Config: b.Config,
		SyncPmLeader: b.SyncPmLeader,
	}
	return in
}

// ListProjectChannels handles GET /api/projects/:id/channels.
func (h *Handlers) ListProjectChannels(c *gin.Context) {
	if h.Channels == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channels unavailable"})
		return
	}
	items, err := h.Channels.ListByProject(c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":                items,
		"secretsKeyConfigured": crypto.Available(),
		"freeAgents":           h.Channels.ListFreeAgents(c.Param("id"), ""),
	})
}

// CreateProjectChannel handles POST /api/projects/:id/channels.
func (h *Handlers) CreateProjectChannel(c *gin.Context) {
	if h.Channels == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channels unavailable"})
		return
	}
	var b channelBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dto, err := h.Channels.Create(b.toInput(c.Param("id")))
	if err != nil {
		writeChannelErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    c.Param("id"),
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionChannel,
		ResourceType: "channel",
		ResourceID:   dto.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "create project channel",
		Payload: map[string]any{
			"type": dto.Type, "enabled": dto.Enabled, "appId": dto.AppID,
			"agentName": dto.AgentName, "isPrimary": dto.IsPrimary,
		},
	})
	c.JSON(http.StatusOK, dto)
}

// UpdateProjectChannel handles PUT /api/projects/:id/channels/:channelId.
func (h *Handlers) UpdateProjectChannel(c *gin.Context) {
	if h.Channels == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channels unavailable"})
		return
	}
	var b channelBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in := b.toInput(c.Param("id"))
	dto, err := h.Channels.Update(c.Param("channelId"), in)
	if err != nil {
		writeChannelErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{
		ProjectID:    c.Param("id"),
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionChannel,
		ResourceType: "channel",
		ResourceID:   dto.ID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "update project channel",
		Payload: map[string]any{
			"type": dto.Type, "enabled": dto.Enabled, "appId": dto.AppID,
			"agentName": dto.AgentName, "isPrimary": dto.IsPrimary,
		},
	})
	c.JSON(http.StatusOK, dto)
}

// DeleteProjectChannelByID handles DELETE /api/projects/:id/channels/:channelId.
func (h *Handlers) DeleteProjectChannelByID(c *gin.Context) {
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
		ProjectID:    c.Param("id"),
		Actor:        h.auditActorFromContext(c),
		Action:       models.AuditActionChannel,
		ResourceType: "channel",
		ResourceID:   c.Param("channelId"),
		Outcome:      models.AuditOutcomeOK,
		Summary:      "delete project channel",
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
	case errors.Is(err, services.ErrProjectNotFound),
		errors.Is(err, services.ErrChannelProjectRequired),
		errors.Is(err, services.ErrChannelAppIDRequired),
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
		errors.Is(err, services.ErrChannelAgentNotInProject),
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
