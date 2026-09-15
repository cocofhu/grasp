package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
)

var (
	// ErrTeamValidation is returned for invalid bootstrap payloads.
	ErrTeamValidation = errors.New("team bootstrap validation failed")
	// ErrTeamAgentConflict is returned when a target agent name already exists.
	ErrTeamAgentConflict = errors.New("agent name already exists")
	// ErrTeamSessionNotFound is returned when a bootstrap session id is unknown.
	ErrTeamSessionNotFound = errors.New("team bootstrap session not found")
	// ErrTeamScopeDenied is returned when a scoped MCP call violates session bounds.
	ErrTeamScopeDenied = errors.New("team bootstrap scope denied")
)

// TeamBootstrapRequest is the body for POST /api/agent-teams/bootstrap.
type TeamBootstrapRequest struct {
	ProjectName   string            `json:"projectName"`
	Prefix        string            `json:"prefix"`
	RootGroupName string            `json:"rootGroupName"`
	PipelineGroup string            `json:"pipelineGroupName"`
	PMName        string            `json:"pmName"`
	Background    string            `json:"background"`
	AcpBackend    string            `json:"acpBackend"`
	APIKey        string            `json:"apiKey,omitempty"`
	CustomConfig  string            `json:"customConfig,omitempty"`
	Region        string            `json:"region,omitempty"`
	GitURL        string            `json:"gitUrl,omitempty"`
	GitCredType   string            `json:"gitCredentialType,omitempty"`
	MCP           []MCPServer       `json:"mcp,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
}

// TeamBootstrapEvent is one progress log line for the UI.
type TeamBootstrapEvent struct {
	Kind    string `json:"kind"` // sys|ok|warn|mcp|err
	Message string `json:"message"`
	At      string `json:"at"`
}

// TeamBootstrapResource is one created resource for the progress panel.
type TeamBootstrapResource struct {
	Kind  string `json:"kind"` // project|group|agent
	Name  string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// TeamBootstrapSession tracks an in-flight or finished team bootstrap.
type TeamBootstrapSession struct {
	ID              string                  `json:"id"`
	Status          string                  `json:"status"` // starting|running|pulling|ready|failed
	Error           string                  `json:"error,omitempty"`
	ProjectID       string                  `json:"projectId,omitempty"`
	RootGroupID     string                  `json:"rootGroupId,omitempty"`
	PipelineGroupID string                  `json:"pipelineGroupId,omitempty"`
	PMAgent         string                  `json:"pmAgent,omitempty"`
	SandboxID       string                  `json:"sandboxId,omitempty"`
	// SandboxStatus mirrors gateway/local sandbox lifecycle (pulling|creating|running|error).
	SandboxStatus   string                  `json:"sandboxStatus,omitempty"`
	Prefix          string                  `json:"prefix,omitempty"`
	Background      string                  `json:"background,omitempty"`
	AllowedGroupIDs []string                `json:"allowedGroupIds,omitempty"`
	AgentNames      []string                `json:"agentNames,omitempty"`
	Events          []TeamBootstrapEvent    `json:"events"`
	Resources       []TeamBootstrapResource `json:"resources"`
	CreatedAt       time.Time               `json:"createdAt"`
	UpdatedAt       time.Time               `json:"updatedAt"`
}

// TeamSandbox is the SandboxService surface used by team bootstrap (g3.3).
type TeamSandbox interface {
	Open(ctx context.Context, profile string, repos []sandbox.RepoSpec, projectID string) (*models.Sandbox, error)
	GetView(ctx context.Context, id uint) (*SandboxView, error)
}

// TeamService orchestrates Create Agent Team bootstrap + scoped template creates.
type TeamService struct {
	Projects *ProjectService
	Skills   *AgentService
	Org      *OrgService
	Pm       *PmService
	Sbx      TeamSandbox

	mu         sync.Mutex
	sessions   map[string]*TeamBootstrapSession
	sessionReq map[string]normalizedTeamReq
}

// NewTeamService wires dependencies (Sbx may be nil in unit tests).
func NewTeamService(projects *ProjectService, skills *AgentService, org *OrgService, pm *PmService, sbx TeamSandbox) *TeamService {
	return &TeamService{
		Projects:   projects,
		Skills:     skills,
		Org:        org,
		Pm:         pm,
		Sbx:        sbx,
		sessions:   map[string]*TeamBootstrapSession{},
		sessionReq: map[string]normalizedTeamReq{},
	}
}

// ListTemplates returns engineer roles plus solo create extras (e.g. preflight).
// Team bootstrap still uses TeamEngineerTemplates only (1 PM + 9).
func (s *TeamService) ListTemplates() []TeamRoleTemplate {
	return AllCreateTemplates()
}

// GetSession returns a bootstrap session by id.
func (s *TeamService) GetSession(id string) (TeamBootstrapSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[strings.TrimSpace(id)]
	if !ok || sess == nil {
		return TeamBootstrapSession{}, ErrTeamSessionNotFound
	}
	cp := *sess
	cp.Events = append([]TeamBootstrapEvent(nil), sess.Events...)
	cp.Resources = append([]TeamBootstrapResource(nil), sess.Resources...)
	cp.AgentNames = append([]string(nil), sess.AgentNames...)
	cp.AllowedGroupIDs = append([]string(nil), sess.AllowedGroupIDs...)
	return cp, nil
}

// Bootstrap creates project + org + PM + 9 engineers, then starts a PM sandbox.
func (s *TeamService) Bootstrap(ctx context.Context, req TeamBootstrapRequest) (TeamBootstrapSession, error) {
	norm, err := s.normalizeRequest(req)
	if err != nil {
		return TeamBootstrapSession{}, err
	}
	if _, ok := s.Skills.Get(norm.PMName); ok {
		return TeamBootstrapSession{}, fmt.Errorf("%w: %s", ErrTeamAgentConflict, norm.PMName)
	}
	for _, role := range TeamEngineerTemplates {
		name := EngineerDisplayName(norm.Prefix, role.RoleLabelZH)
		if _, ok := s.Skills.Get(name); ok {
			return TeamBootstrapSession{}, fmt.Errorf("%w: %s", ErrTeamAgentConflict, name)
		}
	}

	sess := &TeamBootstrapSession{
		ID:         "team-" + uuid.NewString()[:12],
		Status:     "starting",
		Prefix:     norm.Prefix,
		Background: norm.Background,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.sessionReq[sess.ID] = norm
	s.mu.Unlock()
	s.appendEvent(sess.ID, "sys", "bootstrap session "+sess.ID)

	go s.runBootstrap(ctx, sess.ID, norm)
	return s.GetSession(sess.ID)
}

// Retry re-runs sandbox provisioning for a failed bootstrap session (idempotent skips).
func (s *TeamService) Retry(ctx context.Context, sessionID string) (TeamBootstrapSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	cur, err := s.GetSession(sessionID)
	if err != nil {
		return TeamBootstrapSession{}, err
	}
	if cur.Status != "failed" {
		return TeamBootstrapSession{}, fmt.Errorf("%w: only failed sessions can be retried", ErrTeamValidation)
	}
	s.mu.Lock()
	req, ok := s.sessionReq[sessionID]
	s.mu.Unlock()
	if !ok {
		return TeamBootstrapSession{}, fmt.Errorf("%w: bootstrap request no longer available", ErrTeamValidation)
	}
	s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
		sess.Status = "starting"
		sess.Error = ""
	})
	s.appendEvent(sessionID, "sys", "retry bootstrap "+sessionID)
	go s.runRetry(ctx, sessionID, req)
	return s.GetSession(sessionID)
}

type normalizedTeamReq struct {
	ProjectName   string
	Prefix        string
	RootGroupName string
	PipelineGroup string
	PMName        string
	Background    string
	AcpBackend    string
	APIKey        string
	CustomConfig  string
	Region        string
	GitURL        string
	GitCredType   string
	MCP           []MCPServer
	Env           map[string]string
}

func (s *TeamService) normalizeRequest(req TeamBootstrapRequest) (normalizedTeamReq, error) {
	projectName := strings.TrimSpace(req.ProjectName)
	prefix := strings.TrimSpace(req.Prefix)
	pmName := strings.TrimSpace(req.PMName)
	background := strings.TrimSpace(req.Background)
	if projectName == "" {
		return normalizedTeamReq{}, fmt.Errorf("%w: projectName is required", ErrTeamValidation)
	}
	if prefix == "" {
		return normalizedTeamReq{}, fmt.Errorf("%w: prefix is required", ErrTeamValidation)
	}
	if background == "" {
		return normalizedTeamReq{}, fmt.Errorf("%w: background is required", ErrTeamValidation)
	}
	var err error
	pmName, err = NormalizeAndValidateAgentName(pmName)
	if err != nil {
		if pmName == "" {
			pmName, err = NormalizeAndValidateAgentName(PMDisplayName(prefix))
		}
		if err != nil {
			return normalizedTeamReq{}, fmt.Errorf("%w: pmName: %v", ErrTeamValidation, err)
		}
	}
	root := strings.TrimSpace(req.RootGroupName)
	if root == "" {
		root = prefix + "项目组"
	}
	pipeline := strings.TrimSpace(req.PipelineGroup)
	if pipeline == "" {
		pipeline = "Pipeline(GitHub)"
	}
	mcp := req.MCP
	if len(mcp) == 0 {
		mcp = DefaultPlatformMCP()
	}
	env := map[string]string{}
	for k, v := range req.Env {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		env[k] = v
	}
	if _, ok := env["GIT_REPOS"]; !ok {
		env["GIT_REPOS"] = "${vars.repos}"
	}
	return normalizedTeamReq{
		ProjectName:   projectName,
		Prefix:        prefix,
		RootGroupName: root,
		PipelineGroup: pipeline,
		PMName:        pmName,
		Background:    background,
		AcpBackend:    NormalizeAcpBackend(req.AcpBackend),
		APIKey:        strings.TrimSpace(req.APIKey),
		CustomConfig:  strings.TrimSpace(req.CustomConfig),
		Region:        strings.TrimSpace(req.Region),
		GitURL:        strings.TrimSpace(req.GitURL),
		GitCredType:   strings.TrimSpace(req.GitCredType),
		MCP:           mcp,
		Env:           env,
	}, nil
}

func (s *TeamService) runBootstrap(ctx context.Context, sessionID string, req normalizedTeamReq) {
	fail := func(err error) {
		s.setStatus(sessionID, "failed", err.Error())
		s.appendEvent(sessionID, "err", err.Error())
	}

	s.appendEvent(sessionID, "sys", "creating project "+req.ProjectName)
	var envEntries []models.EnvEntry
	if req.APIKey != "" && req.CustomConfig == "" {
		envEntries = append(envEntries, models.EnvEntry{
			Key: primaryAuthEnvKey(req.AcpBackend), Value: req.APIKey, Secret: true,
		})
		switch NormalizeAcpBackend(req.AcpBackend) {
		case AcpBackendCodeBuddy:
			region := req.Region
			if region == "" {
				region = "public"
			}
			envEntries = append(envEntries, models.EnvEntry{Key: runtime.EnvCodeBuddyRegion, Value: region})
		case AcpBackendTrae:
			region := req.Region
			if region == "" {
				region = "intl"
			}
			envEntries = append(envEntries, models.EnvEntry{Key: runtime.EnvTraeRegion, Value: region})
		case AcpBackendOpenCode:
			provider := runtime.NormalizeOpenCodeProvider(req.Env[runtime.EnvOpenCodeProvider])
			envEntries = append(envEntries, models.EnvEntry{Key: runtime.EnvOpenCodeProvider, Value: provider})
			if v := strings.TrimSpace(req.Env[runtime.EnvOpenCodeBaseURL]); v != "" {
				envEntries = append(envEntries, models.EnvEntry{Key: runtime.EnvOpenCodeBaseURL, Value: v})
			}
			if v := strings.TrimSpace(req.Env[runtime.EnvACPBridgeModel]); v != "" {
				envEntries = append(envEntries, models.EnvEntry{Key: runtime.EnvACPBridgeModel, Value: v})
			}
		}
	}
	proj, err := s.Projects.Create(req.ProjectName, req.Background, envEntries, nil)
	if err != nil {
		fail(err)
		return
	}
	s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
		sess.ProjectID = proj.ID
		sess.Status = "running"
	})
	s.addResource(sessionID, "project", proj.Name, proj.ID)
	s.appendEvent(sessionID, "ok", "project created: "+proj.ID)

	rootID := NewGroupID()
	pipeID := NewGroupID()
	s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
		sess.RootGroupID = rootID
		sess.PipelineGroupID = pipeID
		sess.AllowedGroupIDs = []string{rootID, pipeID}
	})

	// Create PM agent
	s.appendEvent(sessionID, "sys", "creating PM "+req.PMName)
	pmAgent, err := s.buildPMAgent(req, proj.ID)
	if err != nil {
		fail(err)
		return
	}
	if err := s.Skills.Save(pmAgent); err != nil {
		fail(err)
		return
	}
	s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
		sess.PMAgent = req.PMName
		sess.AgentNames = append(sess.AgentNames, req.PMName)
	})
	s.addResource(sessionID, "agent", req.PMName, "PM Leader")
	s.appendEvent(sessionID, "ok", "PM agent saved")

	enabled := true
	mcps := append([]string{}, DefaultPmEnabledMcps...)
	if _, err := s.Pm.UpdateBinding(proj.ID, &enabled, &req.PMName, mcps, nil, nil); err != nil {
		fail(fmt.Errorf("bind PM Leader: %w", err))
		return
	}
	s.appendEvent(sessionID, "ok", "PM Leader bound")

	// Org: root + pipeline + PM membership
	org, err := s.Org.Get()
	if err != nil {
		fail(err)
		return
	}
	org.Groups = append(org.Groups,
		OrgGroup{ID: rootID, Name: req.RootGroupName},
		OrgGroup{ID: pipeID, Name: req.PipelineGroup, ParentGroupID: rootID},
	)
	if org.Agents == nil {
		org.Agents = map[string]OrgAgentMembership{}
	}
	org.Agents[req.PMName] = OrgAgentMembership{GroupIDs: []string{rootID}}
	if _, err := s.Org.Put(org, org.Revision); err != nil {
		fail(fmt.Errorf("org put: %w", err))
		return
	}
	s.addResource(sessionID, "group", req.RootGroupName, "root")
	s.addResource(sessionID, "group", req.PipelineGroup, "pipeline · 9 engineers")
	s.appendEvent(sessionID, "mcp", "pm_ensure_child_group\nparent="+req.RootGroupName+"\nchild="+req.PipelineGroup+"\n✓ ok")

	s.appendEvent(sessionID, "warn", "inject Prompt（项目背景）:\n"+truncateRunes(req.Background, 400))
	s.appendEvent(sessionID, "warn", "provision 9 engineers from templates (inherit mcp/env)")

	for _, role := range TeamEngineerTemplates {
		name := EngineerDisplayName(req.Prefix, role.RoleLabelZH)
		s.appendEvent(sessionID, "mcp", "pm_create_agent_from_template\ntemplate="+role.ID+"\nname="+name+"\nproject="+proj.ID)
		created, err := s.CreateAgentFromTemplate(CreateFromTemplateArgs{
			SessionID:    sessionID,
			TemplateID:   role.ID,
			Name:         name,
			ProjectID:    proj.ID,
			AcpBackend:   req.AcpBackend,
			GitCredType:  req.GitCredType,
			MCP:          req.MCP,
			Env:          req.Env,
			Region:       req.Region,
			CustomConfig: req.CustomConfig,
		})
		if err != nil {
			fail(err)
			return
		}
		s.appendEvent(sessionID, "mcp", "pm_set_org_membership\nagent="+created.Name+"\ngroup="+req.PipelineGroup+"\n✓ ok")
		if err := s.SetOrgMembership(SetOrgMembershipArgs{
			SessionID: sessionID,
			AgentName: created.Name,
			GroupIDs:  []string{pipeID},
		}); err != nil {
			fail(err)
			return
		}
		s.addResource(sessionID, "agent", created.Name, "template "+role.ID+" · "+req.PipelineGroup)
		s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
			sess.AgentNames = append(sess.AgentNames, created.Name)
		})
	}

	s.finishBootstrap(ctx, sessionID, req)
}

func (s *TeamService) runRetry(ctx context.Context, sessionID string, req normalizedTeamReq) {
	fail := func(err error) {
		s.setStatus(sessionID, "failed", err.Error())
		s.appendEvent(sessionID, "err", err.Error())
	}
	cur, err := s.GetSession(sessionID)
	if err != nil {
		fail(err)
		return
	}
	// No project yet → full bootstrap.
	if strings.TrimSpace(cur.ProjectID) == "" {
		s.runBootstrap(ctx, sessionID, req)
		return
	}
	s.setStatus(sessionID, "running", "")
	s.appendEvent(sessionID, "sys", "retry: continue engineer provision (skip existing)")

	projID := cur.ProjectID
	pipeID := cur.PipelineGroupID
	if pipeID == "" {
		pipeID = NewGroupID()
		s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
			sess.PipelineGroupID = pipeID
			sess.AllowedGroupIDs = uniqueNonEmptyStrings(append(sess.AllowedGroupIDs, pipeID))
		})
	}
	pmName := cur.PMAgent
	if pmName == "" {
		pmName = req.PMName
	}

	for _, role := range TeamEngineerTemplates {
		name := EngineerDisplayName(req.Prefix, role.RoleLabelZH)
		s.appendEvent(sessionID, "mcp", "pm_create_agent_from_template\ntemplate="+role.ID+"\nname="+name+"\nproject="+projID)
		created, err := s.CreateAgentFromTemplate(CreateFromTemplateArgs{
			SessionID:    sessionID,
			TemplateID:   role.ID,
			Name:         name,
			ProjectID:    projID,
			AcpBackend:   req.AcpBackend,
			GitCredType:  req.GitCredType,
			MCP:          req.MCP,
			Env:          req.Env,
			Region:       req.Region,
			CustomConfig: req.CustomConfig,
			SkipIfExists: true,
		})
		if err != nil {
			fail(err)
			return
		}
		if err := s.SetOrgMembership(SetOrgMembershipArgs{
			SessionID: sessionID,
			AgentName: created.Name,
			GroupIDs:  []string{pipeID},
		}); err != nil {
			fail(err)
			return
		}
		cur2, _ := s.GetSession(sessionID)
		hasAgent := false
		for _, n := range cur2.AgentNames {
			if n == created.Name {
				hasAgent = true
				break
			}
		}
		if !hasAgent {
			s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
				sess.AgentNames = append(sess.AgentNames, created.Name)
			})
		}
		hasRes := false
		for _, r := range cur2.Resources {
			if r.Kind == "agent" && r.Name == created.Name {
				hasRes = true
				break
			}
		}
		if !hasRes {
			s.addResource(sessionID, "agent", created.Name, "template "+role.ID+" · retry")
		} else {
			s.appendEvent(sessionID, "warn", "skip existing agent "+created.Name)
		}
		s.appendEvent(sessionID, "mcp", "pm_set_org_membership\nagent="+created.Name+"\ngroup="+req.PipelineGroup+"\nparent="+pmName+"\n✓ ok")
	}

	s.finishBootstrap(ctx, sessionID, req)
}

func (s *TeamService) finishBootstrap(ctx context.Context, sessionID string, req normalizedTeamReq) {
	cur, err := s.GetSession(sessionID)
	if err != nil {
		s.setStatus(sessionID, "failed", err.Error())
		return
	}
	pmName := cur.PMAgent
	if pmName == "" {
		pmName = req.PMName
	}
	if s.Sbx != nil && pmName != "" {
		s.appendEvent(sessionID, "sys", "starting sandbox for "+pmName)
		var repos []sandbox.RepoSpec
		if req.GitURL != "" {
			repos = sandbox.ReposFromURL(req.GitURL)
		}
		sb, err := s.Sbx.Open(ctx, pmName, repos, cur.ProjectID)
		if err != nil {
			// Quota / missing agent: roster is already created; surface a warn and still finish
			// ready so the user can open Studio. Pull/create failures after Open are failed below.
			s.appendEvent(sessionID, "warn", "sandbox start deferred: "+err.Error())
		} else if sb != nil {
			sid := fmt.Sprintf("%d", sb.ID)
			s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
				sess.SandboxID = sid
				sess.SandboxStatus = strings.TrimSpace(sb.Status)
			})
			s.appendEvent(sessionID, "ok", "sandbox "+sid+" status="+sb.Status)
			// Do not mark ready while image is still pulling/creating (g3.3 / review v1).
			if err := s.waitTeamSandboxReady(ctx, sessionID, sb.ID); err != nil {
				s.appendEvent(sessionID, "err", "sandbox ready failed: "+err.Error())
				s.setStatus(sessionID, "failed", err.Error())
				return
			}
		}
	}
	s.appendEvent(sessionID, "ok", fmt.Sprintf("done: 1 PM + %d engineers (total %d)", len(TeamEngineerTemplates), 1+len(TeamEngineerTemplates)))
	s.setStatus(sessionID, "ready", "")
}

// waitTeamSandboxReady polls GetView until running, or fails on error/timeout.
// While status=pulling the session stays on status=pulling so TeamBootstrapPanel can show pull loading.
func (s *TeamService) waitTeamSandboxReady(ctx context.Context, sessionID string, sandboxID uint) error {
	if s.Sbx == nil {
		return nil
	}
	deadline := time.Now().Add(20 * time.Minute)
	announcedPulling := false
	poll := 500 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sandbox %d timeout waiting for running (incl. image pull)", sandboxID)
		}
		view, err := s.Sbx.GetView(ctx, sandboxID)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
			continue
		}
		st := strings.TrimSpace(view.Status)
		s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
			sess.SandboxStatus = st
		})
		switch st {
		case "running":
			if announcedPulling {
				s.appendEvent(sessionID, "ok", "runtime image ready")
			}
			s.setStatus(sessionID, "running", "")
			return nil
		case "error", "stopped":
			msg := strings.TrimSpace(view.Error)
			if msg == "" {
				msg = "sandbox " + st
			}
			return fmt.Errorf("%s", msg)
		case "pulling":
			if !announcedPulling {
				s.appendEvent(sessionID, "sys", "pulling runtime image…")
				announcedPulling = true
			}
			s.setStatus(sessionID, "pulling", "")
			poll = 500 * time.Millisecond
		default:
			// creating / unknown: keep session running (not ready) while we wait.
			if announcedPulling {
				s.setStatus(sessionID, "running", "")
			}
			poll = 500 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// CreateFromTemplateArgs is used by MCP and bootstrap.
type CreateFromTemplateArgs struct {
	SessionID    string
	TemplateID   string
	Name         string
	ProjectID    string
	AcpBackend   string
	GitCredType  string
	MCP          []MCPServer
	Env          map[string]string
	Region       string
	CustomConfig string
	SkipIfExists bool
	// When SessionID is set, scope checks apply.
}

// CreateAgentFromTemplate creates one engineer from an embedded template.
func (s *TeamService) CreateAgentFromTemplate(args CreateFromTemplateArgs) (Agent, error) {
	role, ok := TeamRoleByID(args.TemplateID)
	if !ok {
		return Agent{}, fmt.Errorf("%w: unknown template %s", ErrTeamValidation, args.TemplateID)
	}
	name, err := NormalizeAndValidateAgentName(args.Name)
	if err != nil {
		return Agent{}, fmt.Errorf("%w: %v", ErrTeamValidation, err)
	}
	if existing, exists := s.Skills.Get(name); exists {
		if args.SkipIfExists {
			return existing, nil
		}
		return Agent{}, fmt.Errorf("%w: %s", ErrTeamAgentConflict, name)
	}
	projectID := strings.TrimSpace(args.ProjectID)
	if projectID == "" {
		return Agent{}, fmt.Errorf("%w: projectId is required", ErrTeamValidation)
	}
	if args.SessionID != "" {
		if err := s.assertSessionProject(args.SessionID, projectID); err != nil {
			return Agent{}, err
		}
	}

	tmpl, err := loadTeamAgentTemplate(role.EmbedName)
	if err != nil {
		return Agent{}, err
	}
	tmpl.Name = name
	tmpl.ProjectID = projectID
	backend := NormalizeAcpBackend(args.AcpBackend)
	if backend == "" {
		backend = tmpl.AcpBackend
	}
	tmpl.AcpBackend = backend
	tmpl.Layout.ConfigRoot = DefaultConfigRootForBackend(backend)
	if strings.TrimSpace(tmpl.Layout.WorkspaceDir) == "" {
		tmpl.Layout.WorkspaceDir = DefaultWorkspaceDir
	}
	if args.GitCredType != "" {
		tmpl.GitCredentialType = args.GitCredType
	}

	// Merge env: template < request
	mergedEnv := map[string]string{}
	for k, v := range tmpl.Env {
		mergedEnv[k] = v
	}
	for k, v := range args.Env {
		k = strings.TrimSpace(k)
		if k != "" {
			mergedEnv[k] = v
		}
	}
	applyOnboardingAgentRegion(mergedEnv, backend, args.Region)
	tmpl.Env = stripTokenKeysFromEnvMap(mergedEnv)

	// MCP: prefer request list when provided, else template
	if len(args.MCP) > 0 {
		tmpl.MCP = args.MCP
	} else if len(tmpl.MCP) == 0 {
		tmpl.MCP = DefaultPlatformMCP()
	}
	if strings.TrimSpace(args.CustomConfig) != "" {
		tmpl.Files = upsertAgentFile(tmpl.Files, agentAuthConfigFileName(backend), strings.TrimSpace(args.CustomConfig))
	}

	if err := s.Skills.Save(tmpl); err != nil {
		return Agent{}, err
	}
	return tmpl, nil
}

// SetOrgMembershipArgs updates groupIds under scope.
type SetOrgMembershipArgs struct {
	SessionID string
	AgentName string
	GroupIDs  []string
}

// SetOrgMembership sets membership for an agent (scoped when SessionID set).
func (s *TeamService) SetOrgMembership(args SetOrgMembershipArgs) error {
	agentName := strings.TrimSpace(args.AgentName)
	if agentName == "" {
		return fmt.Errorf("%w: agentName required", ErrTeamValidation)
	}
	ag, ok := s.Skills.Get(agentName)
	if !ok {
		return fmt.Errorf("%w: agent not found: %s", ErrTeamValidation, agentName)
	}
	groupIDs := uniqueNonEmptyStrings(args.GroupIDs)

	if args.SessionID != "" {
		sess, err := s.GetSession(args.SessionID)
		if err != nil {
			return err
		}
		if !AgentProjectMatches(ag, sess.ProjectID) {
			return fmt.Errorf("%w: agent not in session project", ErrTeamScopeDenied)
		}
		allowed := map[string]bool{}
		for _, id := range sess.AllowedGroupIDs {
			allowed[id] = true
		}
		for _, id := range groupIDs {
			if !allowed[id] {
				return fmt.Errorf("%w: group not in session allow-list: %s", ErrTeamScopeDenied, id)
			}
		}
	}

	org, err := s.Org.Get()
	if err != nil {
		return err
	}
	if org.Agents == nil {
		org.Agents = map[string]OrgAgentMembership{}
	}
	org.Agents[agentName] = OrgAgentMembership{GroupIDs: groupIDs}
	_, err = s.Org.Put(org, org.Revision)
	return err
}


func (s *TeamService) buildPMAgent(req normalizedTeamReq, projectID string) (Agent, error) {
	tmpl, err := loadTeamAgentTemplate(TeamPMEmbedName)
	if err != nil {
		return Agent{}, err
	}
	tmpl.Name = req.PMName
	tmpl.ProjectID = projectID
	backend := NormalizeAcpBackend(req.AcpBackend)
	if backend == "" {
		backend = tmpl.AcpBackend
	}
	tmpl.AcpBackend = backend
	tmpl.Layout.ConfigRoot = DefaultConfigRootForBackend(backend)
	if strings.TrimSpace(tmpl.Layout.WorkspaceDir) == "" {
		tmpl.Layout.WorkspaceDir = DefaultWorkspaceDir
	}
	if req.GitCredType != "" {
		tmpl.GitCredentialType = req.GitCredType
	}

	mergedEnv := map[string]string{}
	for k, v := range tmpl.Env {
		mergedEnv[k] = v
	}
	for k, v := range req.Env {
		k = strings.TrimSpace(k)
		if k != "" {
			mergedEnv[k] = v
		}
	}
	applyOnboardingAgentRegion(mergedEnv, backend, req.Region)
	tmpl.Env = stripTokenKeysFromEnvMap(mergedEnv)

	if len(req.MCP) > 0 {
		tmpl.MCP = req.MCP
	} else if len(tmpl.MCP) == 0 {
		tmpl.MCP = DefaultPlatformMCP()
	}

	tmpl.Files = upsertAgentFile(tmpl.Files, "rules/project-context.md", teamPMProjectContextMarkdown(req))
	if strings.TrimSpace(req.CustomConfig) != "" {
		tmpl.Files = upsertAgentFile(tmpl.Files, agentAuthConfigFileName(backend), strings.TrimSpace(req.CustomConfig))
	}
	return tmpl, nil
}

func teamPMProjectContextMarkdown(req normalizedTeamReq) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: 本项目背景与建团编制（始终应用；由创建 Agent 团队写入）\n")
	b.WriteString("alwaysApply: true\n")
	b.WriteString("---\n\n")
	b.WriteString("# 项目上下文 · " + req.PMName + "\n\n")
	b.WriteString("## 项目背景（建团 Prompt）\n\n")
	b.WriteString(strings.TrimSpace(req.Background))
	b.WriteString("\n\n## 编制约定\n\n")
	b.WriteString("- 命名前缀：`" + req.Prefix + "`\n")
	b.WriteString("- 根组：`" + req.RootGroupName + "`（你在此组）\n")
	b.WriteString("- 流水线子组：`" + req.PipelineGroup + "`（9 名工程师挂此组，上级为你）\n")
	b.WriteString("- PM：`" + req.PMName + "`\n")
	b.WriteString("- 工程师命名：`{前缀}{角色}工程师`（调研/计划/方案/澄清/视觉原型/实现/测试/代码Review/变更摘要视觉）\n\n")
	if req.GitURL != "" {
		b.WriteString("## 仓库\n\n")
		b.WriteString("- Git URL：`" + req.GitURL + "`\n\n")
	}
	b.WriteString("## 工作方式\n\n")
	b.WriteString("先 `pm_get_org` 确认编制，再按流水线分派工程师；缺人时用模板补齐，勿覆盖重名。\n")
	return b.String()
}

func upsertAgentFile(files []AgentFile, path, content string) []AgentFile {
	path = strings.TrimSpace(path)
	for i := range files {
		if files[i].Path == path {
			files[i].Content = content
			return files
		}
	}
	return append(files, AgentFile{Path: path, Content: content})
}

func (s *TeamService) assertSessionProject(sessionID, projectID string) error {
	sess, err := s.GetSession(sessionID)
	if err != nil {
		return err
	}
	if sess.ProjectID == "" || sess.ProjectID != strings.TrimSpace(projectID) {
		return fmt.Errorf("%w: projectId mismatch", ErrTeamScopeDenied)
	}
	return nil
}

func (s *TeamService) appendEvent(id, kind, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return
	}
	sess.Events = append(sess.Events, TeamBootstrapEvent{
		Kind: kind, Message: message, At: time.Now().UTC().Format(time.RFC3339),
	})
	sess.UpdatedAt = time.Now().UTC()
}

func (s *TeamService) addResource(id, kind, name, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return
	}
	sess.Resources = append(sess.Resources, TeamBootstrapResource{Kind: kind, Name: name, Detail: detail})
	sess.UpdatedAt = time.Now().UTC()
}

func (s *TeamService) setStatus(id, status, errMsg string) {
	s.patchSession(id, func(sess *TeamBootstrapSession) {
		sess.Status = status
		sess.Error = errMsg
	})
}

func (s *TeamService) patchSession(id string, fn func(*TeamBootstrapSession)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return
	}
	fn(sess)
	sess.UpdatedAt = time.Now().UTC()
}

func uniqueNonEmptyStrings(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
