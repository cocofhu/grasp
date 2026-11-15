package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
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
	// OnboardingSoloAgentName is the role-free Agent created for Solo sessions.
	OnboardingSoloAgentName = "Solo"
)

var (
	// ErrOnboardingModelRequired is returned when no saved model configuration is selected.
	ErrOnboardingModelRequired = errors.New("modelConfigurationId is required")
	// ErrBaselineReposRequired is returned when no non-empty repository URL is submitted.
	ErrBaselineReposRequired = errors.New("at least one repository is required")
	// ErrOnboardingInvalidTeam is returned when the chosen templates are unknown,
	// duplicated, miss a required template, or use a name other than the
	// language's canonical Agent name.
	ErrOnboardingInvalidTeam = errors.New("invalid onboarding team")
	// ErrOnboardingAgentsIncomplete is returned when neither canonical quartet
	// exists, so a baseline workflow must not be created.
	ErrOnboardingAgentsIncomplete = errors.New("onboarding agents incomplete")
	// ErrProtectedOnboardingAgent is returned when a canonical onboarding Agent
	// would be deleted or renamed. The Agent is required by workflows.
	ErrProtectedOnboardingAgent = errors.New("该 Agent 被工作流使用，不能删除或重命名")
)

// OnboardingBootstrapRequest is the body for POST .../bootstrap-onboarding.
type OnboardingBootstrapRequest struct {
	ModelConfigurationID  string   `json:"modelConfigurationId"`
	ModelConfigurationIDs []string `json:"modelConfigurationIds,omitempty"`
	// Language is the wizard's UI language (zh-CN | en); it names the default
	// workflow and its start/end nodes. Empty means Chinese.
	Language string `json:"language,omitempty"`
	Region   string `json:"region,omitempty"`
	// RepoURL, when set, becomes a repository bound to the default workflow,
	// with its own credential and commit author.
	RepoURL          string `json:"repoUrl,omitempty"`
	RepoBranch       string `json:"repoBranch,omitempty"`
	RepoCredentialID string `json:"repoCredentialId,omitempty"`
	CommitName       string `json:"commitName,omitempty"`
	CommitEmail      string `json:"commitEmail,omitempty"`
	// VncPreview / BrowserMcp default on when omitted (first-install preview stack).
	VncPreview *bool `json:"vncPreview"`
	BrowserMcp *bool `json:"browserMcp"`
	// Agents picks the built-in templates to create. Empty = all four templates
	// with the canonical names for Language. Every template is required, and a
	// non-empty name must equal that language's canonical name.
	Agents []OnboardingAgentChoice `json:"agents,omitempty"`
}

// OnboardingAgentChoice chooses a role and optionally a different saved model.
type OnboardingAgentChoice struct {
	TemplateID string `json:"templateId"`
	// Name must be empty or the canonical name for the wizard language.
	Name                  string   `json:"name,omitempty"`
	ModelConfigurationID  string   `json:"modelConfigurationId,omitempty"`
	ModelConfigurationIDs []string `json:"modelConfigurationIds,omitempty"`
}

// OnboardingRequiredTemplateIDs must all be part of the onboarding team.
var OnboardingRequiredTemplateIDs = []string{"clarify", "implement", "test_review", "deliver"}

// OnboardingBootstrapResult is returned after a successful (idempotent) bootstrap.
type OnboardingBootstrapResult struct {
	AgentIDs    []string `json:"agentIds"`
	SoloAgentID string   `json:"soloAgentId"`
	WorkflowID  string   `json:"workflowId"`
	Published   bool     `json:"published"`
}

// CreateBaselineWorkflowRequest is the body for POST /api/workflows/from-baseline.
// Repos are the repositories bound to the new workflow.
type CreateBaselineWorkflowRequest struct {
	Name  string              `json:"name"`
	Repos []models.RepoChoice `json:"repos"`
}

// OnboardingService configures the built-in team, a role-free Solo Agent and 默认工作流.
type OnboardingService struct {
	Skills      *AgentService
	SharedAgent *SharedAgentService
	WF          *WorkflowService
	Credentials *CredentialService
	Projects    *ProjectService
}

