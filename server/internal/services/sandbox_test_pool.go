package services

import (
	"context"
	"encoding/json"
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

	"github.com/rs/zerolog/log"
)

// findReusable returns an existing interactive test sandbox for the same agent
// profile and project with an empty-workspace (no repo) that can be reused, preferring a
// live/running container. A row still "creating" is returned as-is (the UI will
// poll it to running). "running" rows are only reused when the container is
// actually alive; stale rows are skipped so the caller creates a fresh one.
// Returns nil when nothing suitable exists. Callers only invoke this for the
// no-repo case (repo-backed sandboxes always get a fresh workspace).
func (s *SandboxService) findReusable(ctx context.Context, profile, projectID string) *models.Sandbox {
	var rows []models.Sandbox
	if err := s.db.
		Where("purpose = ? AND profile = ? AND repo_url = ? AND project_id = ? AND status IN ?",
			"test", profile, "", projectID, []string{"running", "creating", "pulling"}).
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
func (s *SandboxService) Open(ctx context.Context, profile string, repos []sandbox.RepoSpec, projectID string) (*models.Sandbox, error) {
	agent, ok := s.skills.Get(profile)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	// Data ownership follows the Agent's home project, not the UI-selected one.
	_ = projectID
	projectID = strings.TrimSpace(agent.ProjectID)
	// Merge SharedAgent so team bootstrap / Open paths inject shared SSH meta.
	agent = s.effectiveAgent(agent, projectID)
	// Reuse an existing test sandbox bound to the same agent when one is still
	// alive, instead of spinning up a fresh container every time. The chat path
	// (ensureConnected) transparently re-attaches if the live ACP connection was
	// dropped, so a reused "running" row is immediately usable.
	//
	// Skip reuse when any repos are configured: the caller wants those clones
	// in a fresh workspace, so always create a new sandbox for them.
	if len(repos) == 0 {
		if row := s.findReusable(ctx, profile, projectID); row != nil {
			at := time.Now().Add(s.TTL())
			s.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"destroy_at": &at, "updated_at": time.Now()})
			row.DestroyAt = &at
			log.Info().Str("name", row.Name).Str("profile", profile).Str("project", projectID).Uint("id", row.ID).Msg("reusing existing test sandbox")
			return row, nil
		}
	}
	if maxN := s.MaxTestSandboxes(); s.activeCount() >= maxN {
		return nil, fmt.Errorf("已达到测试沙箱上限(%d),请先清理空闲沙箱", maxN)
	}

	runID := "test-" + strings.ReplaceAll(filepath.Base(profile), "/", "") + "-" + randID()
	token := s.host.RegisterRun(runID)
	// Pre-allocate the container name so the "creating" record references the
	// same container the background launch will create (keeps startup reconcile
	// consistent even if the process restarts mid-launch).
	name := sandbox.NewContainerName()

	row := &models.Sandbox{
		Name: name, Profile: profile, Purpose: "test", Status: "creating",
		RepoURL: firstTestRepoURL(repos), RunID: runID, Token: token, ProjectID: projectID,
	}
	if err := s.db.Create(row).Error; err != nil {
		s.host.UnregisterRun(runID)
		return nil, err
	}
	log.Info().Str("name", name).Str("profile", profile).Str("project", projectID).Uint("id", row.ID).Msg("test sandbox creating")
	go s.startContainer(row.ID, name, profile, projectID, runID, token, repos, agent, "")
	return row, nil
}

