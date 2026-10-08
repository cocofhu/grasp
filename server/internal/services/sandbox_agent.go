package services

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// SandboxPurposeAgent marks thread-bound agent session sandboxes.
const SandboxPurposeAgent = "agent"

// AgentSandboxDestroyHook is invoked when a thread-bound agent sandbox is
// destroyed so MCP session tokens (and thread sandbox refs) can be revoked.
type AgentSandboxDestroyHook func(projectID, threadID, token string)

// SetAgentSandboxDestroyHook registers cleanup for purpose=agent|pm sandboxes.
func (s *SandboxService) SetAgentSandboxDestroyHook(fn AgentSandboxDestroyHook) {
	s.agentOnDestroy = fn
}

// SetTestSchedulerHooks registers purpose=test read-only scheduler injection.
func (s *SandboxService) SetTestSchedulerHooks(h TestSchedulerHooks) {
	s.testScheduler = h
}

// SetCodexLoginWriteBack registers the project-credential update used when a
// Codex sandbox's auth.json changes. The callback must not log the file body.
func (s *SandboxService) SetCodexLoginWriteBack(fn func(projectID, content string) error) {
	s.codexLoginWriteBack = fn
}

func codexLiveAuthPath(backend runtime.AcpBackend, configRoot string) string {
	if runtime.NormalizeBackend(string(backend)) != runtime.BackendCodex {
		return ""
	}
	return runtime.CodexAuthPath(configRoot)
}

// writeBackLiveCodex reads auth.json before the container is destroyed. A read
// failure, an unchanged file, or a confirmed login refusal keeps the previous
// credential. The body is not logged.
func (s *SandboxService) writeBackLiveCodex(ctx context.Context, id uint, projectID string) {
	if s.codexLoginWriteBack == nil || strings.TrimSpace(projectID) == "" {
		return
	}
	s.mu.Lock()
	ls := s.live[id]
	rejected := false
	if ls != nil {
		rejected = ls.codexAuthRejected
	}
	s.mu.Unlock()
	if ls == nil || ls.sb == nil || ls.codexAuthPath == "" {
		return
	}
	body, err := ls.sb.ReadFile(ctx, ls.codexAuthPath)
	next, ok := runtime.ShouldWriteBackCodexLogin(err, string(body), ls.codexAuthInjected, rejected)
	if !ok {
		if err != nil {
			log.Warn().Err(err).Uint("sandbox", id).Msg("codex login writeback skipped; sandbox file unreadable")
		}
		return
	}
	if werr := s.codexLoginWriteBack(projectID, next); werr != nil {
		log.Warn().Err(werr).Str("project", projectID).Uint("sandbox", id).Msg("codex login writeback failed")
	}
}

// noteLiveCodexAuth records whether the latest turn's CLI error refused the
// login. A later successful turn clears the flag so a refreshed auth.json can
// still be written back. Narration and tool output are not consulted.
func (s *SandboxService) noteLiveCodexAuth(id uint, result *sandbox.ChatResult, err error) {
	errorText := ""
	failed := false
	if result != nil {
		errorText = result.ErrorText
		failed = result.Failed
	}
	rejected := runtime.CodexTurnAuthRejected(err, errorText, failed)
	s.mu.Lock()
	if ls := s.live[id]; ls != nil && ls.codexAuthPath != "" {
		ls.codexAuthRejected = rejected
	}
	s.mu.Unlock()
}

func (s *SandboxService) registerTestScheduler(projectID, profile, runID, token string) {
	if s.testScheduler.Register != nil && strings.TrimSpace(projectID) != "" {
		s.testScheduler.Register(projectID, profile, runID, token)
	}
}

func (s *SandboxService) unregisterTestScheduler(projectID, token string) {
	if s.testScheduler.Unregister != nil && strings.TrimSpace(projectID) != "" && token != "" {
		s.testScheduler.Unregister(token)
	}
}

// AgentSandboxOpenOpts configures a thread-bound agent sandbox open.
// Role-specific MCP servers (e.g. pm-progress) are passed in PlatformSpecs by
// the caller; this API itself is role-agnostic.
type AgentSandboxOpenOpts struct {
	Profile       string
	ProjectID     string
	ThreadID      string
	SharedToken   string
	PlatformSpecs []sandbox.MCPServerSpec
	// Reuse, when true, reuses a running/creating sandbox for the same thread.
	// Cron and one-shot turns should leave this false.
	Reuse bool
	// RunIDPrefix labels the synthetic run id (e.g. "agent", "agentcron").
	RunIDPrefix string
}

