package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

// registerLive records a node's sandbox so its event log can be read straight
// from the container while the turn runs (survives a UI refresh). deregisterLive
// drops it when the sandbox is torn down. acp may be nil for callers that only
// need the event-log handle (react sessions already keep acp elsewhere).
func (c *acpProvider) registerLive(req NodeReq, sb *sandbox.Sandbox, acp *sandbox.ACPClient) {
	c.mu.Lock()
	c.registerLiveLocked(req, sb, acp)
}

// runAbortWatch is one run's abort record while RunAgent calls are in flight.
type runAbortWatch struct {
	last     uint64 // abortSeq of the run's latest AbortRun (0 = none seen)
	watchers int    // RunAgent calls of the run in flight
}

// watchRunAborts snapshots abortSeq for a RunAgent call of runID and keeps
// the run's abort record alive until done is called.
func (c *acpProvider) watchRunAborts(runID string) (snap uint64, done func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runAborts == nil {
		c.runAborts = map[string]*runAbortWatch{}
	}
	w := c.runAborts[runID]
	if w == nil {
		w = &runAbortWatch{}
		c.runAborts[runID] = w
	}
	w.watchers++
	var once sync.Once
	return c.abortSeq, func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if w.watchers--; w.watchers == 0 && c.runAborts[runID] == w {
				delete(c.runAborts, runID)
			}
		})
	}
}

// abortedSince reports whether runID was aborted after snap was taken by
// watchRunAborts (whose done must not have run yet).
func (c *acpProvider) abortedSince(runID string, snap uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.abortedSinceLocked(runID, snap)
}

func (c *acpProvider) abortedSinceLocked(runID string, snap uint64) bool {
	w := c.runAborts[runID]
	return w != nil && w.last > snap
}

// recordAbortLocked records an AbortRun of runID; c.mu must be held.
func (c *acpProvider) recordAbortLocked(runID string) {
	c.abortSeq++
	if w := c.runAborts[runID]; w != nil {
		w.last = c.abortSeq
	}
}

// registerLiveUnlessAborted is registerLive that refuses (false) when the run
// was aborted since snap (watchRunAborts). Check and insert share one lock
// hold, so an AbortRun either sees the registration (and tears it down) or
// is seen.
func (c *acpProvider) registerLiveUnlessAborted(req NodeReq, sb *sandbox.Sandbox, acp *sandbox.ACPClient, snap uint64) bool {
	c.mu.Lock()
	if c.abortedSinceLocked(req.RunID, snap) {
		c.mu.Unlock()
		return false
	}
	c.registerLiveLocked(req, sb, acp)
	return true
}

// registerLiveLocked inserts the live / in-flight entries and starts the
// timeline ingest; c.mu must be held and is released.
func (c *acpProvider) registerLiveLocked(req NodeReq, sb *sandbox.Sandbox, acp *sandbox.ACPClient) {
	key := reactKey(req)
	c.live[key] = sb
	if acp != nil {
		c.inflightACP[key] = acp
	}
	host, port := "", 0
	if sb != nil {
		host, port = sb.Host, sb.Port
	}
	c.mu.Unlock()
	if c.timeline != nil && sb != nil {
		c.timeline.startIngest(req.RunID, req.NodeID, host, port, sb.Password, acpTurnBusy(acp, c.emit != nil))
	}
}

func (c *acpProvider) deregisterLive(req NodeReq) {
	c.mu.Lock()
	key := reactKey(req)
	delete(c.live, key)
	delete(c.inflightACP, key)
	c.mu.Unlock()
	if c.timeline != nil {
		c.timeline.stop(req.RunID, req.NodeID)
	}
}

