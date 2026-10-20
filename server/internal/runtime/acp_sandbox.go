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

// sandboxDestroyer is the manager capability to remove a container by its
// gateway id (sandbox.Manager.DestroyByName), reporting failure.
type sandboxDestroyer interface {
	DestroyByName(ctx context.Context, id string) error
}

// sandboxDestroyTimeout bounds one container destroy call.
var sandboxDestroyTimeout = 2 * time.Minute

// destroyContainer removes the container named id (sb.Name when id is empty)
// through mgr.DestroyByName. Managers without that capability (test fakes)
// fall back to the handle's best-effort Destroy.
func (c *acpProvider) destroyContainer(sb *sandbox.Sandbox, id string) error {
	if id == "" && sb != nil {
		id = sb.Name
	}
	if d, ok := c.mgr.(sandboxDestroyer); ok {
		if id == "" {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), sandboxDestroyTimeout)
		defer cancel()
		return d.DestroyByName(ctx, id)
	}
	if sb != nil {
		sb.Destroy(context.Background())
	}
	return nil
}

// destroyAndForget is the one teardown order for run containers: destroy the
// container first, and only once that succeeded drop the platform row
// (rowName, the placeholder until registerRunSandbox adopted the gateway id)
// and the ConfigHome. A failed destroy keeps the row and home and hands the
// row to the registry's retry path, so nothing is left behind untracked.
func (c *acpProvider) destroyAndForget(rowName string, sb *sandbox.Sandbox, home string) bool {
	id := ""
	if sb != nil {
		id = sb.Name
	}
	if id == "" {
		id = rowName
	}
	if err := c.destroyContainer(sb, id); err != nil {
		log.Warn().Err(err).Str("sandbox", id).Msg("run sandbox destroy failed; kept for retry")
		if r, ok := c.registry.(RunSandboxDestroyRetrier); ok {
			r.MarkRunSandboxDestroyFailed(rowName, id, err)
		}
		return false
	}
	c.deregisterRunSandbox(rowName)
	removeHome(home)
	return true
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
	c.destroyAndForget(sb.Name, sb, home)
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
			c.destroyAndForget(sb.Name, sb, sharedHome)
		}
		nl.release(c.coordinator)
		return events
	}
	if sb == nil {
		removeHome(home)
		return events
	}
	c.destroyAndForget(sb.Name, sb, home)
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
		c.destroyAndForget(placeholder, sb, home)
		return nil, layeredSpec{}, fmt.Errorf("%w: acp not ready: %v", errSandboxSetup, err)
	}
	ls.placeholder = placeholder
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
		created, err := acp.CreateChat(ctx, nodeChatTitle(req), model)
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
		home := created.spec.ConfigHome
		return sharedCreateResult{sb: sb, home: home, fingerprint: created.fingerprint, generation: generation,
			discard: func() {
				// Never adopted: the platform row still carries the placeholder.
				c.destroyAndForget(created.placeholder, sb, home)
			}}, nil
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
	if req.reenterShared {
		if err := c.settleStaleNodeChats(ctx, req, lease.sb); err != nil {
			c.coordinator.release(lease)
			c.forgetCodexLogin(codexLoginKey(req))
			return nil, nil, "", err
		}
	}
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
	// AttachNoEnsure: Attach would push a newer runtime bundle and restart an
	// idle bridge, dropping the chats of a turn still running. Neither fills
	// the bridge password or ConfigRoot, which come from the platform record.
	if attacher, ok := c.mgr.(sandboxAttacher); ok {
		if sb, err := attacher.AttachNoEnsure(ctx, info.Name); err == nil {
			sb.Password = info.Token
			if sb.Password == "" {
				sb.Password = req.Token
			}
			sb.ConfigRoot = c.agentLayout(models.AgentProfile(req.Config), c.effectiveAgent(req)).ConfigRoot
			if werr := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 30*time.Second); werr == nil {
				return sharedCreateResult{sb: sb, home: info.HomeDir, fingerprint: info.EnvFingerprint, generation: prev}, true, prev
			}
		}
	}
	if lifecycle, ok := c.registry.(RunSandboxLifecycle); ok {
		lifecycle.MarkRunSandboxReplacement(req.RunID, prev+1, sharedReplacementReason)
	}
	// Unusable (Attach failed or the bridge never answered): destroy by name
	// so the old generation cannot outlive its row.
	c.destroyAndForget(info.Name, nil, info.HomeDir)
	return res, false, prev + 1
}

