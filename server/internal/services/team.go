package services

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"

	"github.com/google/uuid"
)

var (
	// ErrTeamValidation is returned for invalid bootstrap payloads.
	ErrTeamValidation = errors.New("team bootstrap validation failed")
	// ErrTeamAgentConflict is returned when a target agent name already exists.
	ErrTeamAgentConflict = errors.New("agent name already exists")
	// ErrTeamSessionNotFound is returned when a bootstrap session id is unknown.
	ErrTeamSessionNotFound = errors.New("team bootstrap session not found")
)

// TeamBootstrapRequest is the body for POST /api/agent-teams/bootstrap.
type TeamBootstrapRequest struct {
	Prefix                string            `json:"prefix"`
	ModelConfigurationID  string            `json:"modelConfigurationId"`
	ModelConfigurationIDs []string          `json:"modelConfigurationIds,omitempty"`
	Region                string            `json:"region,omitempty"`
	MCP                   []MCPServer       `json:"mcp,omitempty"`
	Env                   map[string]string `json:"env,omitempty"`
}

// TeamBootstrapEvent is one progress log line for the UI.
type TeamBootstrapEvent struct {
	Kind    string `json:"kind"` // sys|ok|warn|mcp|err
	Message string `json:"message"`
	At      string `json:"at"`
}

// TeamBootstrapResource is one created resource for the progress panel.
type TeamBootstrapResource struct {
	Kind   string `json:"kind"` // workspace|agent
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// TeamBootstrapSession tracks an in-flight or finished team bootstrap.
type TeamBootstrapSession struct {
	ID         string                  `json:"id"`
	Status     string                  `json:"status"` // starting|running|ready|failed
	Error      string                  `json:"error,omitempty"`
	Prefix     string                  `json:"prefix,omitempty"`
	AgentNames []string                `json:"agentNames,omitempty"`
	Events     []TeamBootstrapEvent    `json:"events"`
	Resources  []TeamBootstrapResource `json:"resources"`
	CreatedAt  time.Time               `json:"createdAt"`
	UpdatedAt  time.Time               `json:"updatedAt"`
}

// TeamService orchestrates Create Agent Team bootstrap + scoped template creates.
type TeamService struct {
	Workspace *WorkspaceService
	Skills    *AgentService
	// Credentials resolves the selected model references.
	Credentials *CredentialService

	mu         sync.Mutex
	sessions   map[string]*TeamBootstrapSession
	sessionReq map[string]normalizedTeamReq
}

// NewTeamService wires dependencies.
func NewTeamService(workspace *WorkspaceService, skills *AgentService) *TeamService {
	return &TeamService{
		Workspace:  workspace,
		Skills:     skills,
		sessions:   map[string]*TeamBootstrapSession{},
		sessionReq: map[string]normalizedTeamReq{},
	}
}

// ListTemplates returns the built-in workflow Agent templates.
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
	return cp, nil
}

