package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// sandboxManager is the sandbox-creation seam the provider depends on. The
// real backend is *sandbox.Manager (Docker); tests inject a fake that returns
// sandboxes wired to an in-process fake ACP bridge, so the provider's retry /
// idle / react-rehydrate / harvest logic is exercised without Docker.
type sandboxManager interface {
	Create(ctx context.Context, spec sandbox.Spec) (*sandbox.Sandbox, error)
}

// errSandboxSetup wraps a failure to acquire a working sandbox + ACP session
// (container create / ACP-ready wait / connect handshake). It is a retryable
// infrastructure fault: a fresh attempt in a new container often succeeds.
var errSandboxSetup = errors.New("sandbox setup failed")

// isRetryableSandboxErr reports whether err is a transient sandbox/ACP fault
// worth retrying in a fresh container: setup failures, a connection that
// dropped mid-turn, an idle (stuck) turn, or Cursor CLI transport/reachability
// faults. Agent-reported logic errors, the hard chat deadline
// (DeadlineExceeded), and contract misses are NOT retryable — re-running them
// would just waste a sandbox and likely fail identically.
func isRetryableSandboxErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errSandboxSetup) ||
		errors.Is(err, sandbox.ErrConnClosed) ||
		errors.Is(err, sandbox.ErrChatIdle) ||
		isTransientACPTransportErr(err)
}

// isTransientACPTransportErr matches Cursor CLI / TLS reachability faults that
// surface as agent-process exit errors (not typed ErrConnClosed). A fresh
// sandbox often recovers once the upstream blip clears.
func isTransientACPTransportErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Failed to reach the Cursor API") ||
		strings.Contains(msg, "Client network socket disconnected before secure TLS connection was established") ||
		strings.Contains(msg, "ECONNRESET") ||
		strings.Contains(msg, "ETIMEDOUT") ||
		strings.Contains(msg, "ENETUNREACH")
}

// isChatTimeoutErr reports whether err is the per-turn chat deadline (context
// deadline exceeded). Used to distinguish timeout truncation from contract misses
// in nudge re-prompt paths.
func isChatTimeoutErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// isTurnTimeoutErr reports whether a chat ended because some timeout cut the
// turn: the per-turn deadline, the platform idle window or the sandbox bridge
// watchdog (ErrSandboxTurnTimeout wraps ErrChatIdle).
func isTurnTimeoutErr(err error) bool {
	return isChatTimeoutErr(err) || errors.Is(err, sandbox.ErrChatIdle)
}

// acpProvider is the ACP + Docker sandbox backend. Each agent/react node runs
// in a fresh container launched from the preset image
// (universal-sandbox:local). The in-container ACP agent is
// driven via the acp-bridge WebSocket bridge (ACP JSON-RPC). Declared produces
// are harvested from the container workspace and written through the run-scoped
// artifact-store MCP, satisfying the produces contract the engine enforces.
//
// Each node gets a per-node config-home mount (rules/skills + mcp.json) built
// from the Agent config. The run-scoped artifact-store MCP is not
// auto-injected: an Agent opts in by convention (an MCP named "artifact-store"),
// and the platform fills its run-scoped URL + token so artifacts stay isolated
// per run.
type acpProvider struct {
	host    *mcp.Host
	opts    Options
	mgr     sandboxManager
	backend AcpBackend

	// registry, when set, records each per-run node sandbox in the platform
	// sandbox store so it shows up in the sandbox UI while the node runs.
	registry SandboxRegistry

	// emit, when set by the engine, publishes a node's in-progress ACP events
	// (the same []models.AcpEvent shape that is persisted on completion) so the
	// run detail UI can show a live agent preview while the turn runs. busy is
	// the authoritative queue_state.busy flag (true while the agent is actively
	// processing the turn) used to drive the running/idle indicator.
	emit func(runID, nodeID string, events []models.AcpEvent, busy bool)

	mu       sync.Mutex
	sessions map[string]*reactSession    // runID|nodeID -> live react session
	live     map[string]*sandbox.Sandbox // runID|nodeID -> in-flight sandbox (for live event-log reads)
	// inflightACP tracks the ACP client for in-flight agent turns (not parked
	// in sessions). AbortRun closes these so Cancel-during-agent unblocks
	// streamChat instead of waiting out ChatTimeout.
	inflightACP map[string]*sandbox.ACPClient
	// timeline holds platform-side ACP event snapshots while a node sandbox is
	// live. nodeEvents reads this first; cold FetchEventLog is fallback only.
	timeline *acpTimelineStore
}

// streamChat runs one turn (prompt + optional image attachments), streaming
// incremental events to the sink when one is configured; otherwise it falls
// back to a single blocking aggregation.
func (c *acpProvider) streamChat(ctx context.Context, acp *sandbox.ACPClient, req NodeReq, prompt string, images []models.PromptImage) (*sandbox.ChatResult, error) {
	if c.emit == nil {
		return acp.ChatStructured(ctx, prompt, images)
	}
	return acp.ChatStreamResult(ctx, prompt, images, func(r *sandbox.ChatResult) {

		busy := true
		if r.BusySet {
			busy = r.Busy
		}
		events := chatResultToEvents(r)
		if c.timeline != nil {
			// Absolute current-turn snapshot from streamChat — always replace so
			// a fresh turn cannot be blocked by a longer prior-turn photo.
			c.timeline.replace(req.RunID, req.NodeID, events)
		}
		c.emit(req.RunID, req.NodeID, events, busy)
	})
}

