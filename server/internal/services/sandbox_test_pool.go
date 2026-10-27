package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/tokenledger"

	"github.com/rs/zerolog/log"
)

// findReusable returns an existing interactive test sandbox for the same agent
// profile with an empty-workspace (no repo) that can be reused, preferring a
// live/running container. A row still "creating" is returned as-is (the UI will
// poll it to running). "running" rows are only reused when the container is
// actually alive; stale rows are skipped so the caller creates a fresh one.
// Returns nil when nothing suitable exists. Callers only invoke this for the
// no-repo case (repo-backed sandboxes always get a fresh workspace).
func (s *SandboxService) findReusable(ctx context.Context, profile string) *models.Sandbox {
	var rows []models.Sandbox
	if err := s.db.
		Where("purpose = ? AND profile = ? AND repo_url = ? AND status IN ?",
			"test", profile, "", []string{"running", "creating", "pulling"}).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return nil
	}
	// Prefer a confirmed-running container; fall back to one still launching.
	for i := range rows {
		if rows[i].Status == "running" && s.mgr.Status(ctx, rows[i].Name) == "running" {
			return &rows[i]
		}
	}
	for i := range rows {
		if rows[i].Status == "creating" || rows[i].Status == "pulling" {
			return &rows[i]
		}
	}
	return nil
}

// Open binds an interactive sandbox to an Agent profile. It persists a
// "creating" record and returns immediately; the (potentially slow) container
// launch + ACP handshake runs in the background and flips the record to
// "running" on success or "error" on failure. Callers (the UI) poll Get /
// List to observe the transition. repos empty/nil = empty workspace.
// Existing live sandboxes for the same agent are reused when no repos are
// configured (see findReusable); any configured clone list forces a fresh sandbox.
func (s *SandboxService) Open(ctx context.Context, profile string, repos []sandbox.RepoSpec) (*models.Sandbox, error) {
	agent, ok := s.skills.Get(profile)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	// Shared workspace files and ordinary defaults remain independent of authorization.
	agent = s.effectiveAgent(agent)
	// Reuse an existing test sandbox bound to the same agent when one is still
	// alive, instead of spinning up a fresh container every time. The chat path
	// (ensureConnected) transparently re-attaches if the live ACP connection was
	// dropped, so a reused "running" row is immediately usable.
	//
	// Skip reuse when any repos are configured: the caller wants those clones
	// in a fresh workspace, so always create a new sandbox for them.
	// Model and Git authorization changes do not reject this reuse.
	if len(repos) == 0 {
		if row := s.findReusable(ctx, profile); row != nil {
			at := time.Now().Add(s.TTL())
			s.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"destroy_at": &at, "updated_at": time.Now()})
			row.DestroyAt = &at
			log.Info().Str("name", row.Name).Str("profile", profile).Uint("id", row.ID).Msg("reusing existing test sandbox")
			return row, nil
		}
	}
	if maxN := s.MaxTestSandboxes(); s.activeCount() >= maxN {
		return nil, fmt.Errorf("已达到测试沙箱上限(%d),请先清理空闲沙箱", maxN)
	}

	modelFingerprint, err := s.currentModelFingerprint(agent)
	if err != nil {
		return nil, err
	}
	gitFingerprint, err := s.currentGitFingerprint(agent)
	if err != nil {
		return nil, err
	}
	runID := "test-" + strings.ReplaceAll(filepath.Base(profile), "/", "") + "-" + randID()
	token := s.host.RegisterRun(runID)
	// Pre-allocate the container name so the "creating" record references the
	// same container the background launch will create (keeps startup reconcile
	// consistent even if the process restarts mid-launch).
	name := sandbox.NewContainerName()

	row := &models.Sandbox{
		ModelBindingFingerprint: modelFingerprint, GitBindingsFingerprint: gitFingerprint,
		Name: name, Profile: profile, Purpose: "test", Status: "creating",
		RepoURL: firstTestRepoURL(repos), RunID: runID, Token: token,
	}
	if err := s.db.Create(row).Error; err != nil {
		s.host.UnregisterRun(runID)
		return nil, err
	}
	log.Info().Str("name", name).Str("profile", profile).Uint("id", row.ID).Msg("test sandbox creating")
	go s.startContainer(row.ID, name, profile, runID, token, repos, agent, "")
	return row, nil
}

