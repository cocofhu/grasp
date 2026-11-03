package handlers

import (
	"errors"
	"net/http"
	"slices"

	"github.com/cocofhu/grasp/internal/clicatalog"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func (h *Handlers) previewModelAgent(name string) (services.Agent, bool) {
	if name == "" || h.Agents == nil {
		return services.Agent{}, false
	}
	agent, found := h.Agents.Get(name)
	if !found {
		return services.Agent{}, false
	}
	return h.Agents.Effective(agent), true
}

func (h *Handlers) writePreviewModelCatalog(c *gin.Context, agent services.Agent, found bool) {
	if !found || h.ModelConfigurations == nil || h.ModelConnections == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model catalog unavailable"})
		return
	}
	configs, err := h.ModelConfigurations.List()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model catalog unavailable"})
		return
	}
	allowed := make([]modelrouting.Configuration, 0)
	connections := make(map[string]modelrouting.Connection)
	connectionModels := make(map[string]map[string]bool)
	cli := make(map[string][]clicatalog.Model)
	efforts := make(map[string]map[string][]string)
	unavailable := make([]string, 0)
	cliFailed := false
	for _, config := range configs {
		if config.State != "active" || !modelrouting.HasConfiguration(agent.ModelBinding, config.ID) {
			continue
		}
		binding := &config.Binding
		credentialID := binding.CredentialID
		var original *modelrouting.Connection
		if binding.Kind == "connection" {
			conn, err := h.ModelConnections.Get(binding.ConnectionID)
			if err == nil && conn.State == "active" {
				credentialID = conn.CredentialID
				original = &conn
				if connectionModels[conn.ID] == nil {
					connectionModels[conn.ID] = make(map[string]bool)
					connections[conn.ID] = modelrouting.Connection{
						ID: conn.ID, Name: conn.Name, ProviderID: conn.ProviderID, Models: conn.Models, State: conn.State,
					}
				}
				for _, id := range append([]string{binding.ModelID}, binding.AuxiliaryModelIDs...) {
					connectionModels[conn.ID][id] = true
				}
			}
		}
		if h.Credentials != nil {
			key, err := h.Credentials.Get(credentialID)
			if err != nil || !key.Configured || !key.Enabled || key.RevokedAt != nil {
				unavailable = append(unavailable, config.ID)
			}
		}
		if _, loaded := cli[config.Backend]; !loaded {
			models, err := h.cliBackendModels(c.Request.Context(), config.Backend)
			cli[config.Backend] = models
			cliFailed = cliFailed || err != nil
		}
		efforts[config.ID] = make(map[string][]string)
		for _, id := range append([]string{binding.ModelID}, binding.AuxiliaryModelIDs...) {
			options := modelrouting.ReasoningEfforts(config.Backend, id, original)
			if config.Backend == "opencode" {
				options = modelrouting.SessionEffortChoices(options, binding.ToolSettings.ModelEfforts[id])
			}
			efforts[config.ID][id] = options
		}
		config.Binding.CredentialID = ""
		config.Binding.ToolSettings = modelrouting.ToolSettings{}
		allowed = append(allowed, config)
	}
	pickerConnections := make([]modelrouting.Connection, 0, len(connections))
	for id, conn := range connections {
		conn.Models = slices.DeleteFunc(slices.Clone(conn.Models), func(model modelrouting.Model) bool {
			return !connectionModels[id][model.ID]
		})
		pickerConnections = append(pickerConnections, conn)
	}
	var agentBinding *modelrouting.Binding
	if binding := agent.ModelBinding; binding != nil && binding.Kind == "configuration" {
		agentBinding = &modelrouting.Binding{
			Kind: binding.Kind, ConfigurationID: binding.ConfigurationID,
			ConfigurationIDs: binding.ConfigurationIDs, AllConfigurations: binding.AllConfigurations,
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"agent":          gin.H{"name": agent.Name, "modelBinding": agentBinding},
		"configurations": allowed, "connections": pickerConnections, "cliModels": cli,
		"cliFailed": cliFailed, "efforts": efforts, "unavailableConfigurationIds": unavailable,
	})
}

func (h *Handlers) SoloEmbedModelCatalog(c *gin.Context) {
	row, err := h.Solo.Require(c.Param("id"), c.GetString(soloEmbedUserKey))
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	agent, found := h.previewModelAgent(row.AgentName)
	h.writePreviewModelCatalog(c, agent, found)
}

