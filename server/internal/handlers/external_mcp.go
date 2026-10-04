package handlers

import (
	"errors"
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

type createProjectMcpKeyBody struct {
	Name string `json:"name"`
}

func (h *Handlers) GetProjectExternalMcp(c *gin.Context) {
	projectID := c.Param("id")
	if h.ExternalMcp == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	if _, ok := h.Projects.Get(projectID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	view, err := h.ExternalMcp.Get(projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, externalMcpSettingsDTO(view))
}

func (h *Handlers) UpdateProjectExternalMcp(c *gin.Context) {
	projectID := c.Param("id")
	if h.ExternalMcp == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	var body externalMcpUpdateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	view, err := h.ExternalMcp.Update(projectID, body.Enabled, body.EnabledPacks)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, externalMcpSettingsDTO(view))
}

func (h *Handlers) ListProjectMcpKeys(c *gin.Context) {
	projectID := c.Param("id")
	if h.ProjectMcpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	if _, ok := h.Projects.Get(projectID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	keys := h.ProjectMcpKeys.List(projectID)
	out := make([]gin.H, 0, len(keys))
	for _, k := range keys {
		out = append(out, projectMcpKeyDTO(k))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) CreateProjectMcpKey(c *gin.Context) {
	projectID := c.Param("id")
	if h.ProjectMcpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	var body createProjectMcpKeyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	res, err := h.ProjectMcpKeys.Create(projectID, body.Name)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
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

func (h *Handlers) RevokeProjectMcpKey(c *gin.Context) {
	projectID := c.Param("id")
	keyID := c.Param("keyId")
	if h.ProjectMcpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	if !h.ProjectMcpKeys.Revoke(projectID, keyID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// ExternalMCPRPC handles POST/GET/DELETE /mcp/external/:projectId[/:mcpId].
func (h *Handlers) ExternalMCPRPC(c *gin.Context) {
	if h.PMMCP == nil || h.ExternalMcp == nil || h.ProjectMcpKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "external mcp unavailable"})
		return
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		c.Status(http.StatusOK)
		return
	}
	projectID := strings.TrimSpace(c.Param("projectId"))
	mcpID := strings.TrimSpace(c.Param("mcpId"))
	if !h.ExternalMcp.IsEnabled(projectID) {
		c.Data(http.StatusForbidden, "application/json", platformmcp.MustJSON(platformmcp.RPCResponse{
			JSONRPC: "2.0",
			Error:   &platformmcp.RPCError{Code: -32003, Message: "external mcp disabled for project"},
		}))
		return
	}
	plain := bearer(c.GetHeader("Authorization"))
	keyProjectID, keyID, keyName, ok := h.ProjectMcpKeys.ValidateBearer(plain)
	if !ok || keyProjectID != projectID {
		status, resp := platformmcp.Unauthorized()
		c.Data(status, "application/json", resp)
		return
	}
	enabledPacks := h.ExternalMcp.EnabledPacks(projectID)
	token := h.PMMCP.RestoreExternal(projectID, keyID, keyName, enabledPacks)
	if token == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session init failed"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	status, resp := h.PMMCP.ServeRPC(projectID, mcpID, token, body)
	if resp == nil {
		c.Status(status)
		return
	}
	c.Data(status, "application/json", resp)
}

func externalMcpSettingsDTO(v services.ProjectExternalMcpSettingsView) gin.H {
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

func projectMcpKeyDTO(k models.ProjectMcpApiKey) gin.H {
	return gin.H{
		"id":         k.ID,
		"name":       k.Name,
		"key_prefix": k.KeyPrefix,
		"created_at": k.CreatedAt,
	}
}