// OpenWithEffective is like Open but injects a pre-merged effective Agent
// (shared extend → Agent overlay) and optionally overlays shared workspace
// files under BaseWorkDirSrc. Used by shared-context chat tests.
func (s *SandboxService) OpenWithEffective(ctx context.Context, profile string, repos []sandbox.RepoSpec, effective Agent, sharedWorkDir string) (*models.Sandbox, error) {
	if _, ok := s.skills.Get(profile); !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(effective)
	if err != nil {
		return nil, err
	}
	if maxN := s.MaxTestSandboxes(); s.activeCount() >= maxN {
		return nil, fmt.Errorf("已达到测试沙箱上限(%d),请先清理空闲沙箱", maxN)
	}

	runID := "ptest-" + strings.ReplaceAll(filepath.Base(profile), "/", "") + "-" + randID()
	token := s.host.RegisterRun(runID)
	name := sandbox.NewContainerName()

	row := &models.Sandbox{
		ModelBindingFingerprint: modelFingerprint, GitBindingsFingerprint: gitFingerprint,
		Name: name, Profile: profile, Purpose: "test", Status: "creating",
		RepoURL: firstTestRepoURL(repos), RunID: runID, Token: token,
	}
	if err := s.db.Create(row).Error; err != nil {
		s.host.UnregisterRun(runID)
		return nil, err
	}
	log.Info().Str("name", name).Str("profile", profile).
		Uint("id", row.ID).Msg("shared-context test sandbox creating")
	go s.startContainer(row.ID, name, profile, runID, token, repos, effective, sharedWorkDir)
	return row, nil
}

func firstTestRepoURL(repos []sandbox.RepoSpec) string {
	if len(repos) == 0 {
		return ""
	}
	return repos[0].URL
}

// startContainer performs the slow part of Open in the background: build the
// cursor home, launch the container, wait for ACP, connect, then flip the DB
// record to running. Any failure flips it to "error" (with a short TTL so the
// idle sweeper reclaims the dead row). Runs on a detached context since the
// originating HTTP request has already returned.
func (s *SandboxService) startContainer(id uint, name, profile, runID, token string, repos []sandbox.RepoSpec, agent Agent, sharedWorkDir string) {
	// Cover on-demand image pull + create (same budget as Manager createTimeout / g2.3).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	fail := func(err error) {
		log.Warn().Err(err).Str("name", name).Uint("id", id).Msg("test sandbox start failed")
		s.unregisterTestScheduler(token)
		s.host.UnregisterRun(runID)
		at := time.Now().Add(2 * time.Minute)
		s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
			"status": "error", "error": truncErr(err), "destroy_at": &at, "updated_at": time.Now(),
		})
	}
	// This runs detached from the originating request; a panic here would leave
	// the row stuck in "creating" forever. Recover and route through fail() so
	// the idle sweeper can reclaim it.
	defer func() {
		if r := recover(); r != nil {
			log.Error().Str("name", name).Uint("id", id).Interface("panic", r).
				Bytes("stack", debug.Stack()).Msg("test sandbox start panicked")
			fail(fmt.Errorf("panic during sandbox start: %v", r))
		}
	}()

	agentEnv := envauth.StripSecretEnvKeys(agent.Env)
	vars := runtime.MergeEnvIntoTemplateVars(s.testMcpVars(runID, token, profile), agentEnv)
	specs := s.buildTestSandboxSpecs(profile, runID, token, agent, vars)

	env := map[string]string{}
	for k, v := range s.env {
		if envauth.IsSecretEnvKey(k) {
			log.Warn().Str("key", k).Msg("dropped secret key from platform sandbox.env; configure it in workspace credentials")
			continue
		}
		env[k] = v
	}
	for k, v := range agentEnv {
		env[k] = substTemplate(v, vars)
	}
	// Expose run-scoped coordinates as process env. Do this before auth/model
	// normalization: vars also contains the Agent's original env for template
	// substitution, so applying it afterwards would restore a bare
	// ACP_BRIDGE_MODEL and undo the OpenCode provider prefix.
	for k, v := range vars {
		if v != "" {
			env[k] = v
		}
	}
	creds := map[string]string{}
	backend, err := runtime.ParseBackend(agent.AcpBackend)
	if err != nil {
		fail(err)
		return
	}
	runtime.ApplyCredentialBindings(env, creds, agent.CredentialBindings, s.selectedCredential)
	gitCredentials, err := runtime.ApplyGitBindings(env, creds, agent.GitBindings, s.gitCredential)
	if err != nil {
		fail(err)
		return
	}
	if err := runtime.ApplyModelBinding(env, string(backend), agent.ModelBinding, s.modelBinding); err != nil {
		fail(err)
		return
	}
	modelFingerprint := modelBindingFingerprint(agent, env)
	workDir := s.skills.WorkDir(profile)
	codexLogin := runtime.CodexLoginFileFromEnv(backend, env)
	// Align auth gate with BuildConfigHome: shared extend then Agent overlay.
	merged, err := runtime.PrepareBoundAuth(backend, env, workDir, sharedWorkDir)
	if err != nil {
		fail(err)
		return
	}
	env = merged
	home, err := sandbox.BuildConfigHome(sandbox.ConfigHomeSpec{
		BaseWorkDirSrc:       sharedWorkDir,
		WorkDirSrc:           s.skills.WorkDir(profile),
		IncludeArtifactStore: hasArtifactStoreSpec(specs),
		MCP:                  specs,
		OpenCode:             backend == runtime.BackendOpenCode,
		DeepSeekHarness:      backend == runtime.BackendDeepSeekHarness,
		BrowserMCP:           runtime.EnvEnabled(env["BROWSER_MCP"]),
		Settings:             runtime.CodeBuddySettingsForEnv(backend, env),
	})
	if err != nil {
		fail(fmt.Errorf("build cursor home: %w", err))
		return
	}
	if err := runtime.InstallBoundCodexLoginFile(backend, home, codexLogin, env); err != nil {
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
	// remote-dev parity: ROOT_PASSWORD / ACP_BRIDGE_PASSWORD so
	// code-server (8744) and ACP bridge (8765) accept the same secret for
	// proxied auto-login and direct host:port access.
	sandbox.ApplyPasswords(env, token)
	// This interactive path has no workflow `repos` variable to reference, so
	// wire GIT_REPOS explicitly here. Overrides any stray "${vars.repos}" the
	// Agent env may carry (unresolved in this path).
	env["GIT_REPOS"] = sandbox.EncodeRepos(repos)

	spec := sandbox.Spec{
		GitBindingsConfigured: true,
		GitCredentials:        gitCredentials,
		Name:                  name,
		Image:                 resolveSandboxImage(string(backend)),
		Env:                   env,
		ConfigHome:            home,
		ConfigRoot:            configRoot,
		WorkspaceDir:          agent.Layout.WorkspaceDir,
	}
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
	acp := sb.ACP().
		WithSession(sb.WorkspaceDir, mcpServersJSON(specs)).
		WithBridgeModel(env["ACP_BRIDGE_MODEL"])
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("acp connect: %w", err))
		return
	}

	destroyAt := time.Now().Add(s.TTL())
	// The gateway assigns the authoritative sandbox id; adopt it as the row's
	// Name (the pre-allocated placeholder was only a correlation label) so all
	// later control-plane calls (Status/Attach/Destroy) address the real id.
	res := s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"model_binding_fingerprint": modelFingerprint,
		"git_bindings_fingerprint":  gitBindingsFingerprint(gitCredentials),
		"status":                    "running", "name": sb.Name, "host": sb.Host, "acp_port": sb.Port,
		"code_server_port": sb.CodeServerPort, "error": "",
		"destroy_at": &destroyAt, "updated_at": time.Now(),
	})
	// Row gone (user destroyed it mid-launch) or update failed: tear everything
	// down instead of leaking an orphan container.
	if res.Error != nil || res.RowsAffected == 0 {
		acp.Close()
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		s.unregisterTestScheduler(token)
		s.host.UnregisterRun(runID)
		return
	}
	s.mu.Lock()
	s.live[id] = &liveSandbox{
		sb: sb, acp: acp, home: home,
		modelBindingFingerprint: modelFingerprint,
		gitBindingsFingerprint:  gitBindingsFingerprint(gitCredentials),
		codexAuthInjected:       codexLogin,
		codexAuthCredentialID:   runtime.ModelCredentialIDForWriteBack(env),
		codexAuthPath:           codexLiveAuthPath(backend, configRoot),
	}
	s.mu.Unlock()
	log.Info().Str("name", sb.Name).Str("profile", profile).Uint("id", id).Msg("test sandbox opened")
}

