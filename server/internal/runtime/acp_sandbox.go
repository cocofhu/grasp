package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
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
		c.timeline.startIngest(req.RunID, req.NodeID, host, port)
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
	res, _, err := sandbox.FetchEventLogLastTurn(ctx, sb.Host, sb.Port)
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
	page, err := sandbox.FetchEventLogPage(ctx, sb.Host, sb.Port, cursor, limit)
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
	snap, _, err := sandbox.FetchEventLog(ctx, sb.Host, sb.Port)
	if err != nil || snap == nil {
		return fallback
	}
	if se := snap.AcpEvents(); len(se) > 0 {
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
// does not implement RunSandboxBeginner (e.g. test fakes → legacy behavior).
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
	spec, err := c.spec(req)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: %v", errSandboxSetup, err)
	}
	home := spec.ConfigHome

	placeholder := sandbox.NewContainerName()
	spec.Name = placeholder
	c.beginRunSandbox(req, placeholder, home)
	sb, err := c.mgr.Create(ctx, spec)
	if err != nil {
		c.deregisterRunSandbox(placeholder)
		removeHome(home)
		return nil, nil, "", fmt.Errorf("%w: create sandbox: %v", errSandboxSetup, err)
	}
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		c.deregisterRunSandbox(placeholder)
		sb.Destroy(context.Background())
		removeHome(home)
		return nil, nil, "", fmt.Errorf("%w: acp not ready: %v", errSandboxSetup, err)
	}
	acp := sb.ACP().
		WithSession(sb.WorkspaceDir, c.mcpServers(req)).
		WithIdleTimeout(c.opts.ChatIdleTimeout).
		WithBridgeModel(spec.Env["ACP_BRIDGE_MODEL"])
	if err := acp.Connect(ctx); err != nil {
		c.deregisterRunSandbox(placeholder)
		acp.Close()
		sb.Destroy(context.Background())
		removeHome(home)
		return nil, nil, "", fmt.Errorf("%w: acp connect: %v", errSandboxSetup, err)
	}
	c.registerRunSandbox(req, sb, home)
	return sb, acp, home, nil
}

func removeHome(home string) {
	if home != "" {
		_ = os.RemoveAll(home)
	}
}

