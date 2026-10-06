package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
)

const (
	// OnboardingWorkflowName is the first-install published workflow in Chinese;
	// see onboardingLocales for every language.
	OnboardingWorkflowName = "默认工作流"
	// OnboardingWorkflowNameEN is the first-install published workflow in English.
	OnboardingWorkflowNameEN = "Default Workflow"
)

var (
	// ErrOnboardingAPIKeyRequired is returned when bootstrap is called without an API key.
	ErrOnboardingAPIKeyRequired = errors.New("apiKey is required")
	// ErrOnboardingProjectNotFound is returned when the project id does not exist.
	ErrOnboardingProjectNotFound = errors.New("project not found")
	// ErrOnboardingAgentConflict is returned when a fixed-name agent already belongs to another project.
	ErrOnboardingAgentConflict = errors.New("onboarding agent already bound to another project")
	// ErrBaselineReposRequired is returned when no non-empty repository URL is submitted.
	ErrBaselineReposRequired = errors.New("at least one repository URL is required")
	// ErrOnboardingInvalidTeam is returned when the chosen templates are unknown,
	// duplicated, miss a required template, or produce duplicate Agent names.
	ErrOnboardingInvalidTeam = errors.New("invalid onboarding team")
)

// OnboardingBootstrapRequest is the body for POST .../bootstrap-onboarding.
type OnboardingBootstrapRequest struct {
	AcpBackend string `json:"acpBackend"`
	APIKey     string `json:"apiKey"`
	// Language is the wizard's UI language (zh-CN | en); it names the default
	// workflow and its start/end nodes. Empty means Chinese.
	Language            string `json:"language,omitempty"`
	Region              string `json:"region,omitempty"`
	OpenCodeProvider    string `json:"openCodeProvider,omitempty"`
	OpenCodeBaseURL     string `json:"openCodeBaseURL,omitempty"`
	OpenCodeModel       string `json:"openCodeModel,omitempty"`
	OpenCodeModelVision *bool  `json:"openCodeModelVision,omitempty"`
	GitCredentialType   string `json:"gitCredentialType,omitempty"`
	GitHubToken         string `json:"githubToken,omitempty"`
	GitLabToken         string `json:"gitlabToken,omitempty"`
	GitLabURL           string `json:"gitlabUrl,omitempty"`
	SSHPrivateKey       string `json:"sshPrivateKey,omitempty"`
	SSHKnownHosts       string `json:"sshKnownHosts,omitempty"`
	RepoURL             string `json:"repoUrl,omitempty"`
	RepoBranch          string `json:"repoBranch,omitempty"`
	GitUserName         string `json:"gitUserName,omitempty"`
	GitUserEmail        string `json:"gitUserEmail,omitempty"`
	// VncPreview / BrowserMcp default on when omitted (first-install preview stack).
	VncPreview *bool `json:"vncPreview"`
	BrowserMcp *bool `json:"browserMcp"`
	// Agents picks the built-in templates to create. Empty = all templates with
	// derived names. clarify and implement are required; test_review and deliver are optional.
	Agents []OnboardingAgentChoice `json:"agents,omitempty"`
}

// OnboardingAgentChoice is one chosen built-in template in the wizard's team step.
type OnboardingAgentChoice struct {
	TemplateID string `json:"templateId"`
	// Name overrides the derived Agent name when non-empty.
	Name string `json:"name,omitempty"`
	// Model is written to the Agent's ACP_BRIDGE_MODEL env when non-empty.
	Model string `json:"model,omitempty"`
}

// OnboardingRequiredTemplateIDs must always be part of the onboarding team.
var OnboardingRequiredTemplateIDs = []string{"clarify", "implement"}