// Chat runs one streaming chat turn (text + optional image attachments)
// against a sandbox, forwarding each raw ACP event frame to onEvent. While a
// turn runs the sandbox is marked busy and its idle TTL is suspended. The busy
// guard stays as a safety net; serial ordering of multiple messages is handled
// by the caller's single-worker queue (see handlers.SandboxChat).
func (s *SandboxService) Chat(ctx context.Context, id uint, text string, images []models.PromptImage, onEvent func(json.RawMessage)) error {
	usage, byModel, err := s.ChatWithTimeout(ctx, id, text, images, 0, onEvent)
	s.recordStudioUsage(id, usage, byModel, err)
	return err
}

// recordStudioUsage ledgers an interactive Studio / Agent-workspace chat turn.
func (s *SandboxService) recordStudioUsage(id uint, usage *models.TokenUsage, byModel models.TokenUsageByModel, err error) {
	if usage == nil && byModel == nil {
		return
	}
	var row models.Sandbox
	_ = s.db.Select("id", "thread_id").Where("id = ?", id).Limit(1).Find(&row).Error
	status := models.TokenLedgerStatusOK
	if err != nil {
		status = models.TokenLedgerStatusFailed
		if errors.Is(err, context.Canceled) {
			status = models.TokenLedgerStatusCancelled
		}
	}
	tokenledger.Record(s.db, tokenledger.Entry{
		Source: models.TokenLedgerSourceStudio, Phase: models.TokenLedgerPhaseChat, Status: status,
		SandboxID: id, Usage: usage, ByModel: byModel,
	})
}