// NewOnboardingService wires dependencies. Credentials are resolved by reference.
func NewOnboardingService(skills *AgentService, shared *SharedAgentService, wf *WorkflowService, credentials *CredentialService) *OnboardingService {
	return &OnboardingService{Skills: skills, SharedAgent: shared, WF: wf, Credentials: credentials}
}

// CreateFromBaseline clones the embedded first-install workflow into a new
// published workflow. It does not modify the existing onboarding workflow.
func (s *OnboardingService) CreateFromBaseline(req CreateBaselineWorkflowRequest) (models.WorkflowDef, error) {
	name := strings.TrimSpace(req.Name)
	if err := s.WF.validateWorkflowName(name, ""); err != nil {
		return models.WorkflowDef{}, err
	}
	repos, err := s.boundRepos(req.Repos)
	if err != nil {
		return models.WorkflowDef{}, err
	}
	if len(repos) == 0 {
		return models.WorkflowDef{}, ErrBaselineReposRequired
	}
	envelope, err := loadDefaultWorkflowEnvelope()
	if err != nil {
		return models.WorkflowDef{}, err
	}
	set := BaselineOnboardingSet(s.existingAgentNames())
	if set == "" {
		return models.WorkflowDef{}, ErrOnboardingAgentsIncomplete
	}
	if set == "en" {
		rebindBaselineProfilesToEnglish(&envelope.Graph)
	}
	if err := envelope.Graph.Validate(); err != nil {
		return models.WorkflowDef{}, fmt.Errorf("default workflow graph invalid: %w", err)
	}
	if err := s.requireBaselineAgents(set); err != nil {
		return models.WorkflowDef{}, err
	}

	wf := models.WorkflowDef{
		ID:          "wf-" + uuid.NewString()[:8],
		Name:        name,
		Description: "从默认基线创建。启动运行时可以挑选仓库和分支。",
		NeedsRepo:   true,
		Graph:       envelope.Graph,
		Defaults:    withBoundRepos(envelope.Defaults, repos),
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

func (s *OnboardingService) existingAgentNames() []string {
	if s.Skills == nil {
		return nil
	}
	list := s.Skills.List()
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.Name)
	}
	return names
}

// rebindBaselineProfilesToEnglish points the embedded Chinese agent_profile
// refs at the English canonical names. Existing Agent files are not written.
func rebindBaselineProfilesToEnglish(g *models.Graph) {
	if g == nil {
		return
	}
	mapping := make(map[string]string, len(OnboardingAgentNames))
	for i, zh := range OnboardingAgentNames {
		mapping[zh] = OnboardingAgentNamesEN[i]
	}
	for i := range g.Nodes {
		cfg := g.Nodes[i].Config
		if cfg == nil {
			continue
		}
		cur := models.AgentProfile(cfg)
		next, ok := mapping[cur]
		if !ok {
			continue
		}
		models.SetAgentProfile(cfg, next)
		if g.Nodes[i].Label == cur {
			g.Nodes[i].Label = next
		}
	}
}

// requireBaselineAgents rejects a workflow whose selected canonical Agents are not configured.
func (s *OnboardingService) requireBaselineAgents(set string) error {
	names := OnboardingAgentNames
	if set == "en" {
		names = OnboardingAgentNamesEN
	}
	for _, name := range names {
		agent, ok := s.Skills.Get(name)
		if ok && s.SharedAgent != nil {
			agent = ExtendOverlay(s.SharedAgent.Get(), agent)
		}
		if !ok || agent.ModelBinding == nil {
			return fmt.Errorf("Agent %s is not configured; complete onboarding first", name)
		}
		if err := modelrouting.ValidateConfigurationReference(agent.ModelBinding); err != nil {
			return fmt.Errorf("Agent %s: %w", agent.Name, err)
		}
		configuration, err := s.Credentials.requireModelConfiguration(agent.ModelBinding.ConfigurationID)
		if err != nil {
			return fmt.Errorf("Agent %s: %w", agent.Name, err)
		}
		if configuration.Backend != agent.AcpBackend {
			return fmt.Errorf("Agent %s: model configuration belongs to another coding tool", agent.Name)
		}
	}
	return nil
}

