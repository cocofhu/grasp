package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
)

const (
	// OnboardingWorkflowName is the first-install published workflow.
	OnboardingWorkflowName = "默认工作流"
	// FirstInstallGroupName is the org folder created on first install.
	FirstInstallGroupName = "综合项目组"
	// FirstInstallGroupID is a stable group id so re-bootstrap is idempotent.
	FirstInstallGroupID = "g_first_install_zonghe"
)

var (
	// ErrOnboardingAPIKeyRequired is returned when bootstrap is called without an API key.
	ErrOnboardingAPIKeyRequired = errors.New("apiKey is required")
	// ErrOnboardingProjectNotFound is returned when the project id does not exist.
	ErrOnboardingProjectNotFound = errors.New("project not found")
	// ErrOnboardingAgentConflict is returned when a fixed-name agent already belongs to another project.
	ErrOnboardingAgentConflict = errors.New("onboarding agent already bound to another project")
	// ErrOnboardingNotDefaultProject was used when bootstrap was default-only.
	// Kept for error-string compatibility with older clients; Bootstrap no longer returns it.
	ErrOnboardingNotDefaultProject = errors.New("first-install onboarding is only allowed on the default project")
	// ErrBaselineReposRequired is returned when no non-empty repository URL is submitted.
	ErrBaselineReposRequired = errors.New("at least one repository URL is required")
)

// OnboardingBootstrapRequest is the body for POST .../bootstrap-onboarding.
type OnboardingBootstrapRequest struct {
	AcpBackend          string `json:"acpBackend"`
	APIKey              string `json:"apiKey"`
	Region              string `json:"region,omitempty"`
	OpenCodeProvider    string `json:"openCodeProvider,omitempty"`
	OpenCodeBaseURL     string `json:"openCodeBaseURL,omitempty"`
	OpenCodeModel       string `json:"openCodeModel,omitempty"`
	OpenCodeModelVision *bool  `json:"openCodeModelVision,omitempty"`
	GitCredentialType   string `json:"gitCredentialType,omitempty"`
	GitHubToken         string `json:"githubToken,omitempty"`
	GitLabToken         string `json:"gitlabToken,omitempty"`
	GitLabURL           string `json:"gitlabUrl,omitempty"`
	GitSshPrivateKey    string `json:"gitSshPrivateKey,omitempty"`
	GitSshKnownHosts    string `json:"gitSshKnownHosts,omitempty"`
	RepoURL             string `json:"repoUrl,omitempty"`
	RepoBranch          string `json:"repoBranch,omitempty"`
	GitUserName         string `json:"gitUserName,omitempty"`
	GitUserEmail        string `json:"gitUserEmail,omitempty"`
	// VncPreview / BrowserMcp default on when omitted (first-install preview stack).
	VncPreview *bool `json:"vncPreview"`
	BrowserMcp *bool `json:"browserMcp"`
}

// OnboardingBootstrapResult is returned after a successful (idempotent) bootstrap.
type OnboardingBootstrapResult struct {
	AgentIDs   []string `json:"agentIds"`
	WorkflowID string   `json:"workflowId"`
	Published  bool     `json:"published"`
	GroupName  string   `json:"groupName,omitempty"`
}