// LiveNodeEvents reads the in-flight sandbox's current-turn event snapshot
// (last prompt_begin) for streaming bubble seeds. ok=false, err=nil when the
// node is not currently running in a live sandbox (finished / never started
// here), so callers fall back to the persisted snapshot. When a live sandbox is
// registered but the bridge read fails, ok=false with a non-nil err so
// callers can surface a distinguishable failure (never pretend live+empty).
func (c *acpProvider) LiveNodeEvents(ctx context.Context, runID, nodeID string) ([]models.AcpEvent, bool, error) {
	if c.timeline != nil {
		if e, ok := c.timeline.get(runID, nodeID); ok {
			return append([]models.AcpEvent(nil), e.events...), e.live, nil
		}
	}
	c.mu.Lock()
	sb := c.live[runID+"|"+nodeID]
	c.mu.Unlock()
	if sb == nil {
		return nil, false, nil
	}
	res, _, err := sandbox.FetchEventLogLastTurnWithPassword(ctx, sb.Host, sb.Port, sb.Password)
	if err != nil {
		return nil, false, err
	}
	if res == nil {
		return nil, false, fmt.Errorf("event log unavailable")
	}
	events := res.AcpEvents()
	if c.timeline != nil {
		c.timeline.upsert(runID, nodeID, events)
	}
	return events, true, nil
}

// LiveNodeEventsPage reads a page of current-turn events from the in-flight
// sandbox (streaming seed path). Fetch failures return ok=false with a non-nil
// err (aligned with LiveNodeEvents) — never live=true with an empty page that
// masks the error.
func (c *acpProvider) LiveNodeEventsPage(ctx context.Context, runID, nodeID, cursor string, limit int) ([]models.AcpEvent, string, bool, bool, error) {
	if c.timeline != nil {
		if ev, next, more, live, ok := c.timeline.page(runID, nodeID, cursor, limit); ok {
			return ev, next, more, live, nil
		}
	}
	c.mu.Lock()
	sb := c.live[runID+"|"+nodeID]
	c.mu.Unlock()
	if sb == nil {
		return nil, "", false, false, nil
	}
	page, err := sandbox.FetchEventLogPageWithPassword(ctx, sb.Host, sb.Port, cursor, limit, sb.Password)
	if err != nil {
		return nil, "", false, false, err
	}
	if page == nil {
		return nil, "", false, false, fmt.Errorf("event log page unavailable")
	}
	// Page may span multiple recent turns; seed/timeline only keep the last.
	events := sandbox.AggregateLastTurnFrames(page.Events)
	if c.timeline != nil {
		c.timeline.upsert(runID, nodeID, events)
	}
	return events, page.NextCursor, page.HasMore, true, nil
}

// snapshotEvents captures the full agent event log straight from the sandbox so
// it can be persisted as the node's StateRun snapshot BEFORE the sandbox is
// destroyed — that saved snapshot is the only record once the container is
// gone. Best-effort: falls back to the streamed aggregation if the read fails.
func (c *acpProvider) snapshotEvents(ctx context.Context, sb *sandbox.Sandbox, fallback []models.AcpEvent) []models.AcpEvent {
	if sb == nil {
		return fallback
	}
	frames, _, err := sandbox.NewEventLogReader(sb.Host, sb.Port, sb.Password).Raw(ctx)
	if err != nil {
		return fallback
	}
	if _, bracketed := transcriptTurns(fallback); bracketed {
		chunks := sandbox.SplitFramesByPromptBegin(frames)
		turns := make([][]models.AcpEvent, len(chunks))
		for i, chunk := range chunks {
			turns[i] = sandbox.AggregateFrames(chunk)
		}
		return mergeTranscriptSnapshot(turns, fallback)
	}
	if se := sandbox.AggregateFrames(frames); len(se) > 0 {
		return se
	}
	return fallback
}

// SetEventSink wires a live-event publisher (the engine's broker). Optional;
// when nil the provider falls back to a single blocking turn.
func (c *acpProvider) SetEventSink(fn func(runID, nodeID string, events []models.AcpEvent, busy bool)) {
	c.emit = fn
}

// SetSandboxRegistry wires the platform sandbox store so per-run node
// sandboxes are recorded (and shown in the UI) for their lifetime.
func (c *acpProvider) SetSandboxRegistry(r SandboxRegistry) {
	c.registry = r
}

// ArchiveRunSandboxLogs best-effort archives live logs via the platform
// SandboxService when wired as registry.
func (c *acpProvider) ArchiveRunSandboxLogs(ctx context.Context, runID string) (int, string) {
	if c == nil || c.registry == nil {
		return 0, "未接线沙箱注册表，未能拉取 live logs"
	}
	a, ok := c.registry.(RunSandboxLogArchiver)
	if !ok {
		return 0, "沙箱注册表不支持归档 live logs"
	}
	return a.ArchiveRunSandboxLogs(ctx, runID)
}