// requireModelConfiguration validates an explicit, active configuration and its dependencies.
func (s *CredentialService) requireModelConfiguration(id string) (modelrouting.Configuration, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return modelrouting.Configuration{}, ErrOnboardingModelRequired
	}
	if s == nil {
		return modelrouting.Configuration{}, errors.New("credential service unavailable")
	}
	configuration, err := NewModelConfigurationService(s.db).Get(id)
	if err != nil {
		return modelrouting.Configuration{}, err
	}
	_, err = NewModelConnectionService(s.db).Resolve(configuration.Backend, modelrouting.Binding{Kind: "configuration", ConfigurationID: id})
	return configuration, err
}

func (s *CredentialService) requireModelConfigurationSet(defaultID string, ids []string) (modelrouting.Configuration, error) {
	ref := &modelrouting.Binding{Kind: "configuration", ConfigurationID: defaultID, ConfigurationIDs: ids}
	if len(ids) > 0 {
		if err := modelrouting.ValidateConfigurationReference(ref); err != nil {
			return modelrouting.Configuration{}, err
		}
	}
	configuration, err := s.requireModelConfiguration(defaultID)
	if err != nil {
		return modelrouting.Configuration{}, err
	}
	for _, id := range modelrouting.ConfigurationIDs(ref) {
		if id != defaultID {
			if _, err := s.requireModelConfiguration(id); err != nil {
				return modelrouting.Configuration{}, err
			}
		}
	}
	return configuration, nil
}

// Bootstrap binds the selected saved models and Git authorizations to the chosen
// role templates, then publishes the default workflow. It never starts a run.
func (s *OnboardingService) Bootstrap(req OnboardingBootstrapRequest) (OnboardingBootstrapResult, error) {
	configuration, err := s.Credentials.requireModelConfigurationSet(req.ModelConfigurationID, req.ModelConfigurationIDs)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	loc := onboardingLocaleFor(req.Language)
	region := strings.TrimSpace(req.Region)
	team, err := resolveOnboardingTeam(req.Language, req.Agents)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	nameMap := make(map[string]string, len(team))
	for _, m := range team {
		nameMap[m.Role.RoleLabelZH] = m.Name
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
		selected := configuration
		configurationIDs := req.ModelConfigurationIDs
		if m.ModelConfigurationID != "" {
			configurationIDs = m.ModelConfigurationIDs
			selected, err = s.Credentials.requireModelConfigurationSet(m.ModelConfigurationID, configurationIDs)
			if err != nil {
				return OnboardingBootstrapResult{}, err
			}
		}
		backend := selected.Backend
		tmpl.AcpBackend = backend
		tmpl.InheritModel = false
		tmpl.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: selected.ID, ConfigurationIDs: append([]string(nil), configurationIDs...)}
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
		applyOnboardingAgentRegion(tmpl.Env, backend, region)
		templates = append(templates, tmpl)
	}

	repo, err := s.onboardingRepo(req)
	if err != nil {
		return OnboardingBootstrapResult{}, err
	}
	if err := s.writeWorkspaceSettings(req); err != nil {
		return OnboardingBootstrapResult{}, err
	}

	agentIDs := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		if _, ok := s.Skills.Get(tmpl.Name); ok {
			// Re-entry keeps the saved Agent. Only missing canonical names are created.
			agentIDs = append(agentIDs, tmpl.Name)
			continue
		}
		if err := s.Skills.Save(tmpl); err != nil {
			return OnboardingBootstrapResult{}, fmt.Errorf("save agent %s: %w", tmpl.Name, err)
		}
		agentIDs = append(agentIDs, tmpl.Name)
	}

	if _, ok := s.Skills.Get(OnboardingSoloAgentName); !ok {
		solo := Agent{
			Name:         OnboardingSoloAgentName,
			AcpBackend:   configuration.Backend,
			ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID, ConfigurationIDs: append([]string(nil), req.ModelConfigurationIDs...)},
			Capabilities: &models.AgentCapabilities{Interaction: models.InteractionAuto, Tools: []string{
				models.ToolAskQuestion, models.ToolAskForm, models.ToolSetArtifactPreview, models.ToolSetPreview,
			}, Reads: []string{"*"}},
			Files: []AgentFile{{Path: "AGENTS.md", Content: loc.SoloAgentsMD}},
			Env:   map[string]string{},
			Layout: AgentLayout{
				ConfigRoot:   DefaultConfigRootForBackend(configuration.Backend),
				WorkspaceDir: DefaultWorkspaceDir,
			},
		}
		applyOnboardingAgentRegion(solo.Env, configuration.Backend, region)
		if err := s.Skills.Save(solo); err != nil {
			return OnboardingBootstrapResult{}, fmt.Errorf("save Solo agent: %w", err)
		}
	}

	wf, err := s.upsertDefaultWorkflow(envelope, repo)
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
		AgentIDs:    agentIDs,
		SoloAgentID: OnboardingSoloAgentName,
		WorkflowID:  published.ID,
		Published:   published.Status() == models.WorkflowStatusPublished,
	}, nil
}