// ChatWithTimeout is Chat with a per-call turn timeout override. timeout<=0
// uses the service default (s.chatTimeout). Used by channel/cron turns that may
// legitimately run longer than an interactive UI turn.
//
// Returns the turn's TokenUsage (+ optional by-model breakdown) from
// prompt_done (nil = not reported). Callers that account usage (PmTurnRunner)
// must persist only after a successful turn.
func (s *SandboxService) ChatWithTimeout(ctx context.Context, id uint, text string, images []models.PromptImage, timeout time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	ls, row, err := s.ensureConnected(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	if ls.busy {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("sandbox busy")
	}
	ls.busy = true
	s.mu.Unlock()
	// Suspend TTL while the turn runs.
	s.db.Model(&models.Sandbox{}).Where("id = ?", id).Update("destroy_at", nil)
	resident := row.Purpose == SandboxPurposeSolo
	defer func() {
		s.mu.Lock()
		ls.busy = false
		s.mu.Unlock()
		if resident {
			s.db.Model(&models.Sandbox{}).Where("id = ?", id).Update("updated_at", time.Now())
			return
		}
		at := time.Now().Add(s.TTL())
		s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{"destroy_at": &at, "updated_at": time.Now()})
	}()

	turnTimeout := s.chatTimeout
	if timeout > 0 {
		turnTimeout = timeout
	}
	chatCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()
	result, err := ls.acp.ChatStream(chatCtx, text, images, onEvent)
	s.noteLiveCodexAuth(id, result, err)
	_ = row
	if result == nil {
		return nil, nil, err
	}
	return models.CloneTokenUsage(result.Usage), models.CloneTokenUsageByModel(result.UsageByModel), err
}
func (s *SandboxService) Cancel(id uint) {
	s.mu.Lock()
	ls := s.live[id]
	s.mu.Unlock()
	if ls != nil && ls.acp != nil {
		_ = ls.acp.Cancel()
	}
}

// ensureConnected returns the live connection, re-establishing it lazily if the
// process restarted while the container kept running.
func (s *SandboxService) ensureConnected(ctx context.Context, id uint) (*liveSandbox, *models.Sandbox, error) {
	var row models.Sandbox
	if err := s.db.First(&row, id).Error; err != nil {
		return nil, nil, fmt.Errorf("sandbox not found")
	}
	s.mu.Lock()
	ls := s.live[id]
	s.mu.Unlock()
	agent, _ := s.skills.Get(row.Profile)
	effective := s.effectiveAgent(agent)
	// Keep the model route and Git authorization captured at creation.
	// Later configuration changes do not block or retarget this sandbox.
	modelFingerprint := appliedModelFingerprint(&row, ls)
	gitFingerprint := appliedGitFingerprint(&row, ls)
	agent = effective
	if ls != nil && ls.acp != nil && ls.acp.IsConnected() {
		return ls, &row, nil
	}
	if s.mgr.Status(ctx, row.Name) != "running" {
		return nil, nil, fmt.Errorf("sandbox container 不在运行(状态:%s)", row.Status)
	}
	sb, err := s.mgr.Attach(ctx, row.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("attach: %w", err)
	}
	// Attach rebuilds the handle from the gateway record, which carries no env;
	// restore the acp-bridge secret so ACP() can log in before dialing /ws.
	sb.Password = row.Token
	// Attach can't recover the per-Agent workspace dir; restore it from the
	// Agent's layout so the reconnected session matches the original.
	if agent.Layout.WorkspaceDir != "" {
		sb.WorkspaceDir = agent.Layout.WorkspaceDir
	}
	if agent.Layout.ConfigRoot != "" {
		sb.ConfigRoot = agent.Layout.ConfigRoot
	}
	var specs []sandbox.MCPServerSpec
	if row.Purpose == SandboxPurposeSolo {
		mcpVars := runtime.MergeEnvIntoTemplateVars(nil, envauth.StripSecretEnvKeys(agent.Env))
		if s.credentialReferences != nil {
			mcpVars = runtime.MergeEnvIntoTemplateVars(s.credentialReferences(), mcpVars)
		}
		specs = filterAgentPlatformMCP(resolveAgentMCP(agent.MCP, mcpVars))
	} else {
		specs = s.buildTestSandboxSpecs(row.Profile, row.RunID, row.Token, agent,
			s.testMcpVars(row.RunID, row.Token, row.Profile))
	}
	// Rebuild + SSH-sync ConfigHome so remote pods missing mcp.json/rules
	// (hostPath ignored by K8s gateway) heal on reconnect.
	if s.mgr != nil {
		deepSeekHarness := runtime.NormalizeBackend(agent.AcpBackend) == runtime.BackendDeepSeekHarness
		sharedWorkDir := ""
		if deepSeekHarness && s.shared != nil {
			sharedWorkDir = s.shared.WorkDir()
		}
		if home, err := sandbox.BuildConfigHome(sandbox.ConfigHomeSpec{
			BaseWorkDirSrc:       sharedWorkDir,
			WorkDirSrc:           s.skills.WorkDir(row.Profile),
			DeepSeekHarness:      deepSeekHarness,
			IncludeArtifactStore: hasArtifactStoreSpec(specs),
			MCP:                  specs,
		}); err != nil {
			if deepSeekHarness {
				return nil, nil, fmt.Errorf("reconnect: rebuild DeepSeek Harness config home: %w", err)
			}
			log.Warn().Err(err).Uint("sandbox", id).Msg("reconnect: rebuild config home failed")
		} else {
			s.mgr.EnsureConfigHome(ctx, sb, home, sb.ConfigRoot)
			_ = os.RemoveAll(home)
		}
		// Re-seed helpers on reconnect so SPA mcp_advertise proxies /
		// artifact-upload land after control-plane upgrades. Idempotent.
		s.mgr.EnsureHelpers(ctx, sb)
	}
	acp := sb.ACP().
		WithSession(sb.WorkspaceDir, mcpServersJSON(specs)).
		WithBridgeModel(agent.Env["ACP_BRIDGE_MODEL"])
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		return nil, nil, fmt.Errorf("reconnect acp: %w", err)
	}
	injected := ""
	credentialID := ""
	rejected := false
	if ls != nil {
		injected = ls.codexAuthInjected
		credentialID = ls.codexAuthCredentialID
		rejected = ls.codexAuthRejected
	}
	ls = &liveSandbox{
		sb: sb, acp: acp,
		modelBindingFingerprint: modelFingerprint,
		gitBindingsFingerprint:  gitFingerprint,
		codexAuthInjected:       injected,
		codexAuthCredentialID:   credentialID,
		codexAuthPath:           codexLiveAuthPath(runtime.NormalizeBackend(agent.AcpBackend), agent.Layout.ConfigRoot),
		codexAuthRejected:       rejected,
	}
	s.mu.Lock()
	s.live[id] = ls
	s.mu.Unlock()
	return ls, &row, nil
}

