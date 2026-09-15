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
	Name              string               `json:"name"`
	ProjectID         *string              `json:"projectId"`
	TemplateID        string               `json:"templateId"`
	AcpBackend        string               `json:"acpBackend"`
	GitCredentialType string               `json:"gitCredentialType"`
	GitSshKnownHosts  string               `json:"gitSshKnownHosts"`
	GitSshPrivateKey  string               `json:"gitSshPrivateKey"`
	Files             []services.AgentFile `json:"files"`
	MCP               []services.MCPServer `json:"mcp"`
	Env               map[string]string    `json:"env"`
	Layout            services.AgentLayout `json:"layout"`
	Prompts           *models.AgentPrompts `json:"prompts"`
	Reason            string               `json:"reason,omitempty"`
}

// toAgent builds an Agent. When projectId is omitted (nil), prevProjectID is kept
// so older clients cannot accidentally unbind+purge by leaving the field out.
// Explicit "" unbinds.
func (b agentBody) toAgent(name, prevProjectID string) services.Agent {
	projectID := strings.TrimSpace(prevProjectID)
	if b.ProjectID != nil {
		projectID = strings.TrimSpace(*b.ProjectID)
	}
	return services.Agent{
		Name: name, ProjectID: projectID, AcpBackend: b.AcpBackend,
		GitCredentialType: b.GitCredentialType,
		GitSshKnownHosts:  b.GitSshKnownHosts,
		GitSshPrivateKey:  b.GitSshPrivateKey,
		Files:             b.Files, MCP: b.MCP, Env: b.Env, Layout: b.Layout, Prompts: b.Prompts,
	}
}

// validateAgentProjectBinding enforces Agent↔project rules: unbound Agents may
// not declare project-scoped platform MCPs; a bound Agent must point at an
// existing project. Pure validation — no destructive side effects.
func (h *Handlers) validateAgentProjectBinding(agent services.Agent) error {
	if !services.AgentMayUseProjectPlatformMCP(agent) && services.AgentDeclaresProjectPlatformMCP(agent.MCP) {
		return errors.New("未绑定主项目的 Agent 只能使用 artifact-store；请先绑定主项目再添加 memory-store / context-store / task-scheduler")
	}
	projectID := strings.TrimSpace(agent.ProjectID)
	if projectID == "" {
		return nil
	}
	if h.Projects == nil {
		return errors.New("项目管理服务不可用，无法校验主项目绑定")
	}
	if _, ok := h.Projects.Get(projectID); !ok {
		return errors.New("绑定的主项目不存在")
	}
	return nil
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
	agent := b.toAgent(name, "")

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
		status := http.StatusInternalServerError
		if errors.Is(err, services.ErrSSHMetaVarsRef) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	a, _ := h.Agents.Get(name)
	c.JSON(http.StatusCreated, a)
}

// PatchAgentProject handles PATCH /api/agents/:name/project.
// Group-level assign: only changes projectId (via UpdateProjectID). Empty
// projectId is rejected — unbind stays on the full SaveAgent path.
func (h *Handlers) PatchAgentProject(c *gin.Context) {
	name := c.Param("name")
	var b struct {
		ProjectID string `json:"projectId"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	projectID := strings.TrimSpace(b.ProjectID)
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "组级指定不支持解绑主项目"})
		return
	}
	prev, ok := h.Agents.Get(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	oldProjectID := strings.TrimSpace(prev.ProjectID)
	agent := prev
	agent.ProjectID = projectID
	if err := h.validateAgentProjectBinding(agent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if oldProjectID != "" && oldProjectID != projectID && h.Pm != nil {
		if err := h.Pm.PurgeAgentProjectData(oldProjectID, name); err != nil {
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "清除旧项目数据失败：" + err.Error()})
			return
		}
	}
	if err := h.Agents.UpdateProjectID(name, projectID); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "saved", "projectId": projectID})
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
	agent := b.toAgent(name, oldProjectID)
	if err := h.validateAgentProjectBinding(agent); err != nil {
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
		status := http.StatusInternalServerError
		if errors.Is(err, services.ErrSSHMetaVarsRef) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
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

	if h.Org != nil {
		if err := h.Org.OnDeleteAgent(name); err != nil {
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
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
	if h.Org != nil {
		if err := h.Org.OnRenameAgent(old, name); err != nil {

			if rbErr := h.Agents.Rename(name, old); rbErr != nil {
				_ = c.Error(err)
				_ = c.Error(rbErr)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": err.Error() + "; rename rollback failed: " + rbErr.Error(),
				})
				return
			}
			if h.Pm != nil && name != old {
				if rbData := h.Pm.RenameAgentScopedData(name, old); rbData != nil {
					_ = c.Error(rbData)
				}
			}
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
			if h.Org != nil {
				if rbOrg := h.Org.OnRenameAgent(name, old); rbOrg != nil {
					_ = c.Error(rbOrg)
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

// GetAgentsOrg returns the central Agent organization index.
func (h *Handlers) GetAgentsOrg(c *gin.Context) {
	if h.Org == nil {
		c.JSON(http.StatusOK, services.AgentOrg{Groups: []services.OrgGroup{}, Agents: map[string]services.OrgAgentMembership{}})
		return
	}
	org, err := h.Org.Get()
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, org)
}

type putAgentsOrgBody struct {
	Revision int                                    `json:"revision"`
	Groups   []services.OrgGroup                    `json:"groups"`
	Agents   map[string]services.OrgAgentMembership `json:"agents"`
}

// PutAgentsOrg replaces the organization index (optimistic concurrency via revision).
func (h *Handlers) PutAgentsOrg(c *gin.Context) {
	if h.Org == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "org service unavailable"})
		return
	}
	var b putAgentsOrgBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	org, err := h.Org.Put(services.AgentOrg{
		Groups: b.Groups,
		Agents: b.Agents,
	}, b.Revision)
	if err != nil {
		if errors.Is(err, services.ErrOrgConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, services.ErrOrgValidation) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, org)
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

	agent, err := h.Agents.ImportZIP(raw, targetName, mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.validateAgentProjectBinding(agent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, agent)
}
