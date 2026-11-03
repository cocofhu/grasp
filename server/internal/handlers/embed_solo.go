package handlers

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const soloEmbedProtocol = "grasp-solo-embed"

// CreateSoloEmbedTicket binds the drawer to the owned session's current
// sandbox. The run ID is also the credential scope of the preview injector.
func (h *Handlers) CreateSoloEmbedTicket(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	if h.Embed == nil || h.Preview == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embed unavailable"})
		return
	}
	sb, err := h.Solo.ResourceSandbox(c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	if _, active := h.Preview.SoloPreviewSession(sb.RunID, user); !active || len(h.directPreviewOrigins(sb.RunID, "solo")) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "no active direct preview"})
		return
	}
	h.issueEmbedTicket(c, embed.Claims{
		Kind: models.EmbedKindSolo, RunID: sb.RunID, NodeID: "solo",
		Username: user, GraspOrigin: h.embedGraspOrigin(c),
	})
}

func (h *Handlers) authorizeSoloEmbed(c *gin.Context, token string) (*embed.Claims, models.Sandbox) {
	c.Header("Cache-Control", "no-store")
	runID := c.Param("runId")
	if h.Solo != nil && h.Embed != nil && h.Preview != nil {
		if claims, ok := h.Embed.LookupSession(token); ok && claims.Kind == models.EmbedKindSolo && claims.RunID == runID && claims.NodeID == "solo" {
			if sb, active := h.Preview.SoloPreviewSession(runID, claims.Username); active {
				c.Set(soloEmbedUserKey, claims.Username)
				c.Params = append(c.Params, gin.Param{Key: "id", Value: sb.ThreadID})
				return claims, sb
			}
		}
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_session"})
	return nil, models.Sandbox{}
}

// AuthorizeSoloEmbed is used only by the drawer's allowlisted Solo routes.
func (h *Handlers) AuthorizeSoloEmbed(c *gin.Context) {
	h.authorizeSoloEmbed(c, bearer(c.GetHeader("Authorization")))
}

func (h *Handlers) SoloEmbedChat(c *gin.Context) {
	// WebSocket in a browser cannot set Authorization. Pass the token in a
	// request subprotocol and only echo the fixed protocol name.
	protocols := websocket.Subprotocols(c.Request)
	if !sameOriginRequest(c) || len(protocols) != 2 || protocols[0] != soloEmbedProtocol {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_session"})
		return
	}
	claims, sb := h.authorizeSoloEmbed(c, protocols[1])
	if claims == nil {
		return
	}
	u := upgrader
	u.Subprotocols = []string{soloEmbedProtocol}
	h.serveSoloChat(c, claims.Username, u, claims.ExpiresAt, sb.ID)
}

func (h *Handlers) SoloEmbedArtifacts(c *gin.Context) {
	runID := c.Param("runId")
	c.JSON(http.StatusOK, gin.H{"artifacts": h.Arts.ByRunWithContent(runID)})
}