// Stop removes the container but keeps the DB record (status=stopped).
func (s *SandboxService) Stop(ctx context.Context, id uint) error {
	row, err := s.Get(id)
	if err != nil {
		return err
	}
	if s.isBusyRow(row) {
		return fmt.Errorf("sandbox 正在执行任务,无法停止")
	}
	s.writeBackLiveCodex(ctx, id)
	s.teardownLive(id)
	s.archiveLog(ctx, row.Name)
	_ = s.mgr.DestroyByName(ctx, row.Name)
	return s.db.Model(&models.Sandbox{}).Where("id = ?", id).
		Updates(map[string]any{"status": "stopped", "destroy_at": nil, "updated_at": time.Now()}).Error
}

// Destroy removes the container and deletes the DB record.
func (s *SandboxService) Destroy(ctx context.Context, id uint) error {
	row, err := s.Get(id)
	if err != nil {
		return err
	}
	if s.isBusyRow(row) {
		return fmt.Errorf("sandbox 正在执行任务,无法销毁")
	}
	s.writeBackLiveCodex(ctx, id)
	s.teardownLive(id)
	s.archiveLog(ctx, row.Name)
	_ = s.mgr.DestroyByName(ctx, row.Name)
	if row.HomeDir != "" {
		_ = os.RemoveAll(row.HomeDir)
	}
	if isAgentSandboxPurpose(row.Purpose) {
		s.invokeAgentDestroyHook(row.ThreadID, row.Token)
	} else if row.Purpose == SandboxPurposeSolo {
		if s.host != nil && row.RunID != "" {
			s.host.UnregisterRun(row.RunID)
		}
		defer s.invokeSoloDestroyHook(row.ID)
	} else if row.Purpose == "test" {
		s.unregisterTestScheduler(row.Token)
		if row.RunID != "" {
			s.host.UnregisterRun(row.RunID)
		}
	} else {
		s.releaseRunMCP(row.RunID, row.ID)
	}
	return s.db.Delete(&models.Sandbox{}, id).Error
}

// releaseRunMCP revokes a workflow run's MCP token and tool sessions when one
// of its sandboxes is destroyed, but only once nothing can still use them: the
// run must be finished (the engine revokes on terminal itself) and no other
// sandbox row of the run may remain. Destroying a retired upstream node's
// sandbox must not cut off the node that is executing right now.
func (s *SandboxService) releaseRunMCP(runID string, exceptID uint) {
	if strings.TrimSpace(runID) == "" || s.runStillActive(runID) {
		return
	}
	var others int64
	s.db.Model(&models.Sandbox{}).Where("run_id = ? AND id <> ?", runID, exceptID).Count(&others)
	if others > 0 {
		return
	}
	s.host.UnregisterRun(runID)
}