// BaselineRepo is one repository injected into the embedded default workflow.
type BaselineRepo struct {
	URL    string `json:"url"`
	Name   string `json:"name,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// CreateBaselineWorkflowRequest is the body for POST /api/workflows/from-baseline.
type CreateBaselineWorkflowRequest struct {
	ProjectID string         `json:"projectId"`
	Name      string         `json:"name"`
	Repos     []BaselineRepo `json:"repos"`
}

// OnboardingService bootstraps first-install auth + 综合项目组 + 默认工作流.
type OnboardingService struct {
	Projects    *ProjectService
	Skills      *AgentService
	SharedAgent *SharedAgentService
	WF          *WorkflowService
	Org         *OrgService
}

// NewOnboardingService wires dependencies. org may be nil (agents still saved).
func NewOnboardingService(projects *ProjectService, skills *AgentService, shared *SharedAgentService, wf *WorkflowService, org *OrgService) *OnboardingService {
	return &OnboardingService{Projects: projects, Skills: skills, SharedAgent: shared, WF: wf, Org: org}
}

// CreateFromBaseline clones the embedded first-install workflow into a new
// published workflow. It does not modify the existing onboarding workflow.
func (s *OnboardingService) CreateFromBaseline(req CreateBaselineWorkflowRequest) (models.WorkflowDef, error) {
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return models.WorkflowDef{}, ErrWorkflowProjectRequired
	}
	if _, ok := s.Projects.Get(projectID); !ok {
		return models.WorkflowDef{}, ErrWorkflowProjectNotFound
	}
	name := strings.TrimSpace(req.Name)
	if err := s.WF.validateWorkflowName(name, "", projectID); err != nil {
		return models.WorkflowDef{}, err
	}
	repos := normalizeBaselineRepos(req.Repos)
	if len(repos) == 0 {
		return models.WorkflowDef{}, ErrBaselineReposRequired
	}
	envelope, err := loadFirstInstallWorkflowEnvelope()
	if err != nil {
		return models.WorkflowDef{}, err
	}
	applyBaselineRepos(&envelope.Graph, repos)
	LiftInputVariables(&envelope.Graph)
	MigrateOutputNodes(&envelope.Graph)
	if err := envelope.Graph.Validate(); err != nil {
		return models.WorkflowDef{}, fmt.Errorf("default workflow graph invalid: %w", err)
	}

	wf := models.WorkflowDef{
		ID:          "wf-" + uuid.NewString()[:8],
		ProjectID:   projectID,
		Name:        name,
		Description: "从默认基线创建。仓库可在启动运行时调整。",
		Status:      "draft",
		Version:     1,
		NeedsRepo:   true,
		Graph:       envelope.Graph,
	}
	if err := s.WF.Save(&wf); err != nil {
		return models.WorkflowDef{}, err
	}
	published, err := s.WF.Publish(wf.ID)
	if err != nil {
		_ = s.WF.Delete(wf.ID)
		return models.WorkflowDef{}, fmt.Errorf("publish baseline workflow: %w", err)
	}
	// Align with Bootstrap: Save still forces showOnHome=false; open Home after publish.
	published, err = s.WF.UpdateShowOnHome(published.ID, true)
	if err != nil {
		_ = s.WF.Delete(published.ID)
		return models.WorkflowDef{}, fmt.Errorf("show workflow on home: %w", err)
	}
	return published, nil
}

func normalizeBaselineRepos(input []BaselineRepo) []BaselineRepo {
	out := make([]BaselineRepo, 0, len(input))
	seen := map[string]bool{}
	for _, repo := range input {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			continue
		}
		base := strings.TrimSpace(repo.Name)
		if base == "" || base == "." || base == ".." || strings.ContainsAny(base, "/\\") {
			base = sandbox.RepoNameFromURL(url)
		}
		if base == "" || base == "." || base == ".." || strings.ContainsAny(base, "/\\") {
			base = "repo"
		}
		name := base
		for suffix := 2; seen[name]; suffix++ {
			name = fmt.Sprintf("%s-%d", base, suffix)
		}
		seen[name] = true
		out = append(out, BaselineRepo{URL: url, Name: name, Branch: strings.TrimSpace(repo.Branch)})
	}
	return out
}

func applyBaselineRepos(graph *models.Graph, repos []BaselineRepo) {
	if graph == nil {
		return
	}
	value := make([]any, 0, len(repos))
	for _, repo := range repos {
		value = append(value, map[string]any{
			"url": repo.URL, "name": repo.Name, "branch": repo.Branch,
		})
	}
	for i := range graph.Variables {
		if graph.Variables[i].Name == "repos" {
			graph.Variables[i].Value = value
			return
		}
	}
}

// Bootstrap writes shared-agent env auth, saves the install-group agents, and publishes
// 默认工作流. Allowed on any project: default keeps 综合* names; others derive names
// from the project name. Idempotent within a project. Cross-project name conflicts
// are rejected with ErrOnboardingAgentConflict. It never starts a Run. Missing
// apiKey rejects without creating resources.
func (s *OnboardingService) Bootstrap(projectID string, req OnboardingBootstrapRequest) (OnboardingBootstrapResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return OnboardingBootstrapResult{}, ErrOnboardingProjectNotFound
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		if NormalizeAcpBackend(req.AcpBackend) != AcpBackendCodex {
			return OnboardingBootstrapResult{}, ErrOnboardingAPIKeyRequired
		}
		if _, err := runtime.PrepareAuthEnv(runtime.BackendCodex, nil, ""); err != nil {
			return OnboardingBootstrapResult{}, err
		}
	}
	proj, ok := s.Projects.Get(projectID)
	if !ok {
		return OnboardingBootstrapResult{}, ErrOnboardingProjectNotFound
	}
	defID := ""
	if s.Projects != nil {
		defID = strings.TrimSpace(s.Projects.DefaultProjectID())
	}
	if defID == "" {
		defID = models.DefaultProjectID
	}

	plan, err := BuildOnboardingNamePlan(projectID, proj.Name, defID)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}

	backend := NormalizeAcpBackend(req.AcpBackend)
	region := strings.TrimSpace(req.Region)

	if err := s.checkOnboardingAgentConflicts(projectID, plan.AgentNames); err != nil {
		return OnboardingBootstrapResult{}, err
	}

	envelope, err := loadFirstInstallWorkflowEnvelope()
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	RemapOnboardingAgentProfiles(&envelope.Graph, plan.NameMap)

	templates := make([]Agent, 0, len(OnboardingAgentNames))
	for _, canonical := range OnboardingAgentNames {
		tmpl, err := loadFirstInstallAgentTemplate(canonical)
		if err != nil {
			return OnboardingBootstrapResult{}, err
		}
		tmpl.Name = plan.NameMap[canonical]
		tmpl.ProjectID = projectID
		tmpl.AcpBackend = backend
		tmpl.Layout.ConfigRoot = DefaultConfigRootForBackend(backend)
		if strings.TrimSpace(tmpl.Layout.WorkspaceDir) == "" {
			tmpl.Layout.WorkspaceDir = DefaultWorkspaceDir
		}
		if tmpl.Env == nil {
			tmpl.Env = map[string]string{}
		}
		tmpl.Env = stripTokenKeysFromEnvMap(tmpl.Env)
		delete(tmpl.Env, runtime.EnvCodeBuddyRegion)
		delete(tmpl.Env, runtime.EnvTraeRegion)
		templates = append(templates, tmpl)
	}

	if err := s.writeProjectAuth(projectID, backend, apiKey, region, req); err != nil {
		return OnboardingBootstrapResult{}, err
	}

	agentIDs := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		if err := s.Skills.Save(tmpl); err != nil {
			return OnboardingBootstrapResult{}, fmt.Errorf("save agent %s: %w", tmpl.Name, err)
		}
		agentIDs = append(agentIDs, tmpl.Name)
	}

	if err := s.ensureOnboardingOrg(plan.GroupID, plan.GroupName, agentIDs); err != nil {
		return OnboardingBootstrapResult{}, err
	}

	applyOnboardingRepo(&envelope.Graph, req.RepoURL, req.RepoBranch)

	wf, err := s.upsertDefaultWorkflow(projectID, envelope)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	published, err := s.WF.Publish(wf.ID)
	if err != nil {
		return OnboardingBootstrapResult{}, fmt.Errorf("publish workflow: %w", err)
	}
	published, err = s.WF.UpdateShowOnHome(published.ID, true)
	if err != nil {
		return OnboardingBootstrapResult{}, fmt.Errorf("show workflow on home: %w", err)
	}

	return OnboardingBootstrapResult{
		AgentIDs:   agentIDs,
		WorkflowID: published.ID,
		Published:  published.Status == "published",
		GroupName:  plan.GroupName,
	}, nil
}

func (s *OnboardingService) checkOnboardingAgentConflicts(projectID string, agentNames []string) error {
	for _, name := range agentNames {
		existing, ok := s.Skills.Get(name)
		if !ok {
			continue
		}
		owner := strings.TrimSpace(existing.ProjectID)
		if owner != "" && owner != projectID {
			return fmt.Errorf("%w: %s owned by project %s", ErrOnboardingAgentConflict, name, owner)
		}
	}
	return nil
}

func (s *OnboardingService) writeProjectAuth(projectID, backend, apiKey, region string, req OnboardingBootstrapRequest) error {
	if s.SharedAgent == nil {
		return fmt.Errorf("shared agent service unavailable")
	}
	cfg := s.SharedAgent.Get(projectID)
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	cfg.ProjectID = projectID
	cfg.AcpBackend = backend
	cfg.Env[primaryAuthEnvKey(backend)] = apiKey
	switch backend {
	case AcpBackendCodeBuddy:
		if region == "" {
			region = "public"
		}
		cfg.Env[runtime.EnvCodeBuddyRegion] = region
	case AcpBackendTrae:
		if region == "" {
			region = "intl"
		}
		cfg.Env[runtime.EnvTraeRegion] = region
	case AcpBackendOpenCode:
		applyOpenCodeSharedEnv(cfg.Env, req)
	}
	cred := strings.TrimSpace(req.GitCredentialType)
	if cred != "" {
		cfg.GitCredentialType = cred
	}
	if v := strings.TrimSpace(req.GitHubToken); v != "" {
		cfg.Env["GITHUB_TOKEN"] = v
	}
	if v := strings.TrimSpace(req.GitLabToken); v != "" {
		cfg.Env["GITLAB_TOKEN"] = v
	}
	if v := strings.TrimSpace(req.GitLabURL); v != "" {
		cfg.Env["GITLAB_URL"] = v
	}
	if err := ValidateAgentSSHMeta(req.GitSshKnownHosts, req.GitSshPrivateKey); err != nil {
		return err
	}
	if v := strings.TrimSpace(req.GitSshPrivateKey); v != "" {
		cfg.GitSshPrivateKey = v
	}
	if v := strings.TrimSpace(req.GitSshKnownHosts); v != "" {
		cfg.GitSshKnownHosts = v
	}
	if v := strings.TrimSpace(req.GitUserName); v != "" {
		cfg.Env["GIT_USER_NAME"] = v
	}
	if v := strings.TrimSpace(req.GitUserEmail); v != "" {
		cfg.Env["GIT_USER_EMAIL"] = v
	}
	if boolOrDefault(req.VncPreview, true) {
		cfg.Env["VNC_PREVIEW"] = "1"
	} else {
		cfg.Env["VNC_PREVIEW"] = "0"
	}
	if boolOrDefault(req.BrowserMcp, true) {
		cfg.Env["BROWSER_MCP"] = "1"
	} else {
		cfg.Env["BROWSER_MCP"] = "0"
	}
	return s.SharedAgent.Save(cfg)
}

func boolOrDefault(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func primaryAuthEnvKey(backend string) string {
	switch NormalizeAcpBackend(backend) {
	case AcpBackendCodex:
		return "OPENAI_API_KEY"
	case AcpBackendClaudeCode:
		return "GRASP_CLAUDE_API_KEY"
	case AcpBackendCodeBuddy:
		return "GRASP_CODEBUDDY_API_KEY"
	case AcpBackendTrae:
		return "GRASP_TRAE_API_KEY"
	case AcpBackendOpenCode:
		return "GRASP_OPENCODE_API_KEY"
	default:
		return "GRASP_CURSOR_API_KEY"
	}
}

func agentAuthConfigFileName(backend string) string {
	if NormalizeAcpBackend(backend) == AcpBackendOpenCode {
		return "opencode.json"
	}
	return "settings.json"
}

func (s *OnboardingService) ensureFirstInstallOrg(agentNames []string) error {
	return s.ensureOnboardingOrg(FirstInstallGroupID, FirstInstallGroupName, agentNames)
}

func (s *OnboardingService) ensureOnboardingOrg(groupID, groupName string, agentNames []string) error {
	if s.Org == nil {
		return nil
	}
	groupID = strings.TrimSpace(groupID)
	groupName = strings.TrimSpace(groupName)
	if groupID == "" {
		groupID = FirstInstallGroupID
	}
	if groupName == "" {
		groupName = FirstInstallGroupName
	}
	org, err := s.Org.Get()
	if err != nil {
		return err
	}
	// Match by stable group id only. Name-based reuse would merge two same-named
	// projects into one org folder (both derive "{projectName}项目组").
	gid := groupID
	found := false
	for _, g := range org.Groups {
		if g.ID == groupID {
			gid = g.ID
			found = true
			break
		}
	}
	if !found {
		org.Groups = append(org.Groups, OrgGroup{ID: groupID, Name: groupName})
		gid = groupID
	} else {
		// Keep display name current for derived groups on re-bootstrap.
		for i := range org.Groups {
			if org.Groups[i].ID == gid {
				org.Groups[i].Name = groupName
				break
			}
		}
	}
	if org.Agents == nil {
		org.Agents = map[string]OrgAgentMembership{}
	}
	for _, name := range agentNames {
		org.Agents[name] = OrgAgentMembership{GroupIDs: []string{gid}}
	}
	_, err = s.Org.Put(org, org.Revision)
	return err
}

func (s *OnboardingService) upsertDefaultWorkflow(projectID string, envelope models.ExportEnvelope) (models.WorkflowDef, error) {
	graph := envelope.Graph
	LiftInputVariables(&graph)
	MigrateOutputNodes(&graph)
	if err := graph.Validate(); err != nil {
		return models.WorkflowDef{}, fmt.Errorf("default workflow graph invalid: %w", err)
	}

	var existing *models.WorkflowDef
	for _, wf := range s.WF.List(projectID) {
		if wf.Name == OnboardingWorkflowName {
			full, ok := s.WF.Get(wf.ID)
			if !ok {
				continue
			}
			existing = &full
			break
		}
	}
	desc := strings.TrimSpace(envelope.Description)
	if desc == "" {
		desc = "第一次安装默认工作流。仓库与凭据在运行时 / 共享 Agent 配置中填写。"
	}
	if existing != nil {
		existing.Description = desc
		existing.NeedsRepo = true
		existing.Graph = graph
		if err := s.WF.Save(existing); err != nil {
			return models.WorkflowDef{}, err
		}
		return *existing, nil
	}
	wf := models.WorkflowDef{
		ID:          uuid.NewString(),
		ProjectID:   projectID,
		Name:        OnboardingWorkflowName,
		Description: desc,
		Status:      "draft",
		Version:     1,
		NeedsRepo:   true,
		Graph:       graph,
	}
	if err := s.WF.Save(&wf); err != nil {
		return models.WorkflowDef{}, err
	}
	return wf, nil
}

// applyOnboardingRepo fills the default workflow's `repos` variable from the
// wizard. An empty URL leaves the shipped blank row so the launcher still asks
// for a repo at run start; the name is derived the same way the sandbox does.
func applyOnboardingRepo(graph *models.Graph, url, branch string) {
	url = strings.TrimSpace(url)
	if url == "" || graph == nil {
		return
	}
	name := sandbox.RepoNameFromURL(url)
	if name == "" {
		name = "repo"
	}
	row := map[string]any{"url": url, "name": name, "branch": strings.TrimSpace(branch)}
	for i := range graph.Variables {
		if graph.Variables[i].Name != "repos" {
			continue
		}
		graph.Variables[i].Value = []any{row}
		return
	}
}

// applyOnboardingAgentRegion writes the Studio-managed region env key into an
// Agent env map for backends that require it (CodeBuddy / Trae). Empty region
// falls back to the same defaults as writeProjectAuth / web regionPolicy.
func applyOnboardingAgentRegion(env map[string]string, backend, region string) {
	if env == nil {
		return
	}
	switch NormalizeAcpBackend(backend) {
	case AcpBackendCodeBuddy:
		if region == "" {
			region = "public"
		}
		env[runtime.EnvCodeBuddyRegion] = region
	case AcpBackendTrae:
		if region == "" {
			region = "intl"
		}
		env[runtime.EnvTraeRegion] = region
	}
}

func applyOpenCodeSharedEnv(env map[string]string, req OnboardingBootstrapRequest) {
	if env == nil {
		return
	}
	provider := runtime.NormalizeOpenCodeProvider(req.OpenCodeProvider)
	env[runtime.EnvOpenCodeProvider] = provider
	if v := strings.TrimSpace(req.OpenCodeBaseURL); v != "" {
		env[runtime.EnvOpenCodeBaseURL] = v
	}
	if v := strings.TrimSpace(req.OpenCodeModel); v != "" {
		env[runtime.EnvACPBridgeModel] = v
	}
	if boolOrDefault(req.OpenCodeModelVision, false) {
		env[runtime.EnvOpenCodeModelVision] = "1"
	} else {
		delete(env, runtime.EnvOpenCodeModelVision)
	}
}
