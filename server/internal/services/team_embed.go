package services

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

//go:embed all:team_embed
var teamEmbedFS embed.FS

const teamEmbedRoot = "team_embed"

// TeamRoleTemplate describes one built-in workflow Agent template.
type TeamRoleTemplate struct {
	ID          string `json:"id"`
	EmbedName   string `json:"embedName"`
	RoleLabelZH string `json:"roleLabelZh"`
	Summary     string `json:"summary"`
	// Capabilities is the template's declared capability set (from agent.json).
	Capabilities *models.AgentCapabilities `json:"capabilities,omitempty"`
}

// TeamPMEmbedName is the embedded PM Leader package under team_embed/.
const TeamPMEmbedName = "PMAgent"

// TeamEngineerTemplates are the built-in workflow Agents, in workflow order.
var TeamEngineerTemplates = []TeamRoleTemplate{
	{ID: "clarify", EmbedName: "ClarifyAgent", RoleLabelZH: "需求澄清", Summary: "多轮对话澄清需求、写出计划;缺陷时查清根因,可启动应用演示"},
	{ID: "implement", EmbedName: "ImplementAgent", RoleLabelZH: "实现", Summary: "按计划实现、测试、提交并推送工作分支"},
	{ID: "test_review", EmbedName: "TestReviewAgent", RoleLabelZH: "测试评审", Summary: "执行测试并做代码评审,两项都通过才放行,测试后可启动应用复审"},
	{ID: "deliver", EmbedName: "DeliverAgent", RoleLabelZH: "交付", Summary: "测试评审通过后合入目标分支并创建或复用 MR/PR"},
}

// TeamEmbedPackageNames lists all packages under team_embed/.
func TeamEmbedPackageNames() []string {
	out := []string{TeamPMEmbedName}
	for _, r := range TeamEngineerTemplates {
		out = append(out, r.EmbedName)
	}
	return out
}

// AllCreateTemplates returns the built-in templates with their capabilities.
func AllCreateTemplates() []TeamRoleTemplate {
	out := make([]TeamRoleTemplate, 0, len(TeamEngineerTemplates))
	for _, t := range TeamEngineerTemplates {
		if tmpl, err := loadTeamAgentTemplate(t.EmbedName); err == nil {
			t.Capabilities = tmpl.Capabilities
		}
		out = append(out, t)
	}
	return out
}

// TeamRoleByID returns a built-in template by id.
func TeamRoleByID(id string) (TeamRoleTemplate, bool) {
	id = strings.TrimSpace(id)
	for _, t := range TeamEngineerTemplates {
		if t.ID == id {
			return t, true
		}
	}
	return TeamRoleTemplate{}, false
}

// EngineerDisplayName builds "{prefix}{roleLabel}".
func EngineerDisplayName(prefix, roleLabelZH string) string {
	return strings.TrimSpace(prefix) + strings.TrimSpace(roleLabelZH)
}

// PMDisplayName builds "{prefix}项目经理".
func PMDisplayName(prefix string) string {
	return strings.TrimSpace(prefix) + "项目经理"
}

func loadTeamAgentTemplate(embedName string) (Agent, error) {
	embedName = strings.TrimSpace(embedName)
	if embedName == "" {
		return Agent{}, fmt.Errorf("empty team template name")
	}
	cfgPath := path.Join(teamEmbedRoot, embedName, "agent.json")
	raw, err := teamEmbedFS.ReadFile(cfgPath)
	if err != nil {
		return Agent{}, fmt.Errorf("read team embed agent.json for %s: %w", embedName, err)
	}
	var cfg agentConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Agent{}, fmt.Errorf("parse team embed agent.json for %s: %w", embedName, err)
	}
	files, err := readTeamEmbedWorkspaceFiles(path.Join(teamEmbedRoot, embedName, WorkDirName))
	if err != nil {
		return Agent{}, err
	}
	env := map[string]string{}
	for k, v := range cfg.Env {
		env[k] = v
	}
	if _, ok := env["GIT_REPOS"]; !ok {
		env["GIT_REPOS"] = "${vars.repos}"
	}
	mcp := cfg.MCP
	if len(mcp) == 0 {
		mcp = DefaultPlatformMCP()
	}
	layout := AgentLayout{}
	if cfg.Layout != nil {
		layout = *cfg.Layout
	}
	return Agent{
		Name:         embedName,
		AcpBackend:   NormalizeAcpBackend(cfg.AcpBackend),
		Files:        files,
		MCP:          mcp,
		Env:          env,
		Layout:       layout,
		Capabilities: cfg.Capabilities,
	}, nil
}