// runStillActive reports whether runID can still execute nodes (and so still
// needs its MCP endpoint). A missing run row counts as finished.
func (s *SandboxService) runStillActive(runID string) bool {
	var run models.Run
	if err := s.db.Select("status").Where("id = ?", runID).First(&run).Error; err != nil {
		return false
	}
	switch run.Status {
	case "queued", "running", "waiting_human":
		return true
	}
	return false
}

// CleanupIdle destroys every non-busy sandbox; returns (destroyed, skipped).
func (s *SandboxService) CleanupIdle(ctx context.Context) (destroyed, skipped int) {
	var rows []models.Sandbox
	if err := s.db.Find(&rows).Error; err != nil {
		return 0, 0
	}
	for i := range rows {
		if s.isBusyRow(&rows[i]) || rows[i].Purpose == SandboxPurposeSolo {
			skipped++
			continue
		}
		if err := s.Destroy(ctx, rows[i].ID); err == nil {
			destroyed++
		} else {
			skipped++
		}
	}
	return destroyed, skipped
}

// ShutdownAllTestSandboxes tears down every interactive test sandbox. When
// force is true, busy sandboxes are cancelled and destroyed immediately.
func (s *SandboxService) ShutdownAllTestSandboxes(ctx context.Context, force bool) int {
	var rows []models.Sandbox
	if err := s.db.Where("purpose = ?", "test").Find(&rows).Error; err != nil {
		log.Error().Err(err).Msg("shutdown test sandboxes: query failed")
		return 0
	}
	destroyed := 0
	for i := range rows {
		row := &rows[i]
		if !force && s.isBusyRow(row) {
			continue
		}
		if force {
			s.Cancel(row.ID)
		}
		s.writeBackLiveCodex(ctx, row.ID)
		s.teardownLive(row.ID)
		s.archiveLog(ctx, row.Name)
		_ = s.mgr.DestroyByName(ctx, row.Name)
		if row.HomeDir != "" {
			_ = os.RemoveAll(row.HomeDir)
		}
		s.unregisterTestScheduler(row.Token)
		if row.RunID != "" {
			s.host.UnregisterRun(row.RunID)
		}
		if err := s.db.Delete(&models.Sandbox{}, row.ID).Error; err == nil {
			destroyed++
		}
	}
	if destroyed > 0 {
		log.Info().Int("count", destroyed).Msg("interactive sandbox destroyed")
	}
	return destroyed
}

// RunSweeper periodically destroys idle sandboxes past their TTL deadline.
func (s *SandboxService) RunSweeper(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *SandboxService) sweepOnce(ctx context.Context) {
	var rows []models.Sandbox
	if err := s.db.Where("destroy_at IS NOT NULL AND destroy_at <= ?", time.Now()).Find(&rows).Error; err != nil {
		return
	}
	s.sweepRows(ctx, rows)
}

// sweepRows destroys the due rows found by sweepOnce. Run sandboxes are
// re-checked against their current leases first; rows is only a snapshot.
func (s *SandboxService) sweepRows(ctx context.Context, rows []models.Sandbox) {
	now := time.Now()
	for i := range rows {
		if s.isBusyRow(&rows[i]) {
			continue
		}
		if rows[i].Purpose == "run" && !s.claimExpired(rows[i].ID, now) {
			continue
		}
		if err := s.Destroy(ctx, rows[i].ID); err == nil {
			s.forgetTouch(rows[i].ID)
			log.Info().Str("name", rows[i].Name).Str("purpose", rows[i].Purpose).
				Int("leases", len(rows[i].Leases)).Msg("sandbox swept (lease expired)")
		}
	}
}

// reconcileCreatingGrace keeps young "creating" rows (and their correlated
// gateway sandboxes) through startup reconcile so an in-flight Create that
// has not yet adopted the gateway id into Name is not destroyed. Aligns with
// sandbox-gateway orphanGC.minAge (10m default).
const reconcileCreatingGrace = 10 * time.Minute