// OpenAgentSandbox opens a thread-bound agent sandbox. When opts.Reuse is true
// and a live sandbox exists for the thread, it is returned instead of creating
// a new one.
func (s *SandboxService) OpenAgentSandbox(ctx context.Context, opts AgentSandboxOpenOpts) (*models.Sandbox, bool, error) {
	profile := strings.TrimSpace(opts.Profile)
	threadID := strings.TrimSpace(opts.ThreadID)
	if profile == "" || threadID == "" {
		return nil, false, fmt.Errorf("profile and threadID are required")
	}
	agent, ok := s.skills.Get(profile)
	if !ok {
		return nil, false, fmt.Errorf("agent %q not found", profile)
	}
	home := strings.TrimSpace(agent.ProjectID)
	runtimePID := strings.TrimSpace(opts.ProjectID)
	if home != runtimePID {
		return nil, false, fmt.Errorf("agent %q 主项目为 %q，与运行项目 %q 不一致", profile, home, runtimePID)
	}
	// Merge SharedAgent Token env / meta (Agent → Shared 选源).
	agent = s.effectiveAgent(agent, runtimePID)

	if opts.Reuse {
		var existing models.Sandbox
		if err := s.db.Where("purpose = ? AND thread_id = ? AND status IN ?",
			SandboxPurposeAgent, threadID, []string{"running", "creating", "pulling"}).
			Order("created_at desc").First(&existing).Error; err == nil {
			if existing.Status == "running" && s.mgr.Status(ctx, existing.Name) == "running" {
				at := time.Now().Add(s.TTL())
				s.db.Model(&models.Sandbox{}).Where("id = ?", existing.ID).
					Updates(map[string]any{"destroy_at": &at, "updated_at": time.Now()})
				existing.DestroyAt = &at
				log.Info().Str("name", existing.Name).Str("thread", threadID).
					Uint("id", existing.ID).Msg("reusing agent sandbox for thread")
				return &existing, true, nil
			}
			if existing.Status == "creating" || existing.Status == "pulling" {
				return &existing, true, nil
			}
		}
	}

	if maxN := s.MaxTestSandboxes(); s.activeAgentSandboxCount()+s.activeCount() >= maxN {
		return nil, false, fmt.Errorf("已达到沙箱上限(%d),请先清理空闲沙箱", maxN)
	}

	prefix := strings.TrimSpace(opts.RunIDPrefix)
	if prefix == "" {
		if opts.Reuse {
			prefix = "agent"
		} else {
			prefix = "agentcron"
		}
	}
	runID := prefix + "-" + sanitizeID(opts.ProjectID) + "-" + sanitizeID(threadID)
	if !opts.Reuse {
		runID += "-" + fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
	}
	name := sandbox.NewContainerName()
	row := &models.Sandbox{
		Name: name, Profile: profile, Purpose: SandboxPurposeAgent, Status: "creating",
		RunID: runID, Token: opts.SharedToken, ProjectID: opts.ProjectID, ThreadID: threadID,
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, false, err
	}
	log.Info().Str("name", name).Str("profile", profile).Str("thread", threadID).
		Bool("reuse", opts.Reuse).Uint("id", row.ID).Msg("agent sandbox creating")
	go s.startAgentContainer(row.ID, name, profile, opts.ProjectID, threadID, runID, opts.SharedToken, opts.PlatformSpecs, agent)
	return row, false, nil
}

// OpenAgentSandboxFresh always creates a new agent sandbox (no thread reuse).
// Used by cron and other one-shot agent turns.
func (s *SandboxService) OpenAgentSandboxFresh(ctx context.Context, profile, projectID, threadID, sharedToken string, platformSpecs []sandbox.MCPServerSpec) (*models.Sandbox, error) {
	row, _, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile:       profile,
		ProjectID:     projectID,
		ThreadID:      threadID,
		SharedToken:   sharedToken,
		PlatformSpecs: platformSpecs,
		Reuse:         false,
		RunIDPrefix:   "agentcron",
	})
	return row, err
}

