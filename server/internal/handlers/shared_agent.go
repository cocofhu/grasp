package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type sharedAgentBody struct {
	Files  []services.AgentFile `json:"files"`
	MCP    []services.MCPServer `json:"mcp"`
	Env    map[string]string    `json:"env"`
	Layout services.AgentLayout `json:"layout"`
}

func sharedAgentDTO(cfg services.SharedAgentConfig) gin.H {
	env := cfg.Env
	if env == nil {
		env = map[string]string{}
	}
	files := cfg.Files
	if files == nil {
		files = []services.AgentFile{}
	}
	mcp := cfg.MCP
	if mcp == nil {
		mcp = []services.MCPServer{}
	}
	dto := gin.H{
		"files":  files,
		"mcp":    mcp,
		"env":    env,
		"layout": cfg.Layout,
	}
	return dto
}

// GetSharedAgent returns the shared Agent baseline (empty OK).
func (h *Handlers) GetSharedAgent(c *gin.Context) {
	if h.SharedAgent == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, sharedAgentDTO(h.SharedAgent.Get()))
}

// PutSharedAgent replaces the shared Agent baseline.
func (h *Handlers) PutSharedAgent(c *gin.Context) {
	if h.SharedAgent == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "shared agent unavailable"})
		return
	}
	var b sharedAgentBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg := services.SharedAgentConfig{
		Files:  b.Files,
		MCP:    b.MCP,
		Env:    b.Env,
		Layout: b.Layout,
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if err := h.SharedAgent.Save(cfg); err != nil {
		_ = c.Error(err)
		status := http.StatusInternalServerError
		if errors.Is(err, services.ErrSecretEnvKey) || errors.Is(err, services.ErrInvalidAcpBackend) || errors.Is(err, services.ErrInvalidAgent) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sharedAgentDTO(h.SharedAgent.Get()))
}

// CreateSharedAgentTest starts a chat-test sandbox with extend→overlay
// against the shared config + a chosen Agent.
func (h *Handlers) CreateSharedAgentTest(c *gin.Context) {
	if h.SharedAgent == nil || h.Sbx == nil || h.Agents == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "shared agent test unavailable"})
		return
	}
	var b struct {
		AgentName string          `json:"agentName"`
		Repos     []testRepoInput `json:"repos"`
		RepoURL   string          `json:"repoUrl"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agentName := strings.TrimSpace(b.AgentName)
	if agentName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agentName required"})
		return
	}
	agent, ok := h.Agents.Get(agentName)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent not found"})
		return
	}
	shared := h.SharedAgent.Get()
	effective := services.ExtendOverlay(shared, agent)
	repos := resolveTestRepos(b.Repos, b.RepoURL)
	row, err := h.Sbx.OpenWithEffective(c.Request.Context(), agentName, repos, effective, h.SharedAgent.WorkDir())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}