// ReconcileOnStartup syncs DB rows with live gateway state after a restart and
// drops orphan managed sandboxes not tracked in the DB. Dropping a local row
// always Destroy()s the gateway instance first so sandbox_gateway.sandboxes
// (and the workload it protects) cannot outlive the platform list.
//
// adopted names run sandboxes the engine will reattach: they stay in use with
// no destroy deadline. Run sandboxes are never Attach()ed here, because Attach
// pushes a newer runtime bundle and restarts an idle bridge, which would drop
// the agent session the engine is about to reconnect to.
func (s *SandboxService) ReconcileOnStartup(ctx context.Context, adopted map[string]bool) {
	var rows []models.Sandbox
	if err := s.db.Find(&rows).Error; err != nil {
		return
	}
	// tracked = gateway ids / names that must survive the orphan pass.
	// Only rows we keep are recorded — recycled names must not block Destroy.
	tracked := map[string]bool{}
	// protectedCorr = placeholder Names of in-grace creating rows; orphan pass
	// skips gateway sandboxes labeled grasp.name=<placeholder>.
	protectedCorr := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		if row.Purpose == "run" {
			s.reconcileRunSandbox(ctx, row, adopted[row.Name], tracked)
			continue
		}
		// Young creating rows still use a placeholder Name; the gateway id is
		// adopted only after Create succeeds. Keep them and protect the
		// correlated gateway sandbox from the orphan sweep.
		if (row.Status == "creating" || row.Status == "pulling") && time.Since(row.CreatedAt) < reconcileCreatingGrace {
			tracked[row.Name] = true
			protectedCorr[row.Name] = true
			continue
		}
		switch s.mgr.Status(ctx, row.Name) {
		case "running":
			tracked[row.Name] = true
			if sb, err := s.mgr.Attach(ctx, row.Name); err == nil {
				upd := map[string]any{"status": "running", "host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort}
				if row.DestroyAt == nil && row.Purpose != SandboxPurposeSolo {
					at := time.Now().Add(s.TTL())
					upd["destroy_at"] = &at
				}
				s.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(upd)
			}
		default:
			// Non-running (creating past grace / error / stopped / not_found):
			// Destroy gateway first, then drop the list row (g1.1).
			s.destroyLocalSandbox(ctx, row)
		}
	}
	// Recycle managed gateway sandboxes with no surviving local Name (g1.3).
	managed, err := s.mgr.ListManaged(ctx)
	if err != nil {
		return
	}
	corrKey := sandbox.CorrelationNameKey()
	for i := range managed {
		sb := &managed[i]
		if tracked[sb.ID] {
			continue
		}
		if corr := sb.Labels[corrKey]; corr != "" && protectedCorr[corr] {
			continue
		}
		_ = s.mgr.DestroyByName(ctx, sb.ID)
		log.Info().Str("name", sb.ID).Msg("destroyed orphan sandbox container")
	}
}

// reconcileRunSandbox keeps a live per-run node sandbox across a restart. An
// adopted one is marked in use for the reattaching driver; any other keeps its
// persisted leases (debug window, preview) or is retired like a finished node.
// Missing or stopped containers are dropped.
func (s *SandboxService) reconcileRunSandbox(ctx context.Context, row *models.Sandbox, adopted bool, tracked map[string]bool) {
	if s.mgr.Status(ctx, row.Name) != "running" {
		s.destroyLocalSandbox(ctx, row)
		return
	}
	tracked[row.Name] = true
	if adopted {
		s.mu.Lock()
		s.runActive[row.Name] = true
		s.mu.Unlock()
		s.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("destroy_at", nil)
		return
	}
	if row.DestroyAt == nil {
		s.RetireRunSandbox(row.Name)
	}
}

// destroyLocalSandbox tears down the gateway instance (by Name and by
// grasp.name correlation) then deletes the local list row.
func (s *SandboxService) destroyLocalSandbox(ctx context.Context, row *models.Sandbox) {
	s.writeBackLiveCodex(ctx, row.ID)
	s.archiveLog(ctx, row.Name)
	_ = s.mgr.DestroyByName(ctx, row.Name)
	// Placeholder Name cannot address the real gateway id; also destroy by
	// correlation label so leaked control-plane rows are not left behind.
	s.mgr.DestroyByCorrelationName(ctx, row.Name)
	if row.HomeDir != "" {
		_ = os.RemoveAll(row.HomeDir)
	}
	switch row.Purpose {
	case "test":
		if row.RunID != "" {
			s.host.UnregisterRun(row.RunID)
		}
	case SandboxPurposeSolo:
		if s.host != nil && row.RunID != "" {
			s.host.UnregisterRun(row.RunID)
		}
		defer s.invokeSoloDestroyHook(row.ID)
	default:
		s.releaseRunMCP(row.RunID, row.ID)
	}
	s.db.Delete(&models.Sandbox{}, row.ID)
}

func (s *SandboxService) isBusyRow(row *models.Sandbox) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ls := s.live[row.ID]; ls != nil && ls.busy {
		return true
	}
	return row.Purpose == "run" && s.runActive[row.Name]
}

func (s *SandboxService) teardownLive(id uint) {
	s.mu.Lock()
	ls := s.live[id]
	delete(s.live, id)
	s.mu.Unlock()
	if ls != nil {
		if ls.acp != nil {
			ls.acp.Close()
		}
		if ls.home != "" {
			_ = os.RemoveAll(ls.home)
		}
	}
}

func (s *SandboxService) activeCount() int {
	var n int64
	s.db.Model(&models.Sandbox{}).
		Where("status IN ? AND purpose = ?", []string{"running", "creating"}, "test").
		Count(&n)
	return int(n)
}