// OnboardingBootstrapResult is returned after a successful (idempotent) bootstrap.
type OnboardingBootstrapResult struct {
	AgentIDs   []string `json:"agentIds"`
	WorkflowID string   `json:"workflowId"`
	Published  bool     `json:"published"`
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

// OnboardingService bootstraps first-install auth + the built-in Agents + 默认工作流.
type OnboardingService struct {
	Projects    *ProjectService
	Skills      *AgentService
	SharedAgent *SharedAgentService
	WF          *WorkflowService
	Credentials *ProjectCredentialService
}

// NewOnboardingService wires dependencies. credentials receives every secret the
// wizard submits (AI key, Git tokens, SSH).
func NewOnboardingService(projects *ProjectService, skills *AgentService, shared *SharedAgentService, wf *WorkflowService, credentials *ProjectCredentialService) *OnboardingService {
	return &OnboardingService{Projects: projects, Skills: skills, SharedAgent: shared, WF: wf, Credentials: credentials}
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
	envelope, err := loadDefaultWorkflowEnvelope()
	if err != nil {
		return models.WorkflowDef{}, err
	}
	applyBaselineRepos(&envelope.Graph, repos)
	if err := envelope.Graph.Validate(); err != nil {
		return models.WorkflowDef{}, fmt.Errorf("default workflow graph invalid: %w", err)
	}

	wf := models.WorkflowDef{
		ID:          "wf-" + uuid.NewString()[:8],
		ProjectID:   projectID,
		Name:        name,
		Description: "从默认基线创建。仓库可在启动运行时调整。",
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
// 默认工作流. Allowed on any project: default keeps the template names; others derive names
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
		return OnboardingBootstrapResult{}, ErrOnboardingAPIKeyRequired
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

	loc := onboardingLocaleFor(req.Language)
	plan, err := BuildOnboardingNamePlan(projectID, proj.Name, defID)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}

	backend, err := ParseAcpBackend(req.AcpBackend)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	region := strings.TrimSpace(req.Region)

	team, err := resolveOnboardingTeam(req.Agents, plan.NameMap)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	teamNames := make([]string, 0, len(team))
	nameMap := make(map[string]string, len(team))
	for _, m := range team {
		teamNames = append(teamNames, m.Name)
		nameMap[m.Role.RoleLabelZH] = m.Name
	}

	if err := s.checkOnboardingAgentConflicts(projectID, teamNames); err != nil {
		return OnboardingBootstrapResult{}, err
	}

	envelope, err := loadDefaultWorkflowEnvelope()
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	localizeOnboardingWorkflow(&envelope, loc)
	assignOnboardingAgentProfiles(&envelope.Graph, nameMap)
	for _, role := range TeamEngineerTemplates {
		if _, ok := nameMap[role.RoleLabelZH]; !ok {
			dropOnboardingAgentNode(&envelope.Graph, role.ID)
		}
	}

	templates := make([]Agent, 0, len(team))
	for _, m := range team {
		tmpl, err := onboardingTemplateAgent(m.Role)
		if err != nil {
			return OnboardingBootstrapResult{}, err
		}
		tmpl.Name = m.Name
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
		if m.Model != "" {
			tmpl.Env[runtime.EnvACPBridgeModel] = m.Model
		}
		if backend == AcpBackendOpenCode {
			tmpl.OpenCodeCredentialID = defaultOpenCodeCredentialID(projectID)
		}
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
		Published:  published.Status() == models.WorkflowStatusPublished,
	}, nil
}

type onboardingTeamMember struct {
	Role  TeamRoleTemplate
	Name  string
	Model string
}

// resolveOnboardingTeam turns the wizard's choices into the Agents to save, in
// workflow order. Empty choices keep every template with its derived name.
func resolveOnboardingTeam(choices []OnboardingAgentChoice, derived map[string]string) ([]onboardingTeamMember, error) {
	picked := map[string]OnboardingAgentChoice{}
	for _, c := range choices {
		id := strings.TrimSpace(c.TemplateID)
		if _, ok := TeamRoleByID(id); !ok {
			return nil, fmt.Errorf("%w: unknown template %q", ErrOnboardingInvalidTeam, id)
		}
		if _, dup := picked[id]; dup {
			return nil, fmt.Errorf("%w: duplicate template %q", ErrOnboardingInvalidTeam, id)
		}
		picked[id] = c
	}
	if len(choices) > 0 {
		for _, id := range OnboardingRequiredTemplateIDs {
			if _, ok := picked[id]; !ok {
				return nil, fmt.Errorf("%w: template %q is required", ErrOnboardingInvalidTeam, id)
			}
		}
	}
	out := make([]onboardingTeamMember, 0, len(TeamEngineerTemplates))
	seen := map[string]bool{}
	for _, role := range TeamEngineerTemplates {
		c, ok := picked[role.ID]
		if len(choices) > 0 && !ok {
			continue
		}
		name := derived[role.RoleLabelZH]
		if custom := strings.TrimSpace(c.Name); custom != "" {
			normalized, err := NormalizeAndValidateAgentName(custom)
			if err != nil {
				return nil, err
			}
			name = normalized
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: duplicate agent name %q", ErrOnboardingInvalidTeam, name)
		}
		seen[name] = true
		out = append(out, onboardingTeamMember{Role: role, Name: name, Model: strings.TrimSpace(c.Model)})
	}
	return out, nil
}

// assignOnboardingAgentProfiles points each template node at its chosen Agent
// name in one pass, so a custom name equal to another template label cannot chain.
// The node label follows the name so the canvas shows what the user chose.
func assignOnboardingAgentProfiles(g *models.Graph, nameMap map[string]string) {
	if g == nil {
		return
	}
	for i := range g.Nodes {
		cfg := g.Nodes[i].Config
		if cfg == nil {
			continue
		}
		if name, ok := nameMap[models.AgentProfile(cfg)]; ok {
			models.SetAgentProfile(cfg, name)
			g.Nodes[i].Label = name
		}
	}
}

// dropOnboardingAgentNode removes an unchosen template node: its forward
// predecessors are wired straight to its forward successors (pass / plain
// outlets), rollback edges into or out of it are dropped, and output results
// that read its products are removed.
func dropOnboardingAgentNode(g *models.Graph, nodeID string) {
	if g == nil {
		return
	}
	idx := -1
	for i, n := range g.Nodes {
		if n.ID == nodeID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	var incoming, outgoing []models.Edge
	kept := make([]models.Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		switch {
		case e.Target == nodeID && e.Source != nodeID:
			if e.KindOrDefault() == models.EdgeSuccess {
				incoming = append(incoming, e)
			}
		case e.Source == nodeID && e.Target != nodeID:
			if e.KindOrDefault() == models.EdgeSuccess && (e.SourceHandle == "" || e.SourceHandle == "pass") {
				outgoing = append(outgoing, e)
			}
		case e.Source == nodeID || e.Target == nodeID:
		default:
			kept = append(kept, e)
		}
	}
	exists := func(src, handle, dst string) bool {
		for _, e := range kept {
			if e.Source == src && e.SourceHandle == handle && e.Target == dst {
				return true
			}
		}
		return false
	}
	for _, in := range incoming {
		for _, out := range outgoing {
			if in.Source == out.Target || exists(in.Source, in.SourceHandle, out.Target) {
				continue
			}
			kept = append(kept, models.Edge{
				ID:           "e_" + in.Source + "_" + out.Target,
				Source:       in.Source,
				SourceHandle: in.SourceHandle,
				Target:       out.Target,
				Kind:         models.EdgeSuccess,
			})
		}
	}
	g.Edges = kept
	g.Nodes = append(g.Nodes[:idx:idx], g.Nodes[idx+1:]...)
	ref := "nodes." + nodeID + "."
	for i := range g.Nodes {
		if g.Nodes[i].Type != "output" || g.Nodes[i].Config == nil {
			continue
		}
		results, ok := g.Nodes[i].Config["results"].([]any)
		if !ok {
			continue
		}
		filtered := make([]any, 0, len(results))
		for _, r := range results {
			if s, ok := r.(string); ok && strings.Contains(s, ref) {
				continue
			}
			filtered = append(filtered, r)
		}
		g.Nodes[i].Config["results"] = filtered
	}
}

func (s *OnboardingService) checkOnboardingAgentConflicts(projectID string, agentNames []string) error {
	for _, name := range agentNames {
		existing, ok := s.Skills.Get(name)
		if !ok {
			continue
		}
		owner := strings.TrimSpace(existing.ProjectID)
		if owner != projectID {
			return fmt.Errorf("%w: %s owned by project %s", ErrOnboardingAgentConflict, name, owner)
		}
	}
	return nil
}

func (s *OnboardingService) writeProjectAuth(projectID, backend, apiKey, region string, req OnboardingBootstrapRequest) error {
	if s.SharedAgent == nil {
		return fmt.Errorf("shared agent service unavailable")
	}
	if s.Credentials == nil {
		return fmt.Errorf("project credential service unavailable")
	}
	cfg := s.SharedAgent.Get(projectID)
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	cfg.ProjectID = projectID
	cfg.AcpBackend = backend
	primaryKey := primaryAuthEnvKey(backend)
	creds := []ProjectCredentialInput{
		{Type: "ai", Provider: backend, Name: aiCredentialName(backend), EnvKey: primaryKey, Value: apiKey},
		{Type: "git", Provider: "github", Name: "GitHub HTTPS Token", EnvKey: "GITHUB_TOKEN", Value: req.GitHubToken},
		{Type: "git", Provider: "gitlab", Name: "GitLab HTTPS Token", EnvKey: "GITLAB_TOKEN", Value: req.GitLabToken},
		{Type: "git", Provider: "gitlab", Name: "GitLab URL", EnvKey: "GITLAB_URL", Value: req.GitLabURL},
		{Type: "ssh", Provider: "ssh", Name: "Git SSH Private Key", EnvKey: EnvGitSSHPrivateKey, Value: req.SSHPrivateKey},
		{Type: "ssh", Provider: "ssh", Name: "Git SSH Known Hosts", EnvKey: EnvGitSSHKnownHosts, Value: req.SSHKnownHosts},
	}
	for _, in := range creds {
		in.Value = strings.TrimSpace(in.Value)
		if in.Value == "" {
			continue
		}
		if _, err := s.Credentials.SetByEnvKey(projectID, in); err != nil {
			return err
		}
		delete(cfg.Env, in.EnvKey)
	}
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
	case AcpBackendClaudeCode:
		return "GRASP_CLAUDE_API_KEY"
	case AcpBackendCodeBuddy:
		return "GRASP_CODEBUDDY_API_KEY"
	case AcpBackendTrae:
		return "GRASP_TRAE_API_KEY"
	case AcpBackendOpenCode:
		return "GRASP_OPENCODE_API_KEY"
	case AcpBackendCodex:
		return envauth.EnvCodexAuthFile
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

func (s *OnboardingService) upsertDefaultWorkflow(projectID string, envelope models.ExportEnvelope) (models.WorkflowDef, error) {
	graph := envelope.Graph
	if err := graph.Validate(); err != nil {
		return models.WorkflowDef{}, fmt.Errorf("default workflow graph invalid: %w", err)
	}

	var existing *models.WorkflowDef
	for _, wf := range s.WF.List(projectID) {
		if isOnboardingWorkflowName(wf.Name) {
			full, ok := s.WF.Get(wf.ID)
			if !ok {
				continue
			}
			existing = &full
			break
		}
	}
	desc := strings.TrimSpace(envelope.Description)
	if existing != nil {
		existing.Name = envelope.Name
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
		Name:        envelope.Name,
		Description: desc,
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