type onboardingTeamMember struct {
	Role                  TeamRoleTemplate
	Name                  string
	ModelConfigurationID  string
	ModelConfigurationIDs []string
}

// resolveOnboardingTeam turns the wizard's choices into the Agents to save, in
// workflow order. Empty choices keep every template under the language's
// canonical names. A submitted team must include all four, and each name must
// match that canonical name.
func resolveOnboardingTeam(lang string, choices []OnboardingAgentChoice) ([]onboardingTeamMember, error) {
	canonical := canonicalOnboardingNames(lang)
	if len(choices) == 0 {
		out := make([]onboardingTeamMember, 0, len(TeamEngineerTemplates))
		for i, role := range TeamEngineerTemplates {
			out = append(out, onboardingTeamMember{Role: role, Name: canonical[i]})
		}
		return out, nil
	}
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
	for _, id := range OnboardingRequiredTemplateIDs {
		if _, ok := picked[id]; !ok {
			return nil, fmt.Errorf("%w: template %q is required", ErrOnboardingInvalidTeam, id)
		}
	}
	out := make([]onboardingTeamMember, 0, len(TeamEngineerTemplates))
	seen := map[string]bool{}
	for i, role := range TeamEngineerTemplates {
		c := picked[role.ID]
		name := canonical[i]
		if custom := strings.TrimSpace(c.Name); custom != "" {
			normalized, err := NormalizeAndValidateAgentName(custom)
			if err != nil {
				return nil, err
			}
			if normalized != name {
				return nil, fmt.Errorf("%w: agent name %q must be %q", ErrOnboardingInvalidTeam, normalized, name)
			}
			name = normalized
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: duplicate agent name %q", ErrOnboardingInvalidTeam, name)
		}
		seen[name] = true
		out = append(out, onboardingTeamMember{Role: role, Name: name, ModelConfigurationID: strings.TrimSpace(c.ModelConfigurationID), ModelConfigurationIDs: append([]string(nil), c.ModelConfigurationIDs...)})
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

func (s *OnboardingService) writeWorkspaceSettings(req OnboardingBootstrapRequest) error {
	if s.SharedAgent == nil {
		return errors.New("shared agent service unavailable")
	}
	cfg := s.SharedAgent.Get()
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
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

// upsertDefaultWorkflow saves the default workflow. A non-empty repo is bound
// to it, also on an existing workflow that does not bind it yet.
func (s *OnboardingService) upsertDefaultWorkflow(envelope models.ExportEnvelope, repo string) (models.WorkflowDef, error) {
	graph := envelope.Graph
	if err := graph.Validate(); err != nil {
		return models.WorkflowDef{}, fmt.Errorf("default workflow graph invalid: %w", err)
	}

	var existing *models.WorkflowDef
	for _, wf := range s.WF.List() {
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
	var repos []models.RepoChoice
	if repo != "" {
		repos = []models.RepoChoice{{ProjectID: repo}}
	}
	// Template defaults only seed a new workflow; an existing one keeps the
	// user's defaults.
	if existing != nil {
		existing.Name = envelope.Name
		existing.Description = desc
		existing.NeedsRepo = true
		existing.Graph = graph
		existing.Defaults = withBoundRepos(existing.Defaults, repos)
		if err := s.WF.Save(existing); err != nil {
			return models.WorkflowDef{}, err
		}
		return *existing, nil
	}
	wf := models.WorkflowDef{
		ID:          uuid.NewString(),
		Name:        envelope.Name,
		Description: desc,
		NeedsRepo:   true,
		Graph:       graph,
		Defaults:    withBoundRepos(envelope.Defaults, repos),
	}
	if err := s.WF.Save(&wf); err != nil {
		return models.WorkflowDef{}, err
	}
	return wf, nil
}

// withBoundRepos returns a copy of defaults that also binds repos, keeping
// the repositories already bound and their order.
func withBoundRepos(defaults *models.WorkflowDefaults, repos []models.RepoChoice) *models.WorkflowDefaults {
	if len(repos) == 0 {
		return defaults
	}
	out := models.WorkflowDefaults{InheritWorkspace: true}
	if defaults != nil {
		out = *defaults
	}
	out.Repos = append([]models.RepoChoice(nil), out.Repos...)
	for _, r := range repos {
		bound := false
		for _, b := range out.Repos {
			bound = bound || b.ProjectID == r.ProjectID
		}
		if !bound {
			out.Repos = append(out.Repos, r)
		}
	}
	return &out
}

// boundRepos checks that every chosen repository exists.
func (s *OnboardingService) boundRepos(choices []models.RepoChoice) ([]models.RepoChoice, error) {
	out := make([]models.RepoChoice, 0, len(choices))
	seen := map[string]bool{}
	for _, c := range choices {
		id := strings.TrimSpace(c.ProjectID)
		if id == "" || seen[id] {
			continue
		}
		if s.Projects == nil {
			return nil, errors.New("project service unavailable")
		}
		if _, err := s.Projects.Get(id); err != nil {
			return nil, err
		}
		seen[id] = true
		out = append(out, models.RepoChoice{ProjectID: id, Branch: strings.TrimSpace(c.Branch)})
	}
	return out, nil
}

// onboardingRepo saves the wizard's repository and returns its id; empty when
// the wizard skipped it. A repository with the same URL is reused and takes
// the credential and commit author entered here.
func (s *OnboardingService) onboardingRepo(req OnboardingBootstrapRequest) (string, error) {
	url := strings.TrimSpace(req.RepoURL)
	if url == "" {
		return "", nil
	}
	if s.Projects == nil {
		return "", errors.New("project service unavailable")
	}
	in := ProjectInput{
		RepoURL: url, DefaultBranch: req.RepoBranch,
		CredentialID: req.RepoCredentialID, CommitName: req.CommitName, CommitEmail: req.CommitEmail,
	}
	list, err := s.Projects.List()
	if err != nil {
		return "", err
	}
	for _, p := range list {
		if strings.TrimSpace(p.RepoURL) != url {
			continue
		}
		in.Name, in.Description, in.RepoDirName = p.Name, p.Description, p.RepoDirName
		if strings.TrimSpace(in.DefaultBranch) == "" {
			in.DefaultBranch = p.DefaultBranch
		}
		updated, err := s.Projects.Update(p.ID, in)
		if err != nil {
			return "", err
		}
		return updated.ID, nil
	}
	base := sandbox.RepoNameFromURL(url)
	if base == "" {
		base = "repo"
	}
	in.Name = base
	for suffix := 2; s.Projects.nameTaken(in.Name, ""); suffix++ {
		in.Name = fmt.Sprintf("%s-%d", base, suffix)
	}
	created, err := s.Projects.Create(in, "")
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// applyOnboardingAgentRegion writes the Studio-managed region env key into an
// Agent env map for backends that require it (CodeBuddy / Trae). Empty region
// uses the same region defaults as the tool launcher.
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
	case AcpBackendKimiCode:
		if region == "" {
			region = "intl"
		}
		env[runtime.EnvKimiCodeRegion] = region
	}
}