func (h *Handlers) ownerEmbedUser(token, runID, nodeID string) (string, bool) {
	if h.Embed != nil {
		claims, ok := h.Embed.LookupSession(token)
		if ok && claims.Kind == models.EmbedKindSession && claims.RunID == runID && claims.NodeID == nodeID && claims.Username != "" {
			return claims.Username, true
		}
	}
	return "", false
}

// AuthorizeEmbedNode restricts drawer operations to the owner's exact node.
func (h *Handlers) AuthorizeEmbedNode(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	user, ok := h.ownerEmbedUser(bearer(c.GetHeader("Authorization")), c.Param("id"), c.Param("nodeId"))
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_session"})
		return
	}
	c.Set(soloEmbedUserKey, user)
}

func (h *Handlers) EmbedVisitModelCatalog(c *gin.Context) {
	agent, found := h.visitModelAgent(c.Param("id"), c.Param("nodeId"))
	h.writePreviewModelCatalog(c, agent, found)
}

func (h *Handlers) PublicAppPreviewModelCatalog(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	link, sb := h.publicAppShare(c)
	if link == nil {
		return
	}
	if link.PreviewChatMode == models.PreviewChatIsolated {
		h.writePreviewGuestModelCatalog(c, sb)
		return
	}
	var agent services.Agent
	var found bool
	if sb.Purpose == services.SandboxPurposeSolo {
		agent, found = h.previewModelAgent(sb.Profile)
	} else {
		agent, found = h.visitModelAgent(link.RunID, link.NodeID)
	}
	h.writePreviewModelCatalog(c, agent, found)
}

// PublicAppPreviewModel only acts on the original conversation of a main link.
// Independent visitor selections travel on the isolated chat connection.
func (h *Handlers) PublicAppPreviewModel(c *gin.Context) {
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
	c.Header("Cache-Control", "no-store")
	var body visitModelBody
	if c.Request.Method == http.MethodPost && c.ShouldBindJSON(&body) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	solo := sb.Purpose == services.SandboxPurposeSolo
	if !solo {
		state, active := h.workflowPreviewDialogue(link.RunID, link.NodeID)
		if !active || state.Iteration != link.Iteration {
			c.JSON(http.StatusConflict, gin.H{"error": "main conversation unavailable"})
			return
		}
	}
	if c.Request.Method == http.MethodGet {
		choice, err := h.mainPreviewModel(link, sb)
		if err != nil {
			writeSoloErr(c, err)
			return
		}
		c.JSON(http.StatusOK, choice)
		return
	}
	if solo {
		if h.Solo == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
			return
		}
		row, err := h.Solo.SetCurrentModelRouteSettings(c.Request.Context(), sb.ThreadID, link.CreatedBy, body.ConfigurationID, body.ModelID, body.ReasoningEffort)
		if err != nil {
			writeSoloErr(c, err)
			return
		}
		c.JSON(http.StatusOK, soloPreviewModel(&row.SoloSession))
		return
	}
	agent, found := h.visitModelAgent(link.RunID, link.NodeID)
	if !found {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model choice unavailable"})
		return
	}
	choice, err := services.SetVisitModel(h.Runs.DB(), agent, link.RunID, link.NodeID, link.Iteration, body.ConfigurationID, body.ModelID, body.ReasoningEffort)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, choice)
}

func (h *Handlers) mainPreviewModel(link *models.GateShareLink, sb models.Sandbox) (services.VisitModelChoice, error) {
	if sb.Purpose == services.SandboxPurposeSolo {
		if h.Solo == nil {
			return services.VisitModelChoice{}, errors.New("solo unavailable")
		}
		row, err := h.Solo.Require(sb.ThreadID, link.CreatedBy)
		if err != nil {
			return services.VisitModelChoice{}, err
		}
		return soloPreviewModel(row), nil
	}
	return services.GetVisitModel(h.Runs.DB(), link.RunID, link.NodeID, link.Iteration)
}

func soloPreviewModel(row *models.SoloSession) services.VisitModelChoice {
	choice := services.VisitModelChoice{
		CurrentConfigurationID: row.CurrentConfigurationID, CurrentModelID: row.CurrentModelID,
		CurrentModelSet: row.CurrentModelSet, ReasoningEffort: row.CurrentReasoningEffort, ModelNotice: row.ModelNotice,
	}
	if !row.CurrentModelSet {
		choice.CurrentConfigurationID = row.InitialConfigurationID
		choice.CurrentModelID = row.InitialModelID
		choice.CurrentModelSet = row.InitialModelID != ""
	}
	return choice
}