// absorbChat folds one turn's prompt_done.usage into the StateRun-scoped
// accumulator. Only per-turn ChatResult.Usage is used — never session
// CumulativeUsage — so cross-node session reuse cannot inflate a later node.
func absorbChat(usage **models.TokenUsage, byModel *models.TokenUsageByModel, events *[]models.AcpEvent, res *sandbox.ChatResult) {
	if res == nil {
		return
	}
	if events != nil {
		*events = append(*events, chatResultToEvents(res)...)
	}
	if usage != nil {
		*usage = models.AddTokenUsage(*usage, res.Usage)
	}
	if byModel != nil {
		*byModel = models.AddTokenUsageByModel(*byModel, res.UsageByModel)
	}
}

// reactSession keeps a sandbox + ACP connection alive across the human
// think-time of a multi-turn react dialogue (open → reply → … → done).
type reactSession struct {
	sb   *sandbox.Sandbox
	acp  *sandbox.ACPClient
	home string // temp /root/.cursor host dir to clean up
}

func newBaseACPProvider(host *mcp.Host, opts Options, backend AcpBackend) ExecProvider {
	backend = NormalizeBackend(string(backend))
	image := resolveProviderImage(opts, backend)
	gw := sandbox.NewGatewayClient(opts.GatewayURL, opts.GatewayAPIKey)
	mgr := sandbox.NewManager(gw, sandbox.ManagerOptions{
		Image:           image,
		WorkspaceDir:    "/root/workspace",
		InstallHelpers:  true,
		InjectStore:     opts.InjectStore,
		InjectAdvertise: opts.MCPEndpoint,
		CreateTimeout:   opts.SandboxCreateTimeout,
		Blobs:           opts.Blobs,
	})
	log.Info().Str("image", mgr.Image).Str("gateway", opts.GatewayURL).Str("acpBackend", string(backend)).
		Str("bridge", AgentRuntimeLabel(backend)).Msg("sandbox exec provider ready")
	return &acpProvider{host: host, opts: opts, mgr: mgr, backend: backend,
		sessions: map[string]*reactSession{}, live: map[string]*sandbox.Sandbox{},
		inflightACP: map[string]*sandbox.ACPClient{}, timeline: newAcpTimelineStore()}
}

// resolveProviderImage picks the sandbox image for one acpBackend.
func resolveProviderImage(opts Options, backend AcpBackend) string {
	if img := strings.TrimSpace(opts.SandboxImage); img != "" {
		return img
	}
	b := string(NormalizeBackend(string(backend)))
	if opts.SandboxImages != nil {
		if img := strings.TrimSpace(opts.SandboxImages[b]); img != "" {
			return img
		}
	}
	return config.DefaultSandboxImage(b)
}

func (c *acpProvider) Name() string {
	if c.backend == "" {
		return string(BackendCursor)
	}
	return string(c.backend)
}

func (c *acpProvider) chatTimeout() time.Duration {
	if c.opts.ChatTimeout > 0 {
		return c.opts.ChatTimeout
	}
	return 10 * time.Minute
}

// nodeChatTimeout is the hard per-turn deadline for a node, honoring a per-node
// override before falling back to the global budget. Lets a heavy node (e.g.
// implement) get more headroom than a quick research. Two override keys are
// accepted: the editor card field `timeout` (minutes) and the legacy
// `chat_timeout` (seconds); `chat_timeout` wins when both are set.
// Approve with neither key defaults to 30 minutes (not the global 10).
func (c *acpProvider) nodeChatTimeout(req NodeReq) time.Duration {
	if v, ok := toInt(req.Config["chat_timeout"]); ok && v > 0 {
		return time.Duration(v) * time.Second
	}
	if v, ok := toInt(req.Config["timeout"]); ok && v > 0 {
		return time.Duration(v) * time.Minute
	}
	if nodereg.IsGrasp(req.NodeType) {
		return 30 * time.Minute
	}
	return c.chatTimeout()
}

// sandboxAttempts is the total number of node attempts (>=1) before a retryable
// sandbox fault gives up and the node fails for real.
func (c *acpProvider) sandboxAttempts() int {
	if c.opts.SandboxMaxAttempts > 1 {
		return c.opts.SandboxMaxAttempts
	}
	return 1
}

// backoff waits before the next sandbox retry (exponential from the configured
// base, capped at 30s), aborting early if ctx is cancelled. Returns false when
// ctx ended so the caller stops retrying.
func (c *acpProvider) backoff(ctx context.Context, attempt int) bool {
	base := c.opts.SandboxRetryBackoff
	if base <= 0 {
		base = 2 * time.Second
	}
	d := base << (attempt - 1)
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// emitRetryNotice logs a retryable sandbox fault and, when a live event sink is
// wired, pushes a visible message into the node's event stream so the retry is
// observable in the execution log (without creating a separate execution row).
func (c *acpProvider) emitRetryNotice(req NodeReq, attempt, max int, err error) {
	log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
		Int("attempt", attempt).Int("max", max).Err(err).
		Msg("retryable sandbox fault; retrying in a fresh sandbox")
	if c.emit != nil {
		c.emit(req.RunID, req.NodeID, []models.AcpEvent{{
			Kind:  "message",
			Title: "沙箱异常,自动重试",
			Text:  fmt.Sprintf("第 %d/%d 次尝试遇到沙箱/ACP 故障(%s),正在换新沙箱重试…", attempt, max, err.Error()),
		}}, true)
	}
}