// nodeChatTitle names a node's bridge chat in the run's shared container.
func nodeChatTitle(req NodeReq) string {
	return "run/" + safeSessionPart(req.RunID) + "/" + safeSessionPart(req.NodeID)
}

// staleTurnCancelWait bounds the graceful cancel of a pre-restart turn before
// its chat is deleted (which stops the agent regardless).
var staleTurnCancelWait = 10 * time.Second

// settleStaleNodeChats stops what a previous control-plane process left on
// this node's chat in the shared container before the node's prompt is sent
// again: the bridge keeps chats across WebSocket loss, so the old turn would
// otherwise keep running next to the new one in the same workspace. Each
// matching chat's queue_state is read and its running turn cancelled, then
// the chat is deleted. The new chat cannot adopt the old turn's result (its
// events are not on the default chat #823's reattach reads), so the main
// prompt is resent on a fresh chat afterwards.
func (c *acpProvider) settleStaleNodeChats(ctx context.Context, req NodeReq, sb *sandbox.Sandbox) error {
	title := nodeChatTitle(req)
	chats, err := sandbox.ListChats(ctx, sb.Host, sb.Port, sb.Password)
	if err != nil {
		return fmt.Errorf("%w: list chats before re-entry: %v", errSandboxSetup, err)
	}
	for _, ch := range chats {
		if ch.Title != title || ch.ID == "" {
			continue
		}
		if ch.Busy {
			c.cancelStaleTurn(ctx, req, sb, ch.ID)
		}
		if err := sandbox.DeleteChat(ctx, sb.Host, sb.Port, sb.Password, ch.ID); err != nil {
			return fmt.Errorf("%w: close pre-restart chat %s: %v", errSandboxSetup, ch.ID, err)
		}
		log.Info().Str("run", req.RunID).Str("node", req.NodeID).Str("chat", ch.ID).
			Msg("restart: closed the node's pre-restart chat in the shared sandbox")
	}
	return nil
}

// cancelStaleTurn connects to chatID and cancels the turn its queue_state
// reports running. Best effort: deleting the chat stops it in any case.
func (c *acpProvider) cancelStaleTurn(ctx context.Context, req NodeReq, sb *sandbox.Sandbox, chatID string) {
	acp := sb.ACP().WithSession(sb.WorkspaceDir, c.mcpServers(req)).WithChat(chatID)
	if err := acp.Connect(ctx); err != nil {
		return
	}
	defer acp.Close()
	st, ok := acp.WaitBridgeKnown(ctx, reattachKnownWait)
	if !ok || st.RunningOpID == "" {
		return
	}
	if !acp.CancelTurnAndWait(st.RunningOpID, staleTurnCancelWait) {
		log.Warn().Str("run", req.RunID).Str("node", req.NodeID).Str("op", st.RunningOpID).
			Msg("restart: pre-restart turn did not confirm cancel; deleting its chat")
	}
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
		// The row was never adopted by registerRunSandbox, so it is still
		// keyed by the placeholder (as in createSandbox's failure paths).
		c.destroyAndForget(ls.placeholder, sb, home)
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
		c.destroyAndForget(name, nil, "")
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
		c.destroyAndForget(name, sb, "")
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
	// placeholder is the creating-row name createSandbox registered before
	// the gateway assigned an id. Until registerRunSandbox adopts the real
	// name, the platform row is still keyed by it.
	placeholder string
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
	ApplyCredentialBindings(env, projectCreds, c.projectIDForReq(req), agentCfg.CredentialBindings, c.opts.SelectedCredentialForProject)
	markChangedKeys(agentEnvKeys, beforeCred, env)
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
		out.fingerprint = sharedSandboxFingerprint(c.backend, out.spec, homeSpec, sharedCredentialIDs(c.backend, agentCfg, agentEnvKeys))
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