// LoadTeamAgentTemplate loads an embedded pack by folder name (e.g. ImplementAgent).
func LoadTeamAgentTemplate(embedName string) (Agent, error) {
	return loadTeamAgentTemplate(embedName)
}

// ApplyCreateTemplate overlays an embedded role pack onto a new Agent.
// Workspace files come from the pack; any client-supplied files (e.g. auth config)
// are merged on top. Request env/MCP/backend overlay pack defaults.
func ApplyCreateTemplate(templateID string, agent *Agent) error {
	role, ok := TeamRoleByID(templateID)
	if !ok {
		return fmt.Errorf("unknown templateId %s", templateID)
	}
	tmpl, err := loadTeamAgentTemplate(role.EmbedName)
	if err != nil {
		return err
	}
	extras := agent.Files
	agent.Files = tmpl.Files
	for _, f := range extras {
		if strings.TrimSpace(f.Path) == "" {
			continue
		}
		agent.Files = upsertAgentFile(agent.Files, f.Path, f.Content)
	}
	if len(agent.MCP) == 0 {
		agent.MCP = tmpl.MCP
	}
	mergedEnv := map[string]string{}
	for k, v := range tmpl.Env {
		mergedEnv[k] = v
	}
	for k, v := range agent.Env {
		k = strings.TrimSpace(k)
		if k != "" {
			mergedEnv[k] = v
		}
	}
	agent.Env = mergedEnv
	if strings.TrimSpace(agent.AcpBackend) == "" {
		agent.AcpBackend = tmpl.AcpBackend
	}
	if strings.TrimSpace(agent.Layout.ConfigRoot) == "" && strings.TrimSpace(tmpl.Layout.ConfigRoot) != "" {
		agent.Layout.ConfigRoot = tmpl.Layout.ConfigRoot
	}
	if strings.TrimSpace(agent.Layout.WorkspaceDir) == "" {
		if strings.TrimSpace(tmpl.Layout.WorkspaceDir) != "" {
			agent.Layout.WorkspaceDir = tmpl.Layout.WorkspaceDir
		} else {
			agent.Layout.WorkspaceDir = DefaultWorkspaceDir
		}
	}
	if agent.Capabilities == nil {
		agent.Capabilities = tmpl.Capabilities.Clone()
	}
	return nil
}

func readTeamEmbedWorkspaceFiles(root string) ([]AgentFile, error) {
	if _, err := fs.Stat(teamEmbedFS, root); err != nil {
		return nil, fmt.Errorf("team embed workspace missing %s: %w (rebuild image with team_embed/*.md included)", root, err)
	}
	var out []AgentFile
	prefix := strings.TrimSuffix(root, "/") + "/"
	err := fs.WalkDir(teamEmbedFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, prefix)
		rel = path.Clean(rel)
		if rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
			return nil
		}
		b, err := teamEmbedFS.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, AgentFile{Path: rel, Content: string(b)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk team embed workspace %s: %w", root, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("team embed workspace empty %s (check Docker .md allowlist for team_embed)", root)
	}
	return out, nil
}

// loadDefaultWorkflowEnvelope reads the built-in default workflow
// (需求澄清 → 实现 → 测试评审 → 交付, fail → 实现).
func loadDefaultWorkflowEnvelope() (models.ExportEnvelope, error) {
	raw, err := teamEmbedFS.ReadFile(path.Join(teamEmbedRoot, "default-workflow.json"))
	if err != nil {
		return models.ExportEnvelope{}, fmt.Errorf("read default workflow embed: %w", err)
	}
	env, err := ValidateImport(raw)
	if err != nil {
		return models.ExportEnvelope{}, err
	}
	env.Name = OnboardingWorkflowName
	env.NeedsRepo = true
	return env, nil
}

// onboardingTemplateAgent loads a built-in template as the Agent onboarding saves.
func onboardingTemplateAgent(role TeamRoleTemplate) (Agent, error) {
	tmpl, err := loadTeamAgentTemplate(role.EmbedName)
	if err != nil {
		return Agent{}, err
	}
	tmpl.Env = stripTokenKeysFromEnvMap(tmpl.Env)
	return tmpl, nil
}