// beginRunSandbox records a "creating" placeholder row for a node sandbox before
// the (slow) gateway provisioning, so it shows up in the sandbox list / node
// live log as "starting" instead of a 404. No-op when the registry is absent or
// does not implement RunSandboxBeginner (e.g. test fakes).
func (c *acpProvider) beginRunSandbox(req NodeReq, name, home string) {
	if c.registry == nil || name == "" {
		return
	}
	b, ok := c.registry.(RunSandboxBeginner)
	if !ok {
		return
	}
	b.BeginRunSandbox(RunSandboxInfo{
		Name:         name,
		Profile:      models.AgentProfile(req.Config),
		RunID:        req.RunID,
		WorkflowID:   req.WorkflowID,
		WorkflowName: req.WorkflowName,
		NodeID:       req.NodeID,
		RepoURL:      firstRepoURL(req),
		HomeDir:      home,
		Token:        req.Token,
	})
}

// registerRunSandbox records a freshly-created node sandbox in the platform
// store. No-op when no registry is wired.
func (c *acpProvider) registerRunSandbox(req NodeReq, sb *sandbox.Sandbox, home string) {
	if c.registry == nil || sb == nil {
		return
	}
	repo := firstRepoURL(req)
	c.registry.RegisterRunSandbox(RunSandboxInfo{
		Name:           sb.Name,
		Profile:        models.AgentProfile(req.Config),
		RunID:          req.RunID,
		WorkflowID:     req.WorkflowID,
		WorkflowName:   req.WorkflowName,
		NodeID:         req.NodeID,
		Host:           sb.Host,
		ACPPort:        sb.Port,
		CodeServerPort: sb.CodeServerPort,
		RepoURL:        repo,
		HomeDir:        home,
		Token:          req.Token,
	})
}

// deregisterRunSandbox clears a node sandbox record once its container is
// torn down. No-op when no registry is wired.
func (c *acpProvider) deregisterRunSandbox(name string) {
	if c.registry == nil || name == "" {
		return
	}
	c.registry.UnregisterRunSandbox(name)
}

// retireRunSandbox is the end-of-node teardown for a per-run sandbox. Rather
// than destroying the container immediately, it closes the driving ACP session
// but keeps the container (and its cursor-home mount) alive and hands it to the
// store's idle-TTL sweeper, so the sandbox can be inspected (terminal / IDE /
// ACP / container logs) for debugging. When the store can't retire (no registry
// or capability — e.g. tests), it falls back to an immediate destroy so nothing
// is leaked.
func (c *acpProvider) retireRunSandbox(sb *sandbox.Sandbox, acp *sandbox.ACPClient, home string) {
	if c.releaseSharedSandbox(sb, acp) {
		return
	}
	if acp != nil {
		acp.Close()
	}
	if sb == nil {
		removeHome(home)
		return
	}
	if r, ok := c.registry.(RunSandboxRetirer); ok {

		r.RetireRunSandbox(sb.Name)
		return
	}

	c.deregisterRunSandbox(sb.Name)
	sb.Destroy(context.Background())
	removeHome(home)
}

// discardSandbox tears down a broken sandbox between retries: close the ACP
// session, drop its registry/live bookkeeping, destroy the container, and
// remove its cursor-home. Unlike retireRunSandbox it does NOT keep the
// container for debugging — a faulted sandbox has no value and would leak.
// Best-effort snapshotEvents runs before destroy so ACP events are not lost
// when the container is recycled.
func (c *acpProvider) discardSandbox(ctx context.Context, req NodeReq, sb *sandbox.Sandbox, acp *sandbox.ACPClient, home string, fallback []models.AcpEvent) []models.AcpEvent {
	events := c.snapshotEvents(ctx, sb, fallback)
	c.deregisterLive(req)
	// A shared sandbox outlives the fault: the retry opens a fresh session in
	// it, and replaces the container only if it is no longer reachable.
	if c.releaseSharedSandbox(sb, acp) {
		return events
	}
	if acp != nil {
		acp.Close()
	}
	if sb == nil {
		removeHome(home)
		return events
	}
	c.deregisterRunSandbox(sb.Name)
	sb.Destroy(context.Background())
	removeHome(home)
	return events
}

