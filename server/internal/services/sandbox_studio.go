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
	"github.com/cocofhu/grasp/internal/mcp"
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
func (s *SandboxService) Open(ctx context.Context, profile string, repos []models.SessionRepo) (*models.Sandbox, error) {
	agent, ok := s.skills.Get(profile)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	// Shared workspace files and ordinary defaults remain independent of authorization.
	agent = s.effectiveAgent(agent)
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(agent, repos)
	if err != nil {
		return nil, err
	}
	// Reuse an existing test sandbox bound to the same agent when one is still
	// alive, instead of spinning up a fresh container every time. The chat path
	// (ensureConnected) transparently re-attaches if the live ACP connection was
	// dropped, so a reused "running" row is immediately usable.
	//
	// Skip reuse when any repos are configured: the caller wants those clones
	// in a fresh workspace, so always create a new sandbox for them.
	if len(repos) == 0 {
		if row := s.findReusable(ctx, profile); row != nil {
			s.mu.Lock()
			bindingErr := checkAppliedBindings(modelFingerprint, gitFingerprint, row, s.live[row.ID])
			s.mu.Unlock()
			if bindingErr != nil {
				return nil, bindingErr
			}
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

	runID := "test-" + strings.ReplaceAll(filepath.Base(profile), "/", "") + "-" + randID()
	token := mcp.NewToken()
	// Pre-allocate the container name so the "creating" record references the
	// same container the background launch will create (keeps startup reconcile
	// consistent even if the process restarts mid-launch).
	name := sandbox.NewContainerName()

	row := &models.Sandbox{
		ModelBindingFingerprint: modelFingerprint, GitBindingsFingerprint: gitFingerprint,
		Name: name, Profile: profile, Purpose: "test", Status: "creating",
		RepoURL: firstRepoURL(repos), Repos: repos, RunID: runID, Token: token,
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	log.Info().Str("name", name).Str("profile", profile).Uint("id", row.ID).Msg("test sandbox creating")
	go s.startContainer(row.ID, name, profile, runID, token, repos, agent, "")
	return row, nil
}

// OpenWithEffective is like Open but injects a pre-merged effective Agent
// (shared extend → Agent overlay) and optionally overlays shared workspace
// files under BaseWorkDirSrc. Used by shared-context chat tests.
func (s *SandboxService) OpenWithEffective(ctx context.Context, profile string, repos []models.SessionRepo, effective Agent, sharedWorkDir string) (*models.Sandbox, error) {
	if _, ok := s.skills.Get(profile); !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(effective, repos)
	if err != nil {
		return nil, err
	}
	if maxN := s.MaxTestSandboxes(); s.activeCount() >= maxN {
		return nil, fmt.Errorf("已达到测试沙箱上限(%d),请先清理空闲沙箱", maxN)
	}

	runID := "ptest-" + strings.ReplaceAll(filepath.Base(profile), "/", "") + "-" + randID()
	token := mcp.NewToken()
	name := sandbox.NewContainerName()

	row := &models.Sandbox{
		ModelBindingFingerprint: modelFingerprint, GitBindingsFingerprint: gitFingerprint,
		Name: name, Profile: profile, Purpose: "test", Status: "creating",
		RepoURL: firstRepoURL(repos), Repos: repos, RunID: runID, Token: token,
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	log.Info().Str("name", name).Str("profile", profile).
		Uint("id", row.ID).Msg("shared-context test sandbox creating")
	go s.startContainer(row.ID, name, profile, runID, token, repos, effective, sharedWorkDir)
	return row, nil
}

func firstRepoURL(repos []models.SessionRepo) string {
	if len(repos) == 0 {
		return ""
	}
	return repos[0].RepoURL
}

// startContainer performs the slow part of Open in the background: build the
// cursor home, launch the container, wait for ACP, connect, then flip the DB
// record to running. Any failure flips it to "error" (with a short TTL so the
// idle sweeper reclaims the dead row). Runs on a detached context since the
// originating HTTP request has already returned.
func (s *SandboxService) startContainer(id uint, name, profile, runID, token string, snapshots []models.SessionRepo, agent Agent, sharedWorkDir string) {
	// Cover on-demand image pull + create (same budget as Manager createTimeout / g2.3).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	fail := func(err error) {
		log.Warn().Err(err).Str("name", name).Uint("id", id).Msg("test sandbox start failed")
		s.unregisterTestScheduler(token)
		at := time.Now().Add(2 * time.Minute)
		s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
			"status": "error", "error": truncErr(err), "destroy_at": &at, "updated_at": time.Now(),
		})
		s.releaseSandboxScope("test", runID, "test_sandbox_failed")
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
		env[k] = runtime.SubstVars(v, vars)
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
	runtime.StripGitAuthorizationEnv(env, creds)
	repos, repoCreds := s.repoGit(snapshots)
	gitCredentials, err := runtime.ResolveRepoCredentials(repoCreds, s.gitCredential)
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
		Codex:                backend == runtime.BackendCodex,
		DeepSeekHarness:      backend == runtime.BackendDeepSeekHarness,
		BrowserMCP:           runtime.EnvEnabled(env["BROWSER_MCP"]),
		Settings:             runtime.CodeBuddySettingsForEnv(backend, env),
	})
	if err != nil {
		fail(fmt.Errorf("build cursor home: %w", err))
		return
	}
	codexCredentialID, codexVersion, err := s.installCodexLogin(backend, home, codexLogin, env)
	if err != nil {
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
	sandbox.ApplyRepoEnv(env, repos)
	env["GRASP_RUN_ID"] = runID
	env["GRASP_LAUNCH_KIND"] = "test"

	spec := sandbox.Spec{
		GitCredentials: gitCredentials,
		Name:           name,
		Image:          resolveSandboxImage(string(backend)),
		Env:            env,
		ConfigHome:     home,
		ConfigRoot:     configRoot,
		WorkspaceDir:   agent.Layout.WorkspaceDir,
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
		WithSession(sb.WorkspaceDir, runtime.MCPServersJSON(specs)).
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
		s.releaseSandboxScope("test", runID, "test_sandbox_removed")
		return
	}
	s.mu.Lock()
	s.live[id] = &liveSandbox{
		sb: sb, acp: acp, home: home,
		modelBindingFingerprint: modelFingerprint,
		gitBindingsFingerprint:  gitBindingsFingerprint(gitCredentials),
	}
	s.mu.Unlock()
	s.registerLiveCodex(id, sb, codexCredentialID, codexLiveAuthPath(backend, configRoot), codexVersion)
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

// ChatWithTimeout is Chat with an optional total time limit. timeout<=0 sets
// none: an active turn may run as long as it needs, and a stalled one ends on
// the sandbox no-activity watchdog or ctx cancellation.
//
// Returns the turn's TokenUsage (+ optional by-model breakdown) from
// prompt_done (nil = not reported), including failed turns. Provider failures
// are errors even when their transport completed with prompt_done.
func (s *SandboxService) ChatWithTimeout(ctx context.Context, id uint, text string, images []models.PromptImage, timeout time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	ls, row, err := s.ensureConnected(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	if ls.busy || ls.switching {
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

	chatCtx, cancel := context.WithCancel(ctx)
	if timeout > 0 {
		chatCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	result, err := s.codexLiveTurn(chatCtx, id, func() (*sandbox.ChatResult, error) {
		result, err := ls.acp.ChatStream(chatCtx, text, images, onEvent)
		if result != nil && result.OpID != "" {
			source := models.TokenLedgerSourceStudio
			switch row.Purpose {
			case SandboxPurposeSolo:
				source = models.TokenLedgerSourceSolo
			case SandboxPurposeAgent:
				source = models.TokenLedgerSourcePM
			}
			tokenledger.Record(s.db, tokenledger.Entry{
				ID: result.OpID, At: result.EndedAt, Source: source, Phase: models.TokenLedgerPhaseChat,
				Partial: result.StopReason == "" || result.UsagePartial,
				Status:  result.UsageStatus(err), ThreadID: row.ThreadID, SandboxID: id,
				Usage: result.Usage, ByModel: result.UsageByModel,
			})
			if result.Usage != nil {
				result.Usage.LedgerRecorded = true
			}
		}
		return result, err
	})
	_ = row
	if result == nil {
		return nil, nil, err
	}
	// ChatStream keeps provider failures in the result for callers such as
	// React that render partial output themselves. This API only returns usage,
	// so preserve the failure before discarding the rest of that result. Keep
	// existing transport errors and partial timeout/cancel outcomes unchanged.
	if err == nil && (result.Failed || !result.Interrupted && strings.TrimSpace(result.ErrorText) != "") {
		failure := strings.TrimSpace(result.ErrorText)
		if failure == "" {
			if result.StopReason == "refusal" {
				failure = "Agent 拒绝了本轮请求（stopReason=refusal）"
			} else {
				failure = "Agent 本轮执行失败（stopReason=failed）"
			}
		}
		err = errors.New(failure)
	}
	return models.CloneTokenUsage(result.Usage), models.CloneTokenUsageByModel(result.UsageByModel), err
}
func (s *SandboxService) Cancel(id uint) {
	s.mu.Lock()
	ls := s.live[id]
	switching := ls != nil && ls.switching
	s.mu.Unlock()
	if switching {
		return
	}
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
	switching := ls != nil && ls.switching
	s.mu.Unlock()
	if switching {
		return nil, nil, fmt.Errorf("sandbox 正在切换编码工具")
	}
	agent, _ := s.skills.Get(row.Profile)
	effective := s.effectiveAgent(agent)
	if row.Purpose == SandboxPurposeSolo {
		selected, routeErr := s.appliedSoloAgent(&row, ls, effective)
		if routeErr != nil {
			return nil, nil, routeErr
		}
		effective = selected
	}
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(effective, row.Repos)
	if err != nil {
		return nil, nil, err
	}
	if err := checkAppliedBindings(modelFingerprint, gitFingerprint, &row, ls); err != nil {
		return nil, nil, err
	}
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
		specs = append(specs, s.soloPlatformSpecs(&row)...)
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
			OpenCode:             runtime.NormalizeBackend(agent.AcpBackend) == runtime.BackendOpenCode,
			Codex:                runtime.NormalizeBackend(agent.AcpBackend) == runtime.BackendCodex,
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
		WithSession(sb.WorkspaceDir, runtime.MCPServersJSON(specs)).
		WithBridgeModel(agent.Env["ACP_BRIDGE_MODEL"])
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		return nil, nil, fmt.Errorf("reconnect acp: %w", err)
	}
	var prepared *preparedAgentContainer
	home := ""
	if ls != nil {
		prepared, home = ls.prepared, ls.home
	}
	ls = &liveSandbox{
		sb: sb, acp: acp, prepared: prepared, home: home,
		soloRouteAgent:          &agent,
		modelBindingFingerprint: modelFingerprint,
		gitBindingsFingerprint:  gitFingerprint,
	}
	s.mu.Lock()
	s.live[id] = ls
	s.mu.Unlock()
	if !s.codexLogins.Registered(liveCodexKey(id)) {
		backend := runtime.NormalizeBackend(agent.AcpBackend)
		credentialID := runtime.CodexLoginCredentialForBinding(backend, agent.ModelBinding, s.modelBinding)
		s.registerLiveCodex(id, sb, credentialID, codexLiveAuthPath(backend, agent.Layout.ConfigRoot), "")
	} else {
		s.codexLogins.Bind(liveCodexKey(id), sb)
	}
	return ls, &row, nil
}

// Stop removes the container but keeps the DB record (status=stopped).
func (s *SandboxService) Stop(ctx context.Context, id uint) error {
	row, err := s.Get(id)
	if err != nil {
		return err
	}
	if row.Purpose == SandboxPurposeSolo {
		return fmt.Errorf("Solo 沙箱请通过关闭会话释放")
	}
	if s.isBusyRow(row) {
		return fmt.Errorf("sandbox 正在执行任务,无法停止")
	}
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
	if row.Purpose == SandboxPurposeSolo {
		unlock, lockErr := s.lockSoloRecovery(ctx, id)
		if lockErr != nil {
			return lockErr
		}
		defer unlock()
		if row, err = s.Get(id); err != nil {
			return err
		}
	}
	if s.isBusyRow(row) {
		return fmt.Errorf("sandbox 正在执行任务,无法销毁")
	}
	s.teardownLive(id)
	s.archiveLog(ctx, row.Name)
	_ = s.mgr.DestroyByName(ctx, row.Name)
	if row.HomeDir != "" {
		_ = os.RemoveAll(row.HomeDir)
	}
	if isAgentSandboxPurpose(row.Purpose) {
		s.invokeAgentDestroyHook(row.ThreadID, row.Token)
	} else if row.Purpose == SandboxPurposeSolo {
		defer s.invokeSoloDestroyHook(row.ID)
	} else if row.Purpose == "test" {
		s.unregisterTestScheduler(row.Token)
	}
	if err := s.db.Delete(&models.Sandbox{}, id).Error; err != nil {
		return err
	}
	s.releaseSandboxScope(row.Purpose, row.RunID, "sandbox_destroyed")
	return nil
}

// releaseSandboxScope frees the host's in-memory state for a namespace the
// sandbox owns (Solo / test) once its row is gone or errored. Workflow run
// namespaces belong to the engine and are never released here.
func (s *SandboxService) releaseSandboxScope(purpose, runID, reason string) {
	if s.host == nil || runID == "" || (purpose != SandboxPurposeSolo && purpose != "test") {
		return
	}
	s.host.ReleaseRun(runID, reason)
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
		s.teardownLive(row.ID)
		s.archiveLog(ctx, row.Name)
		_ = s.mgr.DestroyByName(ctx, row.Name)
		if row.HomeDir != "" {
			_ = os.RemoveAll(row.HomeDir)
		}
		s.unregisterTestScheduler(row.Token)
		if err := s.db.Delete(&models.Sandbox{}, row.ID).Error; err == nil {
			s.releaseSandboxScope(row.Purpose, row.RunID, "sandbox_destroyed")
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
		if rows[i].Purpose == SandboxPurposeSolo {
			// Residents never expire, including legacy rows with a stale TTL.
			continue
		}
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
		if row.Purpose == SandboxPurposeSolo {
			s.reconcileSoloSandbox(ctx, row, tracked, protectedCorr)
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
		status, err := s.mgr.ProbeStatus(ctx, row.Name)
		if sandbox.GatewayRecordMissing(err) {
			status, err = "not_found", nil // retain the existing non-resident cleanup policy
		}
		if err != nil {
			tracked[row.Name], protectedCorr[row.Name] = true, true
			log.Warn().Err(err).Str("name", row.Name).Msg("sandbox status unavailable; retaining sandbox")
			continue
		}
		switch status {
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
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	if sandbox.GatewayRecordMissing(err) {
		status, err = "not_found", nil
	}
	if err != nil {
		tracked[row.Name] = true
		log.Warn().Err(err).Str("name", row.Name).Msg("run sandbox status unavailable; retaining sandbox")
		return
	}
	if status != "running" {
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
	s.archiveLog(ctx, row.Name)
	_ = s.mgr.DestroyByName(ctx, row.Name)
	// Placeholder Name cannot address the real gateway id; also destroy by
	// correlation label so leaked control-plane rows are not left behind.
	s.mgr.DestroyByCorrelationName(ctx, row.Name)
	if row.HomeDir != "" {
		_ = os.RemoveAll(row.HomeDir)
	}
	if row.Purpose == SandboxPurposeSolo {
		defer s.invokeSoloDestroyHook(row.ID)
	}
	s.db.Delete(&models.Sandbox{}, row.ID)
	s.releaseSandboxScope(row.Purpose, row.RunID, "sandbox_destroyed")
}

func (s *SandboxService) isBusyRow(row *models.Sandbox) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ls := s.live[row.ID]; ls != nil && (ls.busy || ls.switching) {
		return true
	}
	return row.Purpose == "run" && s.runActive[row.Name]
}

func (s *SandboxService) teardownLive(id uint) {
	s.mu.Lock()
	ls := s.live[id]
	delete(s.live, id)
	s.mu.Unlock()
	s.codexLogins.Unregister(liveCodexKey(id))
	if ls != nil {
		if ls.acp != nil {
			ls.acp.Close()
		}
		if ls.sb != nil && ls.sb.IsLocal() {
			ls.sb.Destroy(context.Background())
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
		url := config.RewriteMisconfiguredMCPAdvertise(runtime.SubstVars(m.URL, vars))
		cmd := runtime.SubstVars(m.Command, vars)
		if url == "" && cmd == "" {
			continue
		}
		out = append(out, sandbox.MCPServerSpec{
			Name:    m.Name,
			URL:     url,
			Headers: runtime.SubstMap(m.Headers, vars),
			Command: cmd,
			Args:    runtime.SubstSlice(m.Args, vars),
			Env:     runtime.SubstMap(m.Env, vars),
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