// OpenWithEffective is like Open but injects a pre-merged effective Agent
// (project shared extend → Agent overlay) and optionally overlays shared
// workspace files under BaseWorkDirSrc. Used by project-context chat tests.
func (s *SandboxService) OpenWithEffective(ctx context.Context, profile, projectID string, repos []sandbox.RepoSpec, effective Agent, sharedWorkDir string) (*models.Sandbox, error) {
	if _, ok := s.skills.Get(profile); !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	projectID = strings.TrimSpace(projectID)
	if maxN := s.MaxTestSandboxes(); s.activeCount() >= maxN {
		return nil, fmt.Errorf("已达到测试沙箱上限(%d),请先清理空闲沙箱", maxN)
	}

	runID := "ptest-" + strings.ReplaceAll(filepath.Base(profile), "/", "") + "-" + randID()
	token := s.host.RegisterRun(runID)
	name := sandbox.NewContainerName()

	row := &models.Sandbox{
		Name: name, Profile: profile, Purpose: "test", Status: "creating",
		RepoURL: firstTestRepoURL(repos), RunID: runID, Token: token, ProjectID: projectID,
	}
	if err := s.db.Create(row).Error; err != nil {
		s.host.UnregisterRun(runID)
		return nil, err
	}
	log.Info().Str("name", name).Str("profile", profile).Str("project", projectID).
		Uint("id", row.ID).Msg("project-context test sandbox creating")
	go s.startContainer(row.ID, name, profile, projectID, runID, token, repos, effective, sharedWorkDir)
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
func (s *SandboxService) startContainer(id uint, name, profile, projectID, runID, token string, repos []sandbox.RepoSpec, agent Agent, sharedWorkDir string) {
	// Cover on-demand image pull + create (same budget as Manager createTimeout / g2.3).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	fail := func(err error) {
		log.Warn().Err(err).Str("name", name).Uint("id", id).Msg("test sandbox start failed")
		s.unregisterTestScheduler(projectID, token)
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

	vars := runtime.MergeEnvIntoTemplateVars(s.testMcpVars(runID, token, projectID, profile), agent.Env)
	specs := s.buildTestSandboxSpecs(projectID, profile, runID, token, agent, vars)

	env := map[string]string{}
	for k, v := range s.env {
		if envauth.IsPlatformAuthEnvKey(k) {
			log.Warn().Str("key", k).Msg("dropped platform sandbox.env official CLI auth key; use GRASP_* on Agent or shared env")
			continue
		}
		env[k] = v
	}
	for k, v := range agent.Env {
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
	backend := runtime.NormalizeBackend(agent.AcpBackend)
	workDir := s.skills.WorkDir(profile)
	// Align auth gate with BuildConfigHome: shared extend then Agent overlay.
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
		IncludeArtifactStore: hasArtifactStoreSpec(specs),
		MCP:                  specs,
		OpenCode:             backend == runtime.BackendOpenCode,
		Codex:                backend == runtime.BackendCodex,
		BrowserMCP:           runtime.EnvEnabled(env["BROWSER_MCP"]),
		Settings:             runtime.CodeBuddySettingsForEnv(backend, env),
		OpenCodeConfig:       ocDoc,
		AgentName:            profile,
		ProfilesRoot:         s.profilesRoot,
		GlobalRulesDir:       s.platformRulesRoot,
	})
	if err != nil {
		fail(fmt.Errorf("build cursor home: %w", err))
		return
	}

	env["AGENT_PROVIDER"] = string(backend)
	env["CONFIG_ROOT"] = agent.Layout.ConfigRoot
	if backend == runtime.BackendCodex {
		env["CODEX_HOME"] = agent.Layout.ConfigRoot
	}
	// remote-dev parity: PASSWORD / ROOT_PASSWORD / CURSOR_ACP_PASSWORD so
	// code-server (8744) and ACP bridge (8765) accept the same secret for
	// proxied auto-login and direct host:port access.
	sandbox.ApplyPasswords(env, token)
	// This interactive path has no workflow `repos` variable to reference, so
	// wire GIT_REPOS explicitly here. Overrides any stray "${vars.repos}" the
	// Agent env may carry (unresolved in this path).
	env["GIT_REPOS"] = sandbox.EncodeRepos(repos)

	spec := sandbox.Spec{
		Name:         name,
		Image:        resolveSandboxImage(string(backend)),
		Env:          env,
		ConfigHome:   home,
		ConfigRoot:   agent.Layout.ConfigRoot,
		WorkspaceDir: agent.Layout.WorkspaceDir,
	}
	ApplyAgentSSHToSpec(&spec, agent)
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
		"status": "running", "name": sb.Name, "host": sb.Host, "acp_port": sb.Port,
		"code_server_port": sb.CodeServerPort, "error": "",
		"destroy_at": &destroyAt, "updated_at": time.Now(),
	})
	// Row gone (user destroyed it mid-launch) or update failed: tear everything
	// down instead of leaking an orphan container.
	if res.Error != nil || res.RowsAffected == 0 {
		acp.Close()
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		s.unregisterTestScheduler(projectID, token)
		s.host.UnregisterRun(runID)
		return
	}
	s.mu.Lock()
	s.live[id] = &liveSandbox{sb: sb, acp: acp, home: home}
	s.mu.Unlock()
	log.Info().Str("name", sb.Name).Str("profile", profile).Uint("id", id).Msg("test sandbox opened")
}