func (c *acpProvider) openSandbox(ctx context.Context, req NodeReq) (*sandbox.Sandbox, *sandbox.ACPClient, string, error) {
	if req.SandboxMode == models.SandboxModeShared && c.shared != nil {
		if sb, acp, home, handled, err := c.openSharedSandbox(ctx, req); handled {
			return sb, acp, home, err
		}
	}
	sb, acp, home, _, err := c.openNodeSandbox(ctx, req)
	return sb, acp, home, err
}

// openNodeSandbox creates a fresh sandbox for the node and connects its agent
// session. It also returns the spec the sandbox was created from.
func (c *acpProvider) openNodeSandbox(ctx context.Context, req NodeReq) (*sandbox.Sandbox, *sandbox.ACPClient, string, sandbox.Spec, error) {
	spec, err := c.spec(req)
	if err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		return nil, nil, "", spec, fmt.Errorf("%w: %v", errSandboxConfig, err)
	}
	home := spec.ConfigHome

	placeholder := sandbox.NewContainerName()
	spec.Name = placeholder
	c.beginRunSandbox(req, placeholder, home)
	sb, err := c.mgr.Create(ctx, spec)
	if err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		c.deregisterRunSandbox(placeholder)
		removeHome(home)
		return nil, nil, "", spec, fmt.Errorf("%w: create sandbox: %v", errSandboxSetup, err)
	}
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		c.deregisterRunSandbox(placeholder)
		sb.Destroy(context.Background())
		removeHome(home)
		return nil, nil, "", spec, fmt.Errorf("%w: acp not ready: %v", errSandboxSetup, err)
	}
	acp := sb.ACP().
		WithSession(sb.WorkspaceDir, c.mcpServers(req)).
		WithIdleTimeoutFunc(c.agentIdle).
		WithBridgeModel(spec.Env["ACP_BRIDGE_MODEL"])
	if err := acp.Connect(ctx); err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		c.deregisterRunSandbox(placeholder)
		acp.Close()
		sb.Destroy(context.Background())
		removeHome(home)
		return nil, nil, "", spec, fmt.Errorf("%w: acp connect: %v", errSandboxSetup, err)
	}
	c.registerRunSandbox(req, sb, home)
	return sb, acp, home, spec, nil
}

// sandboxAttacher is implemented by managers that can rebuild a handle for a
// running sandbox without touching its runtime (see Manager.AttachNoEnsure).
type sandboxAttacher interface {
	AttachNoEnsure(ctx context.Context, id string) (*sandbox.Sandbox, error)
}

// reattachKnownWait bounds how long a reattached connection waits for the
// bridge's first queue_state. A var so tests can shrink it.
var reattachKnownWait = 15 * time.Second

// attachSandbox reconnects to a node sandbox that kept running across a
// control-plane restart. The bridge reuses the agent session already open in
// the container. Failures are errSandboxSetup so RunAgent retries the node in
// a fresh sandbox, and the unusable sandbox is torn down.
func (c *acpProvider) attachSandbox(ctx context.Context, req NodeReq) (*sandbox.Sandbox, *sandbox.ACPClient, error) {
	name := req.ReattachSandbox
	att, ok := c.mgr.(sandboxAttacher)
	if !ok {
		c.deregisterRunSandbox(name)
		return nil, nil, fmt.Errorf("%w: sandbox manager cannot reattach", errSandboxSetup)
	}
	sb, err := att.AttachNoEnsure(ctx, name)
	if err != nil {
		c.deregisterRunSandbox(name)
		return nil, nil, fmt.Errorf("%w: reattach %s: %v", errSandboxSetup, name, err)
	}
	sb.Password = req.Token
	sb.ConfigRoot = c.agentLayout(models.AgentProfile(req.Config), c.effectiveAgent(req)).ConfigRoot
	model, _ := sb.ExecScript(ctx, 10*time.Second, "bash", "printenv "+EnvACPBridgeModel+" || true")
	acp := sb.ACP().
		WithSession(sb.WorkspaceDir, c.mcpServers(req)).
		WithIdleTimeoutFunc(c.agentIdle).
		WithBridgeModel(strings.TrimSpace(model))
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		c.deregisterRunSandbox(name)
		sb.Destroy(context.Background())
		return nil, nil, fmt.Errorf("%w: reattach acp connect: %v", errSandboxSetup, err)
	}
	log.Info().Str("run", req.RunID).Str("node", req.NodeID).Str("sandbox", name).
		Msg("reattached node sandbox after restart")
	c.adoptSharedSandbox(req, sb)
	return sb, acp, nil
}