// Bootstrap creates engineer Agents in the existing workspace.
func (s *TeamService) Bootstrap(req TeamBootstrapRequest) (TeamBootstrapSession, error) {
	norm, err := s.normalizeRequest(req)
	if err != nil {
		return TeamBootstrapSession{}, err
	}
	for _, role := range TeamEngineerTemplates {
		name := EngineerDisplayName(norm.Prefix, role.RoleLabelZH)
		if _, ok := s.Skills.Get(name); ok {
			return TeamBootstrapSession{}, fmt.Errorf("%w: %s", ErrTeamAgentConflict, name)
		}
	}

	sess := &TeamBootstrapSession{
		ID:        "team-" + uuid.NewString()[:12],
		Status:    "starting",
		Prefix:    norm.Prefix,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.sessionReq[sess.ID] = norm
	s.mu.Unlock()
	s.appendEvent(sess.ID, "sys", "bootstrap session "+sess.ID)

	go s.runBootstrap(sess.ID, norm)
	return s.GetSession(sess.ID)
}

// Retry continues engineer provisioning for a failed bootstrap session (existing agents are skipped).
func (s *TeamService) Retry(sessionID string) (TeamBootstrapSession, error) {
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
	go s.runRetry(sessionID, req)
	return s.GetSession(sessionID)
}

type normalizedTeamReq struct {
	WorkspaceName         string
	Prefix                string
	ModelConfigurationID  string
	ModelConfigurationIDs []string
	Region                string
	MCP                   []MCPServer
	Env                   map[string]string
}

func (s *TeamService) normalizeRequest(req TeamBootstrapRequest) (normalizedTeamReq, error) {
	workspace, err := s.Workspace.Workspace()
	if err != nil {
		return normalizedTeamReq{}, err
	}
	prefix := strings.TrimSpace(req.Prefix)
	if prefix == "" {
		return normalizedTeamReq{}, fmt.Errorf("%w: prefix is required", ErrTeamValidation)
	}
	configuration, err := s.Credentials.requireModelConfigurationSet(req.ModelConfigurationID, req.ModelConfigurationIDs)
	if err != nil {
		return normalizedTeamReq{}, fmt.Errorf("%w: %v", ErrTeamValidation, err)
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
	return normalizedTeamReq{
		WorkspaceName:         workspace.Name,
		Prefix:                prefix,
		ModelConfigurationID:  configuration.ID,
		ModelConfigurationIDs: append([]string(nil), req.ModelConfigurationIDs...),
		Region:                strings.TrimSpace(req.Region),
		MCP:                   mcp,
		Env:                   env,
	}, nil
}

func (s *TeamService) runBootstrap(sessionID string, req normalizedTeamReq) {
	fail := func(err error) {
		s.setStatus(sessionID, "failed", err.Error())
		s.appendEvent(sessionID, "err", err.Error())
	}

	s.appendEvent(sessionID, "sys", "preparing workspace "+req.WorkspaceName)
	ws, err := s.Workspace.Workspace()
	if err != nil {
		fail(err)
		return
	}
	s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
		sess.Status = "running"
	})
	s.addResource(sessionID, "workspace", ws.Name, "")
	s.appendEvent(sessionID, "ok", "workspace ready: "+ws.Name)

	s.appendEvent(sessionID, "warn", fmt.Sprintf("provision %d engineers from templates (inherit mcp/env)", len(TeamEngineerTemplates)))

	for _, role := range TeamEngineerTemplates {
		name := EngineerDisplayName(req.Prefix, role.RoleLabelZH)
		s.appendEvent(sessionID, "sys", "create agent from template "+role.ID+": "+name)
		created, err := s.CreateAgentFromTemplate(CreateFromTemplateArgs{
			TemplateID:            role.ID,
			Name:                  name,
			ModelConfigurationID:  req.ModelConfigurationID,
			ModelConfigurationIDs: req.ModelConfigurationIDs,
			MCP:                   req.MCP,
			Env:                   req.Env,
			Region:                req.Region,
		})
		if err != nil {
			fail(err)
			return
		}
		s.addResource(sessionID, "agent", created.Name, "template "+role.ID)
		s.patchSession(sessionID, func(sess *TeamBootstrapSession) {
			sess.AgentNames = append(sess.AgentNames, created.Name)
		})
	}

	s.finishBootstrap(sessionID)
}

func (s *TeamService) runRetry(sessionID string, req normalizedTeamReq) {
	fail := func(err error) {
		s.setStatus(sessionID, "failed", err.Error())
		s.appendEvent(sessionID, "err", err.Error())
	}
	s.setStatus(sessionID, "running", "")
	s.appendEvent(sessionID, "sys", "retry: continue engineer provision (skip existing)")

	for _, role := range TeamEngineerTemplates {
		name := EngineerDisplayName(req.Prefix, role.RoleLabelZH)
		s.appendEvent(sessionID, "sys", "create agent from template "+role.ID+": "+name)
		created, err := s.CreateAgentFromTemplate(CreateFromTemplateArgs{
			TemplateID:            role.ID,
			Name:                  name,
			ModelConfigurationID:  req.ModelConfigurationID,
			ModelConfigurationIDs: req.ModelConfigurationIDs,
			MCP:                   req.MCP,
			Env:                   req.Env,
			Region:                req.Region,
			SkipIfExists:          true,
		})
		if err != nil {
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
	}

	s.finishBootstrap(sessionID)
}

func (s *TeamService) finishBootstrap(sessionID string) {
	s.appendEvent(sessionID, "ok", fmt.Sprintf("done: %d engineers", len(TeamEngineerTemplates)))
	s.setStatus(sessionID, "ready", "")
}

// CreateFromTemplateArgs is used by bootstrap.
type CreateFromTemplateArgs struct {
	TemplateID            string
	Name                  string
	ModelConfigurationID  string
	ModelConfigurationIDs []string
	MCP                   []MCPServer
	Env                   map[string]string
	Region                string
	SkipIfExists          bool
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
	tmpl, err := loadTeamAgentTemplate(role.EmbedName)
	if err != nil {
		return Agent{}, err
	}
	tmpl.Name = name
	configuration, err := s.Credentials.requireModelConfigurationSet(args.ModelConfigurationID, args.ModelConfigurationIDs)
	if err != nil {
		return Agent{}, err
	}
	backend := configuration.Backend
	tmpl.InheritModel = false
	tmpl.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID, ConfigurationIDs: append([]string(nil), args.ModelConfigurationIDs...)}
	tmpl.AcpBackend = backend
	tmpl.Layout.ConfigRoot = DefaultConfigRootForBackend(backend)
	if strings.TrimSpace(tmpl.Layout.WorkspaceDir) == "" {
		tmpl.Layout.WorkspaceDir = DefaultWorkspaceDir
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

	if err := s.Skills.Save(tmpl); err != nil {
		return Agent{}, err
	}
	return tmpl, nil
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