// truncErr renders an error to a bounded string for persistence/display.
func truncErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

func (s *SandboxService) mcpVars(runID, token string) map[string]string {
	url := ""
	base := config.ResolveMCPAdvertise(s.mcpEndpoint)
	if base != "" {
		url = base + "/mcp/runs/" + runID
	}
	return map[string]string{
		"GRASP_ARTIFACT_URL":   url,
		"GRASP_ARTIFACT_TOKEN": token,
		"GRASP_RUN_ID":         runID,
		"GRASP_NODE_ID":        "test",
	}
}

func (s *SandboxService) testMcpVars(runID, token, profile string) map[string]string {
	vars := s.mcpVars(runID, token)
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return vars
	}
	base := strings.TrimRight(config.ResolveMCPAdvertise(s.mcpEndpoint), "/")
	if base == "" {
		return vars
	}
	vars["GRASP_SCHEDULER_URL"] = config.RewriteMisconfiguredMCPAdvertise(
		base + "/mcp/task-scheduler/" + url.PathEscape(profile))
	vars["GRASP_SCHEDULER_TOKEN"] = token
	return vars
}

func (s *SandboxService) buildTestSandboxSpecs(profile, runID, token string, agent Agent, vars map[string]string) []sandbox.MCPServerSpec {
	mcpVars := vars
	if s.credentialReferences != nil {
		// Resolve credential references only while rendering MCP configuration;
		// do not copy their plaintext values into the sandbox environment.
		mcpVars = runtime.MergeEnvIntoTemplateVars(s.credentialReferences(), vars)
	}
	specs := filterAgentPlatformMCP(resolveAgentMCP(agent.MCP, mcpVars))
	if strings.TrimSpace(profile) == "" {
		return specs
	}
	s.registerTestScheduler(profile, runID, token)
	if sched := BuildTestSchedulerMCPSpec(profile, token); sched.Name != "" {
		specs = dedupeMCPByName(append(specs, sched))
	}
	return specs
}

// --- helpers shared with the runtime's MCP wiring (kept local to avoid a
// cross-package dependency) -------------------------------------------------

func substTemplate(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

func substTemplateMap(m, vars map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = substTemplate(v, vars)
	}
	return out
}

func substTemplateSlice(in []string, vars map[string]string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = substTemplate(v, vars)
	}
	return out
}

// resolveAgentMCP maps an Agent's MCP entries to sandbox specs, substituting
// run-scoped template vars and dropping entries that resolve to empty.
func resolveAgentMCP(in []MCPServer, vars map[string]string) []sandbox.MCPServerSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]sandbox.MCPServerSpec, 0, len(in))
	for _, m := range in {
		if m.Name == "" {
			continue
		}
		url := config.RewriteMisconfiguredMCPAdvertise(substTemplate(m.URL, vars))
		cmd := substTemplate(m.Command, vars)
		if url == "" && cmd == "" {
			continue
		}
		out = append(out, sandbox.MCPServerSpec{
			Name:    m.Name,
			URL:     url,
			Headers: substTemplateMap(m.Headers, vars),
			Command: cmd,
			Args:    substTemplateSlice(m.Args, vars),
			Env:     substTemplateMap(m.Env, vars),
		})
	}
	return out
}

func hasArtifactStoreSpec(specs []sandbox.MCPServerSpec) bool {
	for _, sp := range specs {
		if sp.Name == ArtifactStoreMCP {
			return true
		}
	}
	return false
}

// mcpServersJSON builds the ACP session mcpServers array from resolved specs.
func mcpServersJSON(specs []sandbox.MCPServerSpec) json.RawMessage {
	if len(specs) == 0 {
		return nil
	}
	servers := make([]map[string]any, 0, len(specs))
	for _, m := range specs {
		switch {
		case m.URL != "":
			// type:http required by Claude Code / CodeBuddy; url-only is skipped.
			entry := map[string]any{"name": m.Name, "type": "http", "url": m.URL}
			if len(m.Headers) > 0 {
				hs := make([]map[string]string, 0, len(m.Headers))
				for k, v := range m.Headers {
					hs = append(hs, map[string]string{"name": k, "value": v})
				}
				entry["headers"] = hs
			}
			servers = append(servers, entry)
		case m.Command != "":
			entry := map[string]any{"name": m.Name, "command": m.Command}
			if len(m.Args) > 0 {
				entry["args"] = m.Args
			}
			if len(m.Env) > 0 {
				es := make([]map[string]string, 0, len(m.Env))
				for k, v := range m.Env {
					es = append(es, map[string]string{"name": k, "value": v})
				}
				entry["env"] = es
			}
			servers = append(servers, entry)
		}
	}
	if len(servers) == 0 {
		return nil
	}
	b, _ := json.Marshal(servers)
	return b
}