// reattachTurn collects the main turn of a reattached sandbox: it waits out a
// turn still running on the bridge, then rebuilds the node's first turn from
// the event log. A nil result with nil error means no turn was ever sent and
// the caller should send the main prompt.
func (c *acpProvider) reattachTurn(ctx context.Context, sb *sandbox.Sandbox, acp *sandbox.ACPClient) (*sandbox.ChatResult, error) {
	st, ok := acp.WaitBridgeKnown(ctx, reattachKnownWait)
	if !ok {
		return nil, fmt.Errorf("%w: bridge reported no queue state after reattach", errSandboxSetup)
	}
	if st.RunningOpID != "" {
		if err := acp.AwaitTurn(ctx, st.RunningOpID); err != nil {
			return nil, err
		}
	}
	frames, _, err := sandbox.NewEventLogReader(sb.Host, sb.Port, sb.Password).Raw(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: read event log after reattach: %v", errSandboxSetup, err)
	}
	first, ok := sandbox.FirstTurnFrames(frames)
	if !ok {
		return nil, nil
	}
	return acp.ReplayTurn(first)
}

func removeHome(home string) {
	if home != "" {
		_ = os.RemoveAll(home)
	}
}

func (c *acpProvider) spec(req NodeReq) (sandbox.Spec, error) {

	env := map[string]string{}

	// Secrets come only from workspace credentials: platform sandbox.env and
	// Agent/shared env never supply envauth.SecretEnvKeys.
	for k, v := range c.opts.Env {
		if envauth.IsSecretEnvKey(k) {
			log.Warn().Str("key", k).Msg("dropped secret key from platform sandbox.env; configure it in workspace credentials")
			continue
		}
		env[k] = v
	}
	profile := models.AgentProfile(req.Config)
	agentCfg := c.effectiveAgent(req)
	vars := c.mcpVars(req)

	for k, v := range envauth.StripSecretEnvKeys(agentCfg.Env) {
		env[k] = substVars(v, vars)
	}
	creds := map[string]string{}
	protectedCredentialKeys := map[string]struct{}{}
	if c.opts.CredentialKeys != nil {
		protectedCredentialKeys = c.opts.CredentialKeys()
	}
	// Run-scoped StartRun snapshot overlays shared + Agent for user-可控 keys.
	// Empty string values intentionally override. Must stay before mergeAuthEnv /
	// mcpVars / CONFIG_ROOT / ApplyPasswords so reserved platform keys win later.
	if c.opts.RunSandboxEnvForRun != nil && req.RunID != "" {
		for _, e := range c.opts.RunSandboxEnvForRun(req.RunID) {
			k := strings.TrimSpace(e.Key)
			if k == "" || envauth.IsSecretEnvKey(k) {
				continue
			}
			if _, protected := protectedCredentialKeys[k]; protected {
				continue
			}
			env[k] = e.Value
		}
	}
	ApplyCredentialBindings(env, creds, agentCfg.CredentialBindings, c.opts.SelectedCredential)
	gitCredentials, err := ApplyGitBindings(env, creds, agentCfg.GitBindings, c.opts.SelectedCredential)
	if err != nil {
		return sandbox.Spec{}, err
	}
	if err := ApplyModelBinding(env, string(c.backend), agentCfg.ModelBinding, c.opts.ModelBinding); err != nil {
		return sandbox.Spec{}, err
	}
	// Capture the login file before PrepareAuthEnv strips it from the sandbox env.
	codexLogin := CodexLoginFileFromEnv(c.backend, env)
	// Align auth gate with buildConfigHome BaseWorkDirSrc (workspace-shared workspace).
	merged, err := PrepareBoundAuth(c.backend, env, c.workDir(profile), c.sharedWorkDir(req))
	if err != nil {
		return sandbox.Spec{}, err
	}
	env = merged
	env["AGENT_PROVIDER"] = string(c.backend)
	applyPreviewEnv(env, req.Caps)

	for k, v := range vars {
		if strings.HasPrefix(k, "vars.") || v == "" {
			continue
		}
		if IsGitAuthorizationEnvKey(k) {
			continue
		}
		env[k] = v
	}

	layout := c.agentLayout(profile, agentCfg)
	env["CONFIG_ROOT"] = layout.ConfigRoot
	if c.backend == BackendCodex {
		env["CODEX_HOME"] = layout.ConfigRoot
	}

	sandbox.ApplyPasswords(env, req.Token)
	home, err := c.buildConfigHome(req, env)
	if err != nil {
		return sandbox.Spec{}, fmt.Errorf("build backend config home: %w", err)
	}
	if err := InstallBoundCodexLoginFile(c.backend, home, codexLogin, env); err != nil {
		return sandbox.Spec{}, err
	}
	if c.backend == BackendCodex && strings.TrimSpace(codexLogin) != "" {
		c.rememberCodexLogin(codexLoginKey(req), codexLogin, CodexAuthPath(layout.ConfigRoot), ModelCredentialIDForWriteBack(env))
	}
	if c.backend == BackendCodex && env[EnvModelConnection] != "" {
		codexConnectionRuns.Store(c.backendKey(codexLoginKey(req)), true)
	}
	spec := sandbox.Spec{
		GitBindingsConfigured: true,
		GitCredentials:        gitCredentials,
		Image:                 resolveProviderImage(c.opts, c.backend),
		Env:                   env,
		ConfigHome:            home,
		ConfigRoot:            layout.ConfigRoot,
		WorkspaceDir:          layout.WorkspaceDir,
	}
	return spec, nil
}