func (c *acpProvider) spec(req NodeReq) (sandbox.Spec, error) {

	repos := resolveRepos(req)
	env := map[string]string{}

	for k, v := range c.opts.Env {
		if envauth.IsPlatformAuthEnvKey(k) {
			log.Warn().Str("key", k).Msg("dropped platform sandbox.env official CLI auth key; use GRASP_* on Agent or shared env")
			continue
		}
		env[k] = v
	}
	profile := models.AgentProfile(req.Config)
	agentCfg := c.effectiveAgent(req)
	vars := c.mcpVars(req)

	for k, v := range agentCfg.Env {
		env[k] = substVars(v, vars)
	}
	// Run-scoped StartRun snapshot overlays shared + Agent for user-可控 keys.
	// Empty string values intentionally override. Must stay before mergeAuthEnv /
	// mcpVars / CONFIG_ROOT / ApplyPasswords so reserved platform keys win later.
	if c.opts.RunSandboxEnvForRun != nil && req.RunID != "" {
		for _, e := range c.opts.RunSandboxEnvForRun(req.RunID) {
			k := strings.TrimSpace(e.Key)
			if k == "" {
				continue
			}
			env[k] = e.Value
		}
	}
	// Align auth gate with buildConfigHome BaseWorkDirSrc (project-shared workspace).
	merged, err := PrepareAuthEnv(c.backend, env, c.workDir(profile), c.sharedWorkDir(req))
	if err != nil {
		return sandbox.Spec{}, err
	}
	env = merged
	env["AGENT_PROVIDER"] = string(c.backend)
	if err := RequireOpenCodePlaceholderKey(OpenCodeConfigForEnvWithCatalog(context.Background(), c.backend, env, c.opts.OpenCodeCatalog), env); err != nil {
		return sandbox.Spec{}, err
	}
	applyAppPreviewEnv(env, req.NodeType, req.Config, c.opts.PublicAdvertise)

	for k, v := range vars {
		if strings.HasPrefix(k, "vars.") || v == "" {
			continue
		}
		env[k] = v
	}

	if len(repos) > 0 && env["GITLAB_URL"] == "" {
		url := repos[0].URL
		host := gitRepoHost(url)
		ghe := gitRepoHost(env["GITHUB_URL"])
		// Do not treat a GitHub / GHE clone URL as GITLAB_URL (would inject
		// oauth2:GITLAB_TOKEN@github.com when both tokens are configured).
		if host != "" && host != "github.com" && (ghe == "" || host != ghe) {
			if base := gitBaseURL(url); base != "" {
				env["GITLAB_URL"] = base
			}
		}
	}
	layout := c.agentLayout(profile, agentCfg)
	env["CONFIG_ROOT"] = layout.ConfigRoot
	if c.backend == BackendCodex {
		env["CODEX_HOME"] = layout.ConfigRoot
	}

	sandbox.ApplyPasswords(env, req.Token)
	spec := sandbox.Spec{
		Image:        resolveProviderImage(c.opts, c.backend),
		Env:          env,
		ConfigHome:   c.buildConfigHome(req, env),
		ConfigRoot:   layout.ConfigRoot,
		WorkspaceDir: layout.WorkspaceDir,
	}
	if c.backend == BackendCodex && spec.ConfigHome == "" {
		return sandbox.Spec{}, fmt.Errorf("Codex config home could not be prepared")
	}
	// SSH: meta literal preferred; env fallback already vars-expanded above.
	key := agentCfg.GitSshPrivateKey
	if strings.TrimSpace(key) == "" {
		key = env["GIT_SSH_PRIVATE_KEY"]
	}
	hosts := agentCfg.GitSshKnownHosts
	if strings.TrimSpace(hosts) == "" {
		hosts = env["GIT_SSH_KNOWN_HOSTS"]
	}
	sandbox.ApplySSHCredentials(&spec, key, hosts)
	if req.NodeType == "app_preview" {

		spec.Resources = &sandbox.GWResources{CPUCores: 2, MemoryMB: 8192, DiskGi: 40}
	}
	return spec, nil
}

// applyAppPreviewEnv sets VNC_PREVIEW by default unless the merged env already
// has an explicit off (0/false). When the node switch
// direct_preview is on, skip the VNC stack and ask the gateway to 1:1-map a
// PREVIEW_PORT instead (PREVIEW_DIRECT=1).
func applyAppPreviewEnv(env map[string]string, nodeType string, cfg map[string]any, publicAdvertise string) {
	if env == nil || !mcp.SetPreviewAllowed(nodeType) {
		return
	}
	if configTruthy(cfg["direct_preview"]) {
		env["PREVIEW_DIRECT"] = "1"
		if configDefaultOn(cfg["auto_inject"]) {
			env["PREVIEW_AUTO_INJECT"] = "1"
			// Same-origin path served by the in-sandbox injector. PublicAdvertise
			// is often http://localhost:8080, which the reviewer's browser cannot
			// load from an iframe at http://IP:PREVIEW_PORT/.
			env["PREVIEW_PICK_SCRIPT_URL"] = "/__grasp/preview-pick.js"
		} else {
			env["PREVIEW_AUTO_INJECT"] = "0"
			if u := previewPickScriptURL(publicAdvertise); u != "" {
				env["PREVIEW_PICK_SCRIPT_URL"] = u
			}
		}
		return
	}
	// Shared / Agent env can explicitly turn the stack off (first-install wizard).
	if envFlagOff(env["VNC_PREVIEW"]) {
		delete(env, "GRASP_VNC_PREVIEW")
		return
	}
	env["VNC_PREVIEW"] = "1"
	env["GRASP_VNC_PREVIEW"] = "1"
}

func envFlagOff(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "0" || s == "false" || s == "off" || s == "no"
}

func previewPickScriptURL(base string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	if b == "" {
		return ""
	}
	return b + "/preview-pick.js"
}
