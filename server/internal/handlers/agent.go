package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func (h *Handlers) ListAgents(c *gin.Context) {
	c.JSON(http.StatusOK, h.Agents.List())
}

func (h *Handlers) GetAgent(c *gin.Context) {
	a, ok := h.Agents.Get(c.Param("name"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, a)
}

type agentBody struct {
	Name                 string                    `json:"name"`
	ProjectID            string                    `json:"projectId"`
	TemplateID           string                    `json:"templateId"`
	AcpBackend           string                    `json:"acpBackend"`
	GitCredentialType    string                    `json:"gitCredentialType"`
	OpenCodeCredentialID string                    `json:"openCodeCredentialId"`
	AiCredentialID       string                    `json:"aiCredentialId"`
	GitCredentialID      string                    `json:"gitCredentialId"`
	SshHostsCredentialID string                    `json:"sshHostsCredentialId"`
	Files                []services.AgentFile      `json:"files"`
	MCP                  []services.MCPServer      `json:"mcp"`
	Env                  map[string]string         `json:"env"`
	Layout               services.AgentLayout      `json:"layout"`
	Capabilities         *models.AgentCapabilities `json:"capabilities"`
	Reason               string                    `json:"reason,omitempty"`
}

func (b agentBody) toAgent(name string) services.Agent {
	return services.Agent{
		Name: name, ProjectID: strings.TrimSpace(b.ProjectID), AcpBackend: b.AcpBackend,
		GitCredentialType: b.GitCredentialType, OpenCodeCredentialID: strings.TrimSpace(b.OpenCodeCredentialID),
		AiCredentialID: strings.TrimSpace(b.AiCredentialID), GitCredentialID: strings.TrimSpace(b.GitCredentialID),
		SshHostsCredentialID: strings.TrimSpace(b.SshHostsCredentialID),
		Files:                b.Files, MCP: b.MCP, Env: b.Env, Layout: b.Layout, Capabilities: b.Capabilities,
	}
}

// validateAgentProjectBinding requires a home project that exists.
func (h *Handlers) validateAgentProjectBinding(agent services.Agent) error {
	projectID := strings.TrimSpace(agent.ProjectID)
	if projectID == "" {
		return services.ErrAgentProjectRequired
	}
	if h.Projects == nil {
		return errors.New("项目管理服务不可用，无法校验所属项目")
	}
	if _, ok := h.Projects.Get(projectID); !ok {
		return errors.New("所属项目不存在")
	}
	return nil
}

// agentSaveStatus maps Agent save errors to HTTP status.
func agentSaveStatus(err error) int {
	if errors.Is(err, services.ErrSecretEnvKey) || errors.Is(err, services.ErrInvalidAcpBackend) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// CreateAgent registers a new user-defined Agent.
// Optional templateId copies an embedded role pack workspace (name stays client-supplied).
func (h *Handlers) CreateAgent(c *gin.Context) {
	var b agentBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := services.NormalizeAndValidateAgentName(b.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.Agents.Exists(name) {
		c.JSON(http.StatusConflict, gin.H{"error": "agent already exists"})
		return
	}
	agent := b.toAgent(name)

	templateID := strings.TrimSpace(b.TemplateID)
	if templateID != "" {
		if err := services.ApplyCreateTemplate(templateID, &agent); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	if len(agent.MCP) == 0 {
		agent.MCP = services.DefaultPlatformMCP()
	}
	if err := h.validateAgentProjectBinding(agent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Agents.Save(agent); err != nil {
		_ = c.Error(err)
		c.JSON(agentSaveStatus(err), gin.H{"error": err.Error()})
		return
	}
	a, _ := h.Agents.Get(name)
	c.JSON(http.StatusCreated, a)
}

func (h *Handlers) SaveAgent(c *gin.Context) {
	var b agentBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := c.Param("name")
	oldProjectID := ""
	if prev, ok := h.Agents.Get(name); ok {
		oldProjectID = strings.TrimSpace(prev.ProjectID)
	}
	agent := b.toAgent(name)
	if err := h.validateAgentProjectBinding(agent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := services.RejectSecretEnvKeys(agent.Env); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := services.ParseAcpBackend(agent.AcpBackend); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	newProjectID := strings.TrimSpace(agent.ProjectID)
	if oldProjectID != "" && oldProjectID != newProjectID && h.Pm != nil {
		if err := h.Pm.PurgeAgentProjectData(oldProjectID, name); err != nil {
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "清除旧项目数据失败：" + err.Error()})
			return
		}
	}
	reason := strings.TrimSpace(b.Reason)
	if reason == "" {
		reason = "Studio 保存"
	}
	if _, err := h.Agents.SaveAgentWithVcs(agent, sessionUsername(c), reason, true); err != nil {
		_ = c.Error(err)
		c.JSON(agentSaveStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "saved"})
}

func (h *Handlers) DeleteAgent(c *gin.Context) {
	name := c.Param("name")
	if h.Pm != nil {
		if err := h.Pm.PurgeAgentEverywhere(name); err != nil {
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "清除 Agent 项目数据失败：" + err.Error()})
			return
		}
	}
	if err := h.Agents.Delete(name); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// renameAgentResp is the RenameAgent success payload: agent fields plus the
// count of WorkflowDef rows whose Def and/or Version graphs were rewritten.
type renameAgentResp struct {
	services.Agent
	UpdatedWorkflowCount int `json:"updatedWorkflowCount"`
}

// RenameAgent atomically renames an existing Agent to the name in the body.
func (h *Handlers) RenameAgent(c *gin.Context) {
	old := c.Param("name")
	var b struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := services.NormalizeAndValidateAgentName(b.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.Agents.Exists(old) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if name != old && h.Agents.Exists(name) {
		c.JSON(http.StatusConflict, gin.H{"error": "agent already exists"})
		return
	}
	if err := h.Agents.Rename(old, name); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.Pm != nil && name != old {
		if err := h.Pm.RenameAgentScopedData(old, name); err != nil {
			if rbErr := h.Agents.Rename(name, old); rbErr != nil {
				_ = c.Error(err)
				_ = c.Error(rbErr)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": err.Error() + "; rename rollback failed: " + rbErr.Error(),
				})
				return
			}
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重命名 Agent 数据失败：" + err.Error()})
			return
		}
	}
	updatedWorkflowCount := 0
	if h.WF != nil && name != old {
		n, err := h.WF.RenameAgentProfileRefs(old, name)
		if err != nil {

			if rbErr := h.Agents.Rename(name, old); rbErr != nil {
				_ = c.Error(err)
				_ = c.Error(rbErr)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": err.Error() + "; rename rollback failed: " + rbErr.Error(),
				})
				return
			}
			if h.Pm != nil {
				if rbData := h.Pm.RenameAgentScopedData(name, old); rbData != nil {
					_ = c.Error(rbData)
				}
			}
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重命名工作流引用失败：" + err.Error()})
			return
		}
		updatedWorkflowCount = n
	}
	a, _ := h.Agents.Get(name)
	c.JSON(http.StatusOK, renameAgentResp{Agent: a, UpdatedWorkflowCount: updatedWorkflowCount})
}