func (s *SandboxService) startAgentContainer(id uint, name, profile, projectID, threadID, runID, sharedToken string, platformSpecs []sandbox.MCPServerSpec, agent Agent) {
	// Cover on-demand image pull + create (same budget as Manager createTimeout / g2.3).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	fail := func(err error) {
		log.Warn().Err(err).Str("name", name).Uint("id", id).Msg("agent sandbox start failed")
		if s.agentOnDestroy != nil {
			s.agentOnDestroy(projectID, threadID, sharedToken)
		}
		at := time.Now().Add(2 * time.Minute)
		s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
			"status": "error", "error": truncErr(err), "destroy_at": &at, "updated_at": time.Now(),
		})
	}
	defer func() {
		if r := recover(); r != nil {
			log.Error().Str("name", name).Uint("id", id).Interface("panic", r).
				Bytes("stack", debug.Stack()).Msg("agent sandbox start panicked")
			fail(fmt.Errorf("panic during sandbox start: %v", r))
		}
	}()

	vars := map[string]string{}
	for _, sp := range platformSpecs {
		switch sp.Name {
		case MemoryStoreMCP:
			vars["GRASP_MEMORY_URL"] = sp.URL
			vars["GRASP_MEMORY_TOKEN"] = sharedToken
		case ContextStoreMCP:
			vars["GRASP_CONTEXT_URL"] = sp.URL
			vars["GRASP_CONTEXT_TOKEN"] = sharedToken
		case TaskSchedulerMCP:
			vars["GRASP_SCHEDULER_URL"] = sp.URL
			vars["GRASP_SCHEDULER_TOKEN"] = sharedToken
		case PmProgressMCP, PmWorkflowReadMCP, PmWorkflowWriteMCP:
			vars["GRASP_PM_URL"] = sp.URL
			vars["GRASP_PM_TOKEN"] = sharedToken
		}
	}

	agentEnv := envauth.StripSecretEnvKeys(agent.Env)
	vars = runtime.MergeEnvIntoTemplateVars(vars, agentEnv)
	mcpVars := vars
	if s.projectCredentialReferences != nil {
		// Credential references are scoped to user-authored MCP templates. Keep
		// them out of vars so the ordinary sandbox environment never receives
		// these values as global keys.
		mcpVars = runtime.MergeEnvIntoTemplateVars(s.projectCredentialReferences(projectID), vars)
	}
	specs := filterAgentPlatformMCP(resolveAgentMCP(agent.MCP, mcpVars))
	specs = append(specs, platformSpecs...)
	specs = dedupeMCPByName(specs)

	env := map[string]string{}
	for k, v := range s.env {
		if envauth.IsSecretEnvKey(k) {
			log.Warn().Str("key", k).Msg("dropped secret key from platform sandbox.env; configure it in project credentials")
			continue
		}
		env[k] = v
	}
	for k, v := range agentEnv {
		if strings.Contains(v, "GRASP_ARTIFACT") {
			continue
		}
		env[k] = v
	}
	for k, v := range vars {
		env[k] = v
	}
	// Project UI credentials are the highest-priority source. Apply them after
	// template/Agent vars so interactive, PM and cron sandboxes match workflow
	// resolver precedence.
	var projectCreds map[string]string
	if s.projectCredentials != nil {
		projectCreds = s.projectCredentials(projectID)
		overlayProjectCredentialEnv(env, projectCreds)
	}
	backend, err := runtime.ParseBackend(agent.AcpBackend)
	if err != nil {
		fail(err)
		return
	}
	if backend == runtime.BackendOpenCode && s.openCodeCredential != nil {
		runtime.ApplyOpenCodeSelection(env, s.openCodeCredential(projectID, agent.OpenCodeCredentialID))
	}
	runtime.ApplyAgentCredentialChoice(env, projectCreds, projectID, runtime.AgentCredentialChoice{
		Backend:              string(backend),
		GitCredentialType:    agent.GitCredentialType,
		AiCredentialID:       agent.AiCredentialID,
		GitCredentialID:      agent.GitCredentialID,
		SshHostsCredentialID: agent.SshHostsCredentialID,
	}, s.selectedCredential)
	workDir := s.skills.WorkDir(profile)
	sharedWorkDir := ""
	if s.shared != nil && strings.TrimSpace(projectID) != "" {
		sharedWorkDir = s.shared.WorkDir(projectID)
	}
	codexLogin := runtime.CodexLoginFileFromEnv(backend, env)
	merged, err := runtime.PrepareAuthEnv(backend, env, workDir, sharedWorkDir)
	if err != nil {
		fail(err)
		return
	}
	env = merged
	ocDoc := runtime.OpenCodeConfigForEnvWithCatalog(
		context.Background(), backend, env, s.openCodeCatalog,
	)
	if err := runtime.RequireOpenCodePlaceholderKey(ocDoc, env); err != nil {
		fail(err)
		return
	}

	home, err := sandbox.BuildConfigHome(sandbox.ConfigHomeSpec{
		BaseWorkDirSrc:       sharedWorkDir,
		WorkDirSrc:           s.skills.WorkDir(profile),
		IncludeArtifactStore: false,
		MCP:                  specs,
		OpenCode:             backend == runtime.BackendOpenCode,
		BrowserMCP:           runtime.EnvEnabled(env["BROWSER_MCP"]),
		Settings:             runtime.CodeBuddySettingsForEnv(backend, env),
		OpenCodeConfig:       ocDoc,
	})
	if err != nil {
		fail(fmt.Errorf("build cursor home: %w", err))
		return
	}
	if err := runtime.InstallCodexLoginFile(backend, home, codexLogin); err != nil {
		_ = os.RemoveAll(home)
		fail(err)
		return
	}

	env["AGENT_PROVIDER"] = string(backend)
	configRoot := agent.Layout.ConfigRoot
	if backend == runtime.BackendCodex && strings.TrimSpace(configRoot) == "" {
		configRoot = runtime.CodexConfigRoot
	}
	env["CONFIG_ROOT"] = configRoot
	if backend == runtime.BackendCodex {
		env["CODEX_HOME"] = configRoot
	}
	env["GRASP_PROJECT_ID"] = projectID
	env["GRASP_THREAD_ID"] = threadID
	env["GRASP_RUN_ID"] = runID
	sandbox.ApplyPasswords(env, sharedToken)
	env["GIT_REPOS"] = sandbox.EncodeRepos(nil)

	spec := sandbox.Spec{
		Name:         name,
		Image:        resolveSandboxImage(string(backend)),
		Env:          env,
		ConfigHome:   home,
		ConfigRoot:   configRoot,
		WorkspaceDir: agent.Layout.WorkspaceDir,
	}
	ApplyProjectSSHToSpec(&spec, projectCreds)
	sb, err := s.mgr.Create(ctx, spec)
	if err != nil {
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("create sandbox: %w", err))
		return
	}
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("acp not ready: %w", err))
		return
	}
	acp := sb.ACP().WithSession(sb.WorkspaceDir, mcpServersJSON(specs))
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("acp connect: %w", err))
		return
	}

	destroyAt := time.Now().Add(s.TTL())
	res := s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"status": "running", "name": sb.Name, "host": sb.Host, "acp_port": sb.Port,
		"code_server_port": sb.CodeServerPort, "error": "",
		"destroy_at": &destroyAt, "updated_at": time.Now(),
	})
	if res.Error != nil || res.RowsAffected == 0 {
		acp.Close()
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		if s.agentOnDestroy != nil {
			s.agentOnDestroy(projectID, threadID, sharedToken)
		}
		return
	}
	s.mu.Lock()
	s.live[id] = &liveSandbox{
		sb: sb, acp: acp, home: home,
		codexAuthInjected: codexLogin,
		codexAuthPath:     codexLiveAuthPath(backend, configRoot),
	}
	s.mu.Unlock()
	log.Info().Str("name", sb.Name).Str("profile", profile).Str("thread", threadID).
		Uint("id", id).Msg("agent sandbox opened")
}

