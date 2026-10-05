package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type sharedAgentBody struct {
	AcpBackend        string               `json:"acpBackend"`
	DefaultProjectID  string               `json:"defaultProjectId"`
	GitCredentialType string               `json:"gitCredentialType"`
	GitSshKnownHosts  string               `json:"gitSshKnownHosts"`
	GitSshPrivateKey  string               `json:"gitSshPrivateKey"`
	Files             []services.AgentFile `json:"files"`
	MCP               []services.MCPServer `json:"mcp"`
	Env               map[string]string    `json:"env"`
	Layout            services.AgentLayout `json:"layout"`
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
	return gin.H{
		"projectId":         cfg.ProjectID,
		"acpBackend":        cfg.AcpBackend,
		"defaultProjectId":  cfg.DefaultProjectID,
		"gitCredentialType": cfg.GitCredentialType,
		"gitSshKnownHosts":  cfg.GitSshKnownHosts,
		"gitSshPrivateKey":  cfg.GitSshPrivateKey,
		"files":             files,
		"mcp":               mcp,
		"env":               env,
		"layout":            cfg.Layout,
	}
}

// GetProjectSharedAgent returns the project's shared Agent baseline (empty OK).
func (h *Handlers) GetProjectSharedAgent(c *gin.Context) {
	if h.Projects == nil || h.SharedAgent == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	pid := c.Param("id")
	if _, ok := h.Projects.Get(pid); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, sharedAgentDTO(h.SharedAgent.Get(pid)))
}

// PutProjectSharedAgent replaces the project's shared Agent baseline.
func (h *Handlers) PutProjectSharedAgent(c *gin.Context) {
	if h.Projects == nil || h.SharedAgent == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "shared agent unavailable"})
		return
	}
	pid := c.Param("id")
	if _, ok := h.Projects.Get(pid); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var b sharedAgentBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg := services.SharedAgentConfig{
		ProjectID:         pid,
		AcpBackend:        b.AcpBackend,
		DefaultProjectID:  strings.TrimSpace(b.DefaultProjectID),
		GitCredentialType: b.GitCredentialType,
		GitSshKnownHosts:  b.GitSshKnownHosts,
		GitSshPrivateKey:  b.GitSshPrivateKey,
		Files:             b.Files,
		MCP:               b.MCP,
		Env:               b.Env,
		Layout:            b.Layout,
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if err := h.SharedAgent.Save(cfg); err != nil {
		_ = c.Error(err)
		status := http.StatusInternalServerError
		if errors.Is(err, services.ErrSSHMetaVarsRef) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sharedAgentDTO(h.SharedAgent.Get(pid)))
}

// CreateProjectSharedAgentTest starts a chat-test sandbox with extend→overlay
// against the current project shared config + a chosen Agent.
func (h *Handlers) CreateProjectSharedAgentTest(c *gin.Context) {
	if h.Projects == nil || h.SharedAgent == nil || h.Sbx == nil || h.Agents == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "shared agent test unavailable"})
		return
	}
	pid := c.Param("id")
	if _, ok := h.Projects.Get(pid); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
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
	shared := h.SharedAgent.Get(pid)
	effective := services.ExtendOverlay(shared, agent)
	repos := resolveTestRepos(b.Repos, b.RepoURL)
	row, err := h.Sbx.OpenWithEffective(c.Request.Context(), agentName, pid, repos, effective, h.SharedAgent.WorkDir(pid))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, row)
}