// ExportAgent streams a ZIP export of one agent (on-disk state only).
func (h *Handlers) ExportAgent(c *gin.Context) {
	name := c.Param("name")
	raw, err := h.Agents.ExportZIP(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename="+name+".zip")
	c.Data(http.StatusOK, "application/zip", raw)
}

// ImportAgent accepts a multipart ZIP and creates or overwrites an agent.
func (h *Handlers) ImportAgent(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	targetName := strings.TrimSpace(c.PostForm("targetName"))
	if targetName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "targetName is required"})
		return
	}
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if err := h.validateAgentProjectBinding(services.Agent{ProjectID: projectID}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mode := services.ImportZIPMode(strings.TrimSpace(c.PostForm("mode")))
	if mode == "" {
		mode = services.ImportZIPCreate
	}

	if mode == services.ImportZIPCreate {
		normalized, err := services.NormalizeAndValidateAgentName(targetName)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		targetName = normalized
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	raw, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if mode == services.ImportZIPOverwrite && h.Pm != nil {
		if prev, ok := h.Agents.Get(targetName); ok {
			if old := strings.TrimSpace(prev.ProjectID); old != "" && old != projectID {
				if err := h.Pm.PurgeAgentProjectData(old, targetName); err != nil {
					_ = c.Error(err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "清除旧项目数据失败：" + err.Error()})
					return
				}
			}
		}
	}
	agent, err := h.Agents.ImportZIP(raw, targetName, projectID, mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, agent)
}