func (s *SandboxService) activeAgentSandboxCount() int {
	var n int64
	s.db.Model(&models.Sandbox{}).
		Where("status IN ? AND purpose = ?",
			[]string{"running", "creating", "pulling"}, SandboxPurposeAgent).
		Count(&n)
	return int(n)
}

func isAgentSandboxPurpose(p string) bool {
	return p == SandboxPurposeAgent
}

// AgentProjectMatches reports whether the Agent's home project equals projectID.
func AgentProjectMatches(a Agent, projectID string) bool {
	home := strings.TrimSpace(a.ProjectID)
	return home != "" && home == strings.TrimSpace(projectID)
}

// IsProjectPlatformMCP reports whether a single MCP entry is a project-scoped
// platform MCP (memory/context/scheduler) by name or URL path.
func IsProjectPlatformMCP(name, url string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case MemoryStoreMCP, ContextStoreMCP, TaskSchedulerMCP:
		return true
	}
	return strings.Contains(url, "/mcp/memory-store/") ||
		strings.Contains(url, "/mcp/context-store/") ||
		strings.Contains(url, "/mcp/task-scheduler/")
}

// filterAgentPlatformMCP drops artifact-store and platform MCP names so they
// are not double-injected from Agent mcp[] (platform appends real endpoints).
func filterAgentPlatformMCP(in []sandbox.MCPServerSpec) []sandbox.MCPServerSpec {
	if len(in) == 0 {
		return nil
	}
	deny := map[string]bool{
		ArtifactStoreMCP: true, MemoryStoreMCP: true, ContextStoreMCP: true,
		TaskSchedulerMCP: true, PmProgressMCP: true,
		PmWorkflowReadMCP: true, PmWorkflowWriteMCP: true, PmAgentFSMCP: true,
		PmPrdManagerMCP: true,
	}
	out := make([]sandbox.MCPServerSpec, 0, len(in))
	for _, sp := range in {
		name := strings.ToLower(strings.TrimSpace(sp.Name))
		if deny[name] {
			continue
		}
		if strings.Contains(sp.URL, "/mcp/runs/") || strings.Contains(sp.URL, "/mcp/pm/") ||
			strings.Contains(sp.URL, "/mcp/memory-store/") || strings.Contains(sp.URL, "/mcp/context-store/") ||
			strings.Contains(sp.URL, "/mcp/task-scheduler/") {
			continue
		}
		out = append(out, sp)
	}
	return out
}

