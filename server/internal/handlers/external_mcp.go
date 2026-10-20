package handlers

import (
	"io"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type externalMcpUpdateBody struct {
	Enabled      bool     `json:"enabled"`
	EnabledPacks []string `json:"enabledPacks"`
}

type createMcpKeyBody struct {
	Name string `json:"name"`
}

func (h *Handlers) GetExternalMcp(c *gin.Context) {
	if h.ExternalMcp == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	view, err := h.ExternalMcp.Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, externalMcpSettingsDTO(view))
}

func (h *Handlers) UpdateExternalMcp(c *gin.Context) {
	if h.ExternalMcp == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	var body externalMcpUpdateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	view, err := h.ExternalMcp.Update(body.Enabled, body.EnabledPacks)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, externalMcpSettingsDTO(view))
}

func (h *Handlers) ListMcpKeys(c *gin.Context) {
	if h.McpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	keys := h.McpKeys.List()
	out := make([]gin.H, 0, len(keys))
	for _, k := range keys {
		out = append(out, mcpKeyDTO(k))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) CreateMcpKey(c *gin.Context) {
	if h.McpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	var body createMcpKeyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	res, err := h.McpKeys.Create(body.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":         res.Key.ID,
		"name":       res.Key.Name,
		"key":        res.Plaintext,
		"key_prefix": res.Key.KeyPrefix,
		"created_at": res.Key.CreatedAt,
	})
}

func (h *Handlers) RevokeMcpKey(c *gin.Context) {
	keyID := c.Param("keyId")
	if h.McpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	if !h.McpKeys.Revoke(keyID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// ExternalMCPRPC handles POST/GET/DELETE /mcp/workspace[/:mcpId].
func (h *Handlers) ExternalMCPRPC(c *gin.Context) {
	if h.Shutdown != nil && h.Shutdown.IsDraining() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workspace maintenance"})
		return
	}
	if h.PMMCP == nil || h.ExternalMcp == nil || h.McpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		c.Status(http.StatusOK)
		return
	}
	mcpID := strings.TrimSpace(c.Param("mcpId"))
	if !h.ExternalMcp.IsEnabled() {
		c.Data(http.StatusForbidden, "application/json", platformmcp.MustJSON(platformmcp.RPCResponse{
			JSONRPC: "2.0",
			Error:   &platformmcp.RPCError{Code: -32003, Message: "external mcp disabled"},
		}))
		return
	}
	plain := bearer(c.GetHeader("Authorization"))
	keyID, keyName, ok := h.McpKeys.ValidateBearer(plain)
	if !ok {
		status, resp := platformmcp.Unauthorized()
		c.Data(status, "application/json", resp)
		return
	}
	enabledPacks := h.ExternalMcp.EnabledPacks()
	token := h.PMMCP.RestoreExternal(keyID, keyName, enabledPacks)
	if token == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session init failed"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	status, resp := h.PMMCP.ServeRPC(mcpID, token, body)
	if resp == nil {
		c.Status(status)
		return
	}
	c.Data(status, "application/json", resp)
}

func externalMcpSettingsDTO(v services.ExternalMcpSettingsView) gin.H {
	packs := v.EnabledPacks
	if packs == nil {
		packs = []string{}
	}
	out := gin.H{
		"enabled":      v.Enabled,
		"enabledPacks": packs,
		"mcpBaseUrl":   v.McpBaseURL,
	}
	if !v.UpdatedAt.IsZero() {
		out["updatedAt"] = v.UpdatedAt
	}
	return out
}

func mcpKeyDTO(k models.WorkspaceMcpApiKey) gin.H {
	return gin.H{
		"id":         k.ID,
		"name":       k.Name,
		"key_prefix": k.KeyPrefix,
		"created_at": k.CreatedAt,
	}
}