// Chat runs one streaming chat turn (text + optional image attachments)
// against a sandbox, forwarding each raw ACP event frame to onEvent. While a
// turn runs the sandbox is marked busy and its idle TTL is suspended. The busy
// guard stays as a safety net; serial ordering of multiple messages is handled
// by the caller's single-worker queue (see handlers.SandboxChat).
func (s *SandboxService) Chat(ctx context.Context, id uint, text string, images []models.PromptImage, onEvent func(json.RawMessage)) error {
	_, _, err := s.ChatWithTimeout(ctx, id, text, images, 0, onEvent)
	return err
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
	defer func() {
		s.mu.Lock()
		ls.busy = false
		s.mu.Unlock()
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
	agent, _ := s.skills.Get(row.Profile)
	// Attach can't recover the per-Agent workspace dir; restore it from the
	// Agent's layout so the reconnected session matches the original.
	if agent.Layout.WorkspaceDir != "" {
		sb.WorkspaceDir = agent.Layout.WorkspaceDir
	}
	if agent.Layout.ConfigRoot != "" {
		sb.ConfigRoot = agent.Layout.ConfigRoot
	}
	specs := s.buildTestSandboxSpecs(row.ProjectID, row.Profile, row.RunID, row.Token, agent,
		s.testMcpVars(row.RunID, row.Token, row.ProjectID, row.Profile))
	// Rebuild + SSH-sync ConfigHome so remote pods missing mcp.json/rules
	// (hostPath ignored by K8s gateway) heal on reconnect.
	if s.mgr != nil {
		if home, err := sandbox.BuildConfigHome(sandbox.ConfigHomeSpec{
			WorkDirSrc:           s.skills.WorkDir(row.Profile),
			Codex:                runtime.NormalizeBackend(agent.AcpBackend) == runtime.BackendCodex,
			IncludeArtifactStore: hasArtifactStoreSpec(specs),
			MCP:                  specs,
			AgentName:            row.Profile,
			ProfilesRoot:         s.profilesRoot,
			GlobalRulesDir:       s.platformRulesRoot,
		}); err != nil {
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
	ls = &liveSandbox{sb: sb, acp: acp}
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
	s.teardownLive(id)
	s.archiveLog(ctx, row.Name)
	_ = s.mgr.DestroyByName(ctx, row.Name)
	if row.HomeDir != "" {
		_ = os.RemoveAll(row.HomeDir)
	}
	if isAgentSandboxPurpose(row.Purpose) {
		if s.agentOnDestroy != nil {
			s.agentOnDestroy(row.ProjectID, row.ThreadID, row.Token)
		}
	} else if row.Purpose == "test" {
		s.unregisterTestScheduler(row.ProjectID, row.Token)
		if row.RunID != "" {
			s.host.UnregisterRun(row.RunID)
		}
	} else if row.RunID != "" {
		s.host.UnregisterRun(row.RunID)
	}
	return s.db.Delete(&models.Sandbox{}, id).Error
}

// CleanupIdle destroys every non-busy sandbox; returns (destroyed, skipped).
func (s *SandboxService) CleanupIdle(ctx context.Context) (destroyed, skipped int) {
	var rows []models.Sandbox
	if err := s.db.Find(&rows).Error; err != nil {
		return 0, 0
	}
	for i := range rows {
		if s.isBusyRow(&rows[i]) {
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
		s.unregisterTestScheduler(row.ProjectID, row.Token)
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
	for i := range rows {
		if s.isBusyRow(&rows[i]) {
			continue
		}
		if err := s.Destroy(ctx, rows[i].ID); err == nil {
			log.Info().Str("name", rows[i].Name).Msg("test sandbox swept (idle ttl)")
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
func (s *SandboxService) ReconcileOnStartup(ctx context.Context) {
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
		// Per-run node sandboxes are owned by the runtime provider; after a
		// restart their driving goroutine is gone, so any surviving container
		// is an orphan. Destroy it and drop the record.
		if row.Purpose == "run" {
			s.destroyLocalSandbox(ctx, row)
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
				if row.DestroyAt == nil {
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
	if row.RunID != "" {
		s.host.UnregisterRun(row.RunID)
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

func (s *SandboxService) testMcpVars(runID, token, projectID, profile string) map[string]string {
	vars := s.mcpVars(runID, token)
	projectID = strings.TrimSpace(projectID)
	profile = strings.TrimSpace(profile)
	if projectID == "" || profile == "" {
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

func (s *SandboxService) buildTestSandboxSpecs(projectID, profile, runID, token string, agent Agent, vars map[string]string) []sandbox.MCPServerSpec {
	specs := filterAgentPlatformMCP(resolveAgentMCP(agent.MCP, vars))
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return specs
	}
	s.registerTestScheduler(projectID, profile, runID, token)
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