// applyPreviewEnv asks the gateway to 1:1-map a PREVIEW_PORT for Agents
// granted set_preview: previews are IP-direct, with the pick script injected
// by the in-sandbox proxy at a same-origin path. Those Agents also get the
// headed desktop browser and chrome-devtools MCP on it, so the VNC preview
// shows the Agent's own screen; an explicit VNC_PREVIEW / BROWSER_MCP wins.
func applyPreviewEnv(env map[string]string, caps *models.AgentCapabilities) {
	if env == nil || !caps.CanPreview() {
		return
	}
	env["PREVIEW_DIRECT"] = "1"
	env["PREVIEW_AUTO_INJECT"] = "1"
	env["PREVIEW_PICK_SCRIPT_URL"] = "/__grasp/preview-pick.js"
	for _, k := range []string{"VNC_PREVIEW", "BROWSER_MCP"} {
		if _, set := env[k]; !set {
			env[k] = "1"
		}
	}
}

// settleCodexLogin writes a changed auth.json back to the same workspace credential
// after a Codex run. A login rejection, an unreadable sandbox, or an unchanged
// file leaves the previous credential in place. The file body is not logged.
func (c *acpProvider) settleCodexLogin(req NodeReq, sb *sandbox.Sandbox, runErr error, events []models.AcpEvent) error {
	if c.backend != BackendCodex {
		c.forgetCodexLogin(codexLoginKey(req))
		return runErr
	}
	if _, connection := codexConnectionRuns.LoadAndDelete(c.backendKey(codexLoginKey(req))); connection {
		c.forgetCodexLogin(codexLoginKey(req))
		return runErr
	}
	state, ok := c.takeCodexLogin(codexLoginKey(req))
	rejected := CodexAuthRejected(runErr, events)
	if rejected && runErr != nil {
		runErr = asCodexLoginErr(runErr)
	}
	if !ok || sb == nil {
		return runErr
	}
	body, readErr := sb.ReadFile(context.Background(), state.path)
	next, write := ShouldWriteBackCodexLogin(readErr, string(body), state.body, rejected)
	if !write {
		if readErr != nil {
			log.Warn().Err(readErr).Str("run", req.RunID).Str("node", req.NodeID).Msg("codex login writeback skipped; sandbox file unreadable")
		}
		return runErr
	}
	if c.opts.WriteBackCodexLoginFile == nil || state.credentialID == "" {
		return runErr
	}
	if err := c.opts.WriteBackCodexLoginFile(state.credentialID, next); err != nil {
		log.Warn().Err(err).Str("run", req.RunID).Msg("codex login writeback failed")
	}
	return runErr
}