func dedupeMCPByName(in []sandbox.MCPServerSpec) []sandbox.MCPServerSpec {
	seen := map[string]bool{}
	out := make([]sandbox.MCPServerSpec, 0, len(in))
	for _, sp := range in {
		n := strings.ToLower(strings.TrimSpace(sp.Name))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, sp)
	}
	return out
}

func sanitizeID(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, " ", "")
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}

// BuildTestSchedulerMCPSpec returns a single task-scheduler inject spec for
// purpose=test sandboxes (read-only scheduler session; no memory/context).
func BuildTestSchedulerMCPSpec(agentName, sharedToken string) sandbox.MCPServerSpec {
	base := strings.TrimRight(config.ResolveMCPAdvertise(""), "/")
	if base == "" || sharedToken == "" || strings.TrimSpace(agentName) == "" {
		return sandbox.MCPServerSpec{}
	}
	auth := map[string]string{"Authorization": "Bearer " + sharedToken}
	escapedAgent := url.PathEscape(agentName)
	return sandbox.MCPServerSpec{
		Name:    TaskSchedulerMCP,
		URL:     config.RewriteMisconfiguredMCPAdvertise(base + "/mcp/task-scheduler/" + escapedAgent),
		Headers: auth,
	}
}

// BuildAgentPlatformMCPSpecs builds role-agnostic platform MCP inject specs
// (memory-store / context-store / task-scheduler) for any Agent session.
func BuildAgentPlatformMCPSpecs(projectID, agentName, sharedToken string) []sandbox.MCPServerSpec {
	base := strings.TrimRight(config.ResolveMCPAdvertise(""), "/")
	if base == "" || sharedToken == "" || strings.TrimSpace(agentName) == "" {
		return nil
	}
	auth := map[string]string{"Authorization": "Bearer " + sharedToken}
	escapedAgent := url.PathEscape(agentName)
	return []sandbox.MCPServerSpec{
		{Name: MemoryStoreMCP, URL: config.RewriteMisconfiguredMCPAdvertise(base + "/mcp/memory-store/" + projectID), Headers: auth},
		{Name: ContextStoreMCP, URL: config.RewriteMisconfiguredMCPAdvertise(base + "/mcp/context-store/" + projectID), Headers: auth},
		{Name: TaskSchedulerMCP, URL: config.RewriteMisconfiguredMCPAdvertise(base + "/mcp/task-scheduler/" + escapedAgent), Headers: auth},
	}
}

// BuildPmRoleMCPSpecs builds PM-only role MCP specs (pm-progress / pm-workflow-read /
// pm-workflow-write / pm-agent-fs / pm-prd-manager).
// nil enabledMcps means defaults; an explicit empty list injects none.
func BuildPmRoleMCPSpecs(projectID, sharedToken string, enabledMcps []string) []sandbox.MCPServerSpec {
	base := strings.TrimRight(config.ResolveMCPAdvertise(""), "/")
	if base == "" || sharedToken == "" {
		return nil
	}
	enabledMcps = EffectivePmEnabledMcps(enabledMcps)
	auth := map[string]string{"Authorization": "Bearer " + sharedToken}
	out := make([]sandbox.MCPServerSpec, 0, len(enabledMcps))
	for _, id := range enabledMcps {
		out = append(out, sandbox.MCPServerSpec{
			Name: id, URL: config.RewriteMisconfiguredMCPAdvertise(base + "/mcp/pm/" + projectID + "/" + id), Headers: auth,
		})
	}
	return out
}
