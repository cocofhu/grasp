package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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

const (
	// sandboxScopeRun marks the run-level container shared by compatible nodes.
	sandboxScopeRun = "run"
	// sandboxScopeNode marks a node-private container (no coordinator, or a
	// node whose configuration does not match the run's shared container).
	sandboxScopeNode = "node"
)

const sharedReplacementReason = "共享沙箱不可恢复，创建替代代次"

// beginRunSandbox records a "creating" placeholder row for a node sandbox before
// the (slow) gateway provisioning, so it shows up in the sandbox list / node
// live log as "starting" instead of a 404. No-op when the registry is absent or
// does not implement RunSandboxBeginner (e.g. test fakes).
func (c *acpProvider) beginRunSandbox(req NodeReq, name, home, scope string, generation int) {
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
		Scope:        scope,
		Generation:   generation,
	})
}

// registerRunSandbox records a live sandbox in the platform store. No-op when
// no registry is wired.
func (c *acpProvider) registerRunSandbox(req NodeReq, sb *sandbox.Sandbox, home, scope string, generation int, replacementReason, fingerprint string) {
	if c.registry == nil || sb == nil {
		return
	}
	c.registry.RegisterRunSandbox(RunSandboxInfo{
		Name:              sb.Name,
		Profile:           models.AgentProfile(req.Config),
		RunID:             req.RunID,
		WorkflowID:        req.WorkflowID,
		WorkflowName:      req.WorkflowName,
		NodeID:            req.NodeID,
		Host:              sb.Host,
		ACPPort:           sb.Port,
		CodeServerPort:    sb.CodeServerPort,
		RepoURL:           firstRepoURL(req),
		HomeDir:           home,
		Token:             req.Token,
		Scope:             scope,
		Generation:        generation,
		ReplacementReason: replacementReason,
		EnvFingerprint:    fingerprint,
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
// is leaked. A node in the run's shared container only releases its lease;
// AbortRun performs the single terminal teardown.
func (c *acpProvider) retireRunSandbox(sb *sandbox.Sandbox, acp *sandbox.ACPClient, home string) {
	nl := c.takeNodeLease(acp)
	if acp != nil {
		acp.CloseOwnedChat(context.Background())
		acp.Close()
	}
	if nl != nil {
		nl.release(c.coordinator)
		return
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

// sharedSandboxHealthy probes a shared container before a node failure is
// allowed to destroy it. Overridden in tests.
var sharedSandboxHealthy = func(sb *sandbox.Sandbox) bool {
	if sb == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 5*time.Second) == nil
}

// discardSandbox tears down a broken sandbox between retries: close the ACP
// session, drop its registry/live bookkeeping, destroy the container, and
// remove its cursor-home. Unlike retireRunSandbox it does NOT keep the
// container for debugging — a faulted sandbox has no value and would leak.
// Best-effort snapshotEvents runs before destroy so ACP events are not lost
// when the container is recycled.
//
// In the run's shared container a node fault (idle, stuck, dropped socket) is
// usually confined to that node's chat, while the workspace holds earlier
// nodes' work. The container is therefore kept and only the lease released
// unless the bridge no longer answers.
func (c *acpProvider) discardSandbox(ctx context.Context, req NodeReq, sb *sandbox.Sandbox, acp *sandbox.ACPClient, home string, fallback []models.AcpEvent) []models.AcpEvent {
	events := c.snapshotEvents(ctx, sb, fallback)
	c.deregisterLive(req)
	nl := c.takeNodeLease(acp)
	if acp != nil {
		acp.CloseOwnedChat(context.Background())
		acp.Close()
	}
	if nl != nil {
		if sharedSandboxHealthy(sb) {
			nl.release(c.coordinator)
			return events
		}
		if lifecycle, ok := c.registry.(RunSandboxLifecycle); ok {
			lifecycle.MarkRunSandboxReplacement(req.RunID, c.coordinator.nextGeneration(req.RunID), sharedReplacementReason)
		}
		if sharedHome, removed := c.coordinator.discard(req.RunID, sb); removed {
			c.deregisterRunSandbox(sb.Name)
			sb.Destroy(context.Background())
			removeHome(sharedHome)
		}
		nl.release(c.coordinator)
		return events
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

// createSandbox provisions one container and leaves ACP session creation to the
// caller. Keeping these phases separate lets a run-level coordinator reuse the
// container while every node still receives a fresh ACP chat/tab.
func (c *acpProvider) createSandbox(ctx context.Context, req NodeReq, shared bool, generation int) (*sandbox.Sandbox, layeredSpec, error) {
	ls, err := c.specLayered(req, shared, true)
	if err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		return nil, layeredSpec{}, fmt.Errorf("%w: %v", errSandboxConfig, err)
	}
	spec := ls.spec
	home := spec.ConfigHome
	scope := sandboxScopeNode
	if shared {
		scope = sandboxScopeRun
	}

	placeholder := sandbox.NewContainerName()
	spec.Name = placeholder
	c.beginRunSandbox(req, placeholder, home, scope, generation)
	sb, err := c.mgr.Create(ctx, spec)
	if err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		c.deregisterRunSandbox(placeholder)
		removeHome(home)
		return nil, layeredSpec{}, fmt.Errorf("%w: create sandbox: %v", errSandboxSetup, err)
	}
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		c.deregisterRunSandbox(placeholder)
		sb.Destroy(context.Background())
		removeHome(home)
		return nil, layeredSpec{}, fmt.Errorf("%w: acp not ready: %v", errSandboxSetup, err)
	}
	return sb, ls, nil
}

// connectSandbox opens the node's ACP session. nodeChat allocates a dedicated
// bridge chat (shared container) so transcripts and the per-chat model stay
// node-scoped; otherwise the container's default chat is used.
func (c *acpProvider) connectSandbox(ctx context.Context, req NodeReq, sb *sandbox.Sandbox, model string, nodeChat bool, overlay map[string]string) (*sandbox.ACPClient, error) {
	acp := sb.ACP().
		WithSession(sb.WorkspaceDir, c.mcpServers(req)).
		WithIdleTimeoutFunc(c.agentIdle).
		WithBridgeModel(model).
		WithEnv(sanitizeNodeEnvOverlay(overlay))
	if nodeChat {
		created, err := acp.CreateChat(ctx, "run/"+safeSessionPart(req.RunID)+"/"+safeSessionPart(req.NodeID), model)
		if err != nil {
			acp.CloseOwnedChat(context.Background())
			acp.Close()
			return nil, fmt.Errorf("%w: create node chat: %v", errSandboxSetup, err)
		}
		acp.WithChat(created)
	}
	if err := acp.Connect(ctx); err != nil {
		acp.CloseOwnedChat(context.Background())
		acp.Close()
		return nil, fmt.Errorf("%w: acp connect: %v", errSandboxSetup, err)
	}
	return acp, nil
}

func (c *acpProvider) openSandbox(ctx context.Context, req NodeReq) (*sandbox.Sandbox, *sandbox.ACPClient, string, error) {
	if c.coordinator != nil && strings.TrimSpace(req.RunID) != "" {
		sb, acp, home, err := c.openSharedSandbox(ctx, req)
		if !errors.Is(err, errSharedSandboxIncompatible) {
			return sb, acp, home, err
		}
		log.Info().Str("run", req.RunID).Str("node", req.NodeID).
			Msg("node backend/config differs from the run's shared sandbox; using a node-private sandbox")
	}
	return c.openPrivateSandbox(ctx, req)
}

// openSharedSandbox runs the node in the run's shared container under an
// exclusive lease. The lease is released by retireRunSandbox/discardSandbox
// after the node's ACP client closes, when a KeepAliveForReview session parks,
// or when a ReAct dialogue ends.
func (c *acpProvider) openSharedSandbox(ctx context.Context, req NodeReq) (*sandbox.Sandbox, *sandbox.ACPClient, string, error) {
	ls, err := c.specLayered(req, true, false)
	if err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		return nil, nil, "", fmt.Errorf("%w: %v", errSandboxConfig, err)
	}
	lease, err := c.coordinator.acquire(ctx, req.RunID, ls.fingerprint, func() (sharedCreateResult, error) {
		res, attached, generation := c.attachRunSandbox(ctx, req)
		if attached {
			return res, nil
		}
		if generation < 1 {
			generation = c.coordinator.currentGeneration(req.RunID)
		}
		sb, created, err := c.createSandbox(ctx, req, true, generation)
		if err != nil {
			return sharedCreateResult{}, err
		}
		return sharedCreateResult{sb: sb, home: created.spec.ConfigHome, fingerprint: created.fingerprint, generation: generation}, nil
	})
	if err != nil {
		if !errors.Is(err, errSharedSandboxIncompatible) {
			c.forgetCodexLogin(codexLoginKey(req))
		}
		return nil, nil, "", err
	}
	reason := ""
	if lease.generation > 1 {
		reason = sharedReplacementReason
	}
	c.registerRunSandbox(req, lease.sb, lease.home, sandboxScopeRun, lease.generation, reason, lease.fingerprint)
	acp, err := c.connectSandbox(ctx, req, lease.sb, ls.model, true, ls.overlay)
	if err != nil {
		// Connect/chat failures are node-scoped: release the lease so a
		// retry or later node can reuse the healthy run-level container.
		c.coordinator.release(lease)
		c.forgetCodexLogin(codexLoginKey(req))
		return nil, nil, "", err
	}
	c.trackNodeLease(acp, lease)
	return lease.sb, acp, lease.home, nil
}

// attachRunSandbox rehydrates the run's durable shared container after a
// server restart. An unrecoverable container is destroyed and its row dropped
// so it cannot leak; generation then names the replacement's generation.
func (c *acpProvider) attachRunSandbox(ctx context.Context, req NodeReq) (res sharedCreateResult, attached bool, generation int) {
	lookup, ok := c.registry.(RunSandboxLookup)
	if !ok {
		return res, false, 0
	}
	info, found := lookup.FindRunSandbox(req.RunID)
	if !found || info.Name == "" {
		return res, false, 0
	}
	prev := max(info.Generation, 1)
	if attacher, ok := c.mgr.(sandboxAttacher); ok {
		if sb, err := attacher.Attach(ctx, info.Name); err == nil {
			if werr := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 30*time.Second); werr == nil {
				return sharedCreateResult{sb: sb, home: info.HomeDir, fingerprint: info.EnvFingerprint, generation: prev}, true, prev
			}
			sb.Destroy(context.Background())
		}
	}
	if lifecycle, ok := c.registry.(RunSandboxLifecycle); ok {
		lifecycle.MarkRunSandboxReplacement(req.RunID, prev+1, sharedReplacementReason)
	}
	c.deregisterRunSandbox(info.Name)
	removeHome(info.HomeDir)
	return res, false, prev + 1
}

// sandboxAttacher is the optional manager capability to re-open a container
// by its gateway id.
type sandboxAttacher interface {
	Attach(context.Context, string) (*sandbox.Sandbox, error)
}

func (c *acpProvider) openPrivateSandbox(ctx context.Context, req NodeReq) (*sandbox.Sandbox, *sandbox.ACPClient, string, error) {
	sb, ls, err := c.createSandbox(ctx, req, false, 1)
	if err != nil {
		return nil, nil, "", err
	}
	home := ls.spec.ConfigHome
	acp, err := c.connectSandbox(ctx, req, sb, ls.model, false, nil)
	if err != nil {
		c.forgetCodexLogin(codexLoginKey(req))
		c.deregisterRunSandbox(sb.Name)
		sb.Destroy(context.Background())
		removeHome(home)
		return nil, nil, "", err
	}
	c.registerRunSandbox(req, sb, home, sandboxScopeNode, 1, "", "")
	return sb, acp, home, nil
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
	ls, err := c.specLayered(req, false, true)
	return ls.spec, err
}

// layeredSpec is a node's sandbox Spec split for the shared-container path.
type layeredSpec struct {
	spec sandbox.Spec
	// overlay carries node-scoped Agent env for the ACP connect (shared only).
	overlay map[string]string
	// model is the node's ACP_BRIDGE_MODEL, applied per bridge chat.
	model string
	// fingerprint digests every container-shaping input (shared only); nodes
	// reuse a shared container only when fingerprints are equal.
	fingerprint string
}

// specLayered builds the sandbox Spec. When shared is true, Spec.Env keeps only
// run-level + platform-reserved keys; overlay carries node-scoped Agent
// credentials for ACP connect injection. When shared is false the container is
// node-scoped so Spec.Env is the full merge and overlay is nil. materialize
// false skips building the ConfigHome directory (Spec.ConfigHome stays empty)
// for callers that only need the overlay / fingerprint of an existing container.
func (c *acpProvider) specLayered(req NodeReq, shared, materialize bool) (layeredSpec, error) {

	repos := resolveRepos(req)
	env := map[string]string{}
	agentEnvKeys := map[string]struct{}{}

	// Secrets come only from project credentials: platform sandbox.env and
	// Agent/shared env never supply envauth.SecretEnvKeys.
	for k, v := range c.opts.Env {
		if envauth.IsSecretEnvKey(k) {
			log.Warn().Str("key", k).Msg("dropped secret key from platform sandbox.env; configure it in project credentials")
			continue
		}
		env[k] = v
	}
	profile := models.AgentProfile(req.Config)
	agentCfg := c.effectiveAgent(req)
	vars := c.mcpVars(req)

	strippedAgentEnv := envauth.StripSecretEnvKeys(agentCfg.Env)
	markKeys(agentEnvKeys, strippedAgentEnv)
	for k, v := range strippedAgentEnv {
		env[k] = substVars(v, vars)
	}
	// Project credentials are the only secret source. They are resolved only
	// for the owning project and never copied into persisted Agent config.
	// Platform-reserved keys from mcpVars are re-applied below and still win.
	var projectCreds map[string]string
	if c.opts.ProjectCredentialsForProject != nil {
		projectCreds = c.opts.ProjectCredentialsForProject(c.projectIDForReq(req))
		for k, v := range projectCreds {
			k = strings.TrimSpace(k)
			if k != "" {
				env[k] = v
			}
		}
	}
	protectedCredentialKeys := map[string]struct{}{}
	if c.opts.ProjectCredentialKeysForProject != nil {
		protectedCredentialKeys = c.opts.ProjectCredentialKeysForProject(c.projectIDForReq(req))
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
			if _, protected := protectedCredentialKeys[k]; protected {
				continue
			}
			env[k] = e.Value
		}
	}
	// The selected model-vendor credential wins over a merged project env and
	// over the run snapshot. An empty selection injects nothing.
	if c.backend == BackendOpenCode && c.opts.OpenCodeCredentialForProject != nil {
		before := cloneStringMap(env)
		ApplyOpenCodeSelection(env, c.opts.OpenCodeCredentialForProject(c.projectIDForReq(req), agentCfg.OpenCodeCredentialID))
		markChangedKeys(agentEnvKeys, before, env)
	}
	beforeCred := cloneStringMap(env)
	ApplyAgentCredentialChoice(env, projectCreds, c.projectIDForReq(req), AgentCredentialChoice{
		Backend:              string(c.backend),
		GitCredentialType:    agentCfg.GitCredentialType,
		AiCredentialID:       agentCfg.AiCredentialID,
		GitCredentialID:      agentCfg.GitCredentialID,
		SshHostsCredentialID: agentCfg.SshHostsCredentialID,
	}, c.opts.SelectedCredentialForProject)
	markChangedKeys(agentEnvKeys, beforeCred, env)
	ApplyCredentialBindings(env, projectCreds, c.projectIDForReq(req), agentCfg.CredentialBindings, c.opts.SelectedCredentialForProject)
	// Capture the login file before PrepareAuthEnv strips it from the sandbox env.
	codexLogin := CodexLoginFileFromEnv(c.backend, env)
	// Align auth gate with buildConfigHome BaseWorkDirSrc (project-shared workspace).
	merged, err := PrepareAuthEnv(c.backend, env, c.workDir(profile), c.sharedWorkDir(req))
	if err != nil {
		return layeredSpec{}, err
	}
	markChangedKeys(agentEnvKeys, env, merged)
	env = merged
	env["AGENT_PROVIDER"] = string(c.backend)
	if err := RequireOpenCodePlaceholderKey(OpenCodeConfigForEnvWithCatalog(context.Background(), c.backend, env, c.opts.OpenCodeCatalog), env); err != nil {
		return layeredSpec{}, err
	}
	applyPreviewEnv(env, req.Caps)

	for k, v := range vars {
		// Node identity is available to MCP template substitution through
		// templateVars, but must not become a process-wide container variable:
		// inject it per node via the ACP session overlay instead.
		if strings.HasPrefix(k, "vars.") || v == "" {
			continue
		}
		if k == "GRASP_NODE_ID" {
			agentEnvKeys[k] = struct{}{}
			env[k] = v
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
	homeSpec := c.configHomeSpec(req, env)
	home := ""
	if materialize {
		home = c.buildConfigHome(req, env)
		if err := InstallCodexLoginFile(c.backend, home, codexLogin); err != nil {
			return layeredSpec{}, err
		}
	} else if c.backend == BackendCodex && strings.TrimSpace(codexLogin) == "" {
		return layeredSpec{}, ErrCodexLoginFileMissing
	}
	// Every node remembers its login, including nodes reusing a shared
	// container, so settleCodexLogin can write back a refreshed auth.json.
	if c.backend == BackendCodex && strings.TrimSpace(codexLogin) != "" {
		c.rememberCodexLogin(codexLoginKey(req), codexLogin, CodexAuthPath(layout.ConfigRoot), CredentialIDForWriteBack(c.projectIDForReq(req), envauth.EnvCodexAuthFile, codexLogin, agentCfg.AiCredentialID, agentCfg.CredentialBindings, c.opts.SelectedCredentialForProject, c.opts.InjectedCredentialID))
	}

	out := layeredSpec{model: strings.TrimSpace(env[EnvACPBridgeModel])}
	specEnv := env
	if shared {
		specEnv, out.overlay = partitionSharedSandboxEnv(env, agentEnvKeys)
	}

	out.spec = sandbox.Spec{
		Image:        resolveProviderImage(c.opts, c.backend),
		Env:          specEnv,
		ConfigHome:   home,
		ConfigRoot:   layout.ConfigRoot,
		WorkspaceDir: layout.WorkspaceDir,
	}
	// SSH comes only from project credentials.
	sandbox.ApplySSHCredentials(&out.spec, projectCreds["GIT_SSH_PRIVATE_KEY"], projectCreds["GIT_SSH_KNOWN_HOSTS"])
	if shared {
		out.fingerprint = sharedSandboxFingerprint(c.backend, out.spec, homeSpec, sharedCredentialIDs(c.backend, agentCfg))
	}
	return out, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func markChangedKeys(dst map[string]struct{}, before, after map[string]string) {
	if dst == nil {
		return
	}
	for k, v := range after {
		if before[k] != v {
			k = strings.TrimSpace(k)
			if k != "" {
				dst[k] = struct{}{}
			}
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			k = strings.TrimSpace(k)
			if k != "" {
				dst[k] = struct{}{}
			}
		}
	}
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

// settleCodexLogin writes a changed auth.json back to the same project credential
// after a Codex run. A login rejection, an unreadable sandbox, or an unchanged
// file leaves the previous credential in place. The file body is not logged.
func (c *acpProvider) settleCodexLogin(req NodeReq, sb *sandbox.Sandbox, runErr error, events []models.AcpEvent) error {
	if c.backend != BackendCodex {
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
	pid := c.projectIDForReq(req)
	if err := c.opts.WriteBackCodexLoginFile(pid, state.credentialID, next); err != nil {
		log.Warn().Err(err).Str("project", pid).Str("run", req.RunID).Msg("codex login writeback failed")
	}
	return runErr
}
