// Package runtime executes agent/react nodes behind an ExecProvider
// abstraction. ProviderRegistry routes agent_profile.acpBackend to one of four
// ACP backends (cursor, claude_code, codebuddy, trae, opencode) sharing baseACPProvider
// logic. Production runs real CLI bridges in per-node Docker sandboxes; fake-
// bridge E2E tests in this package exercise the same path without live CLIs.
package runtime

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// Options configures the execution providers (mainly the sandbox backend).
type Options struct {
	// SandboxImage, when non-empty, forces one image for every acpBackend.
	SandboxImage string
	// SandboxImages maps acpBackend → image; used when SandboxImage is empty.
	SandboxImages map[string]string
	// GatewayURL is the sandbox-gateway control-plane base URL (e.g.
	// http://127.0.0.1:8899) approving calls to create/manage sandboxes.
	GatewayURL string
	// GatewayAPIKey is the optional bearer token for the gateway (empty when
	// the gateway runs with auth disabled).
	GatewayAPIKey string
	// Env is the vendor-neutral set of environment variables injected into
	// every sandbox (protocol config.env channel). For the reference image it
	// typically carries CURSOR_API_KEY; other images carry whatever they need.
	Env map[string]string
	// CursorAuthPath, if set, is mounted read-only into the reference image so
	// its cursor CLI reuses a host login. Reference-implementation only.
	CursorAuthPath string
	ChatTimeout    time.Duration
	// ChatIdleTimeout aborts a chat turn when no ACP event arrives within the
	// window (0 disables). Detects a stuck agent/sandbox without killing a
	// slow-but-productive turn that keeps streaming events.
	ChatIdleTimeout time.Duration
	// SandboxMaxAttempts caps how many times a node is (re)attempted when a
	// retryable sandbox/ACP fault occurs (create/ready/connect/mid-turn crash).
	// <=1 disables retry (single attempt).
	SandboxMaxAttempts int
	// SandboxRetryBackoff is the base backoff between sandbox retries; the wait
	// grows exponentially (base * 2^(attempt-1)) and is capped internally.
	SandboxRetryBackoff time.Duration
	// SandboxCreateTimeout bounds how long Create waits for the gateway to report
	// a running sandbox. Must stay >= the gateway's FinalizeTimeout so approving
	// does not give up during a slow cold-start. 0 → Manager default.
	SandboxCreateTimeout time.Duration
	// MCPEndpoint is the artifact-store MCP base URL reachable from inside
	// a sandbox container (e.g. http://host.docker.internal:8090).
	MCPEndpoint string
	// InjectStore holds short-lived ConfigHome .tgz for gateway bundleUrl inject.
	InjectStore *sandbox.BundleStore
	// Blobs resolves blob:{id} attachments for ACP chat turns.
	Blobs blob.Store
	// ProfilesRoot is where agent_profile rules live (<root>/<profile>/rules.md),
	// copied into the per-node /root/.cursor mount.
	ProfilesRoot string
	// PlatformRulesRoot is where global platform-rule defaults are stored.
	PlatformRulesRoot string
	// SharedAgentForProject, when set, returns the current project's shared
	// Agent baseline used as the extend layer before overlaying the Agent.
	// Wired for workflow Run sandboxes and project-context chat tests only —
	// Agent Studio interactive tests must leave it unused.
	SharedAgentForProject func(projectID string) SharedAgentView
	// ProjectIDForWorkflow resolves workflow → owning project for extend.
	ProjectIDForWorkflow func(workflowID string) string
	// RunSandboxEnvForRun, when set, returns the immutable StartRun sandbox env
	// snapshot for this Run (plaintext). Applied in acpProvider.spec after Agent
	// env and before mergeAuthEnv / mcpVars / passwords / CONFIG_ROOT so run-level
	// keys overlay shared+Agent but never win over platform reserved write-backs.
	// Only pipeline openSandbox paths should wire this; Agent Studio / interactive
	// test / PM chat / Cron must leave it nil so run env does not leak.
	// KeepAliveForReview reuses an already-created sandbox and therefore keeps
	// the environ from create time (no hot update); a later new sandbox for the
	// same Run re-reads this snapshot.
	RunSandboxEnvForRun func(runID string) []models.EnvEntry
	// PublicAdvertise is the browser-facing base URL (preview proxy / pick.js).
	PublicAdvertise string
	// OpenCodeCatalog tells whether OpenCode resolves a provider id natively, so
	// a gateway absent from its catalog can be declared with an adapter instead
	// of failing at run time. Nil keeps the conservative `custom`-only behavior.
	OpenCodeCatalog OpenCodeCatalog
}

// SharedAgentView is the runtime-facing slice of project shared Agent config
// needed for extend→overlay (avoids importing services into every call site).
type SharedAgentView struct {
	AcpBackend       string
	GitSshKnownHosts string
	GitSshPrivateKey string
	MCP              []SharedMCPView
	Env              map[string]string
	Layout           SharedLayoutView
	Prompts          *models.AgentPrompts
	WorkDir          string // host path to shared workspace/, empty if none
	ProjectID        string // defaultProjectId or project id for fill-empty
}

// SharedMCPView mirrors one MCP entry from shared agent.json.
type SharedMCPView struct {
	Name    string
	URL     string
	Headers map[string]string
	Command string
	Args    []string
	Env     map[string]string
}

// SharedLayoutView mirrors shared layout fields.
type SharedLayoutView struct {
	ConfigRoot   string
	WorkspaceDir string
}

// NodeReq is the resolved request to execute one agent/react node.
type NodeReq struct {
	RunID        string
	WorkflowID   string
	WorkflowName string
	Token        string // run-scoped MCP token, injected to the sandbox
	NodeID       string
	NodeType     string
	Config       map[string]any
	Vars         map[string]any
	// PromptImages are vars-referenced image attachments collected by the engine
	// for the node's first streamChat turn (react open / agent / rehydrate).
	PromptImages []models.PromptImage
	// KeepAliveForReview asks the provider to PARK the node's live sandbox +
	// ACP session after a successful RunAgent (instead of retiring it), so the
	// engine can drive an interactive post-run ReAct review phase (or a
	// downstream approval gate's ReAct reject) in the SAME sandbox without a
	// cold restart. The parked session is keyed runID|nodeID (reactKey) and is
	// retired only on review finish / gate approve / run abort.
	KeepAliveForReview bool
}

// GitInfo is the VCS-agnostic push detection result.
type GitInfo struct {
	Pushed    bool   `json:"pushed"`
	PushedSHA string `json:"pushedSha,omitempty"`
	Branch    string `json:"branch,omitempty"`
	MrURL     string `json:"mrUrl,omitempty"`
	// Repos carries per-repo git info in multi-repo (flat) mode; empty in
	// single-repo mode (where the top-level Branch/Pushed describe the run).
	Repos []RepoGit `json:"repos,omitempty"`
}

// RepoGit is one repository's git info in multi-repo mode.
type RepoGit struct {
	Name      string `json:"name"`
	Branch    string `json:"branch,omitempty"`
	Pushed    bool   `json:"pushed"`
	PushedSHA string `json:"pushedSha,omitempty"`
}

// NodeResult is the outcome of executing an agent/react node.
type NodeResult struct {
	OutputMd string
	Outputs  map[string]any
	Events   []models.AcpEvent
	Git      *GitInfo
	// Usage is token accounting accumulated across chat turns in this provider
	// call (nil = none reported). Engine merges it into the StateRun by adding
	// components so multi-turn react visits accumulate without double-counting.
	Usage *models.TokenUsage
	// UsageByModel is the per-model breakdown paired with Usage (after ingest
	// weak-key merge / ACP_BRIDGE_MODEL backfill).
	UsageByModel models.TokenUsageByModel
}

// ReactTurn is the outcome of one react dialogue turn (open or reply). The
// clarification is agent-driven: as long as the agent keeps raising structured
// Questions (via the ask_question MCP tool) the dialogue pauses for the human
// to choose; a turn with no Questions (or a forced/round-capped finish) sets
// Done, at which point the produces contract has been ensured and Result holds
// the node's outputs/artifacts.
type ReactTurn struct {
	Msg       string
	Questions []models.ReactQuestion
	Forms     []models.ReactForm
	Done      bool
	Result    NodeResult        // populated when Done (outputs/git)
	Events    []models.AcpEvent // this turn's event log (live/persisted timeline)
	// AgentSummary is the induction of the whole human dialogue, produced by
	// the hidden summary turn that only runs on「确认并流转」. Distinct from Msg
	// (transcript narration) and empty on every ordinary ReAct turn — callers
	// must not fall back to FeedbackSummary / first line / bubble text.
	AgentSummary string
	// Usage is this provider call's token delta (open/reply/finish chats).
	// Engine adds it onto the StateRun so mid-turn pauses still surface usage.
	Usage *models.TokenUsage
	// UsageByModel is the per-model breakdown paired with Usage.
	UsageByModel models.TokenUsageByModel
	// SetupErr is set when the sandbox/ACP session could not be acquired
	// (container create, image pull, connect handshake). Distinct from a normal
	// clarify pause where the agent raises Questions via ask_question.
	SetupErr error
	// Err is set when Done is true but the finish path failed (e.g. a re-prompt
	// nudge hit the per-turn chat deadline). Distinct from SetupErr.
	Err error
	// Interrupted marks a turn that did not finish on its own (sandbox or
	// platform timeout, cancel): Msg is partial and must not read as 已完成.
	Interrupted bool
}

// ExecProvider runs the two user-defined agent node kinds.
type ExecProvider interface {
	// Name identifies the backend; "cursor" is the only product backend.
	Name() string
	// RunAgent executes an autonomous agent node to completion.
	RunAgent(ctx context.Context, req NodeReq) (NodeResult, error)
	// ReactOpen produces the agent's opening turn of a react dialogue.
	ReactOpen(ctx context.Context, req NodeReq) ReactTurn
	// ReactReply advances a react dialogue with a human reply. When force is
	// true the caller (user "finish early") asks the agent to wrap up and the
	// turn finishes regardless of any further questions.
	ReactReply(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) ReactTurn
}

// ReviewProvider is an optional provider capability for the post-run ReAct
// review phase: a single in-place edit turn against a parked session that does
// NOT finish/close the node (unlike ReactReply's clarify semantics). The engine
// type-asserts for it; a provider without it degrades to no interactive review.
type ReviewProvider interface {
	// ReviseInPlace sends one human turn (annotations folded into `human`) to
	// the parked session for req's node, re-prompts the agent to persist its
	// structured product, and keeps the session alive for further edits. The
	// returned turn is never Done — finishing is a separate force step handled
	// by ReactReply.
	ReviseInPlace(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) ReactTurn
	// OfferCommitOnConfirm inspects the parked sandbox working trees before
	// RetireSession. If any repo is dirty it prompts the agent to selectively
	// commit meaningful changes (skip temp files) and then pushes already-
	// committed working branches. Best-effort: never fails the confirm.
	// Msg is non-empty only when the agent was actually asked to decide.
	OfferCommitOnConfirm(ctx context.Context, req NodeReq) ReactTurn
	// ReconcileOnConfirm runs the confirm-time pair on the parked session: one
	// visible turn asking the agent to reconcile its structured products with
	// the whole transcript, then a hidden turn inducing that transcript into
	// AgentSummary for the feedback ledger. Msg is the reconcile narration.
	// Best-effort: a dead session or failed chat returns an empty turn rather
	// than failing the confirm. Approve/clarify get the same pair inside
	// ReactReply(force=true).
	ReconcileOnConfirm(ctx context.Context, req NodeReq) ReactTurn
	// HasLiveSession reports whether a parked session is currently held for
	// (runID, nodeID) — used to decide whether an approval gate can offer the
	// ReAct reject entry (upstream session still alive).
	HasLiveSession(runID, nodeID string) bool
	// RetireSession closes and retires the parked session for (runID, nodeID),
	// if any (e.g. an approval gate approve that supersedes upstream review).
	RetireSession(runID, nodeID string)
}

// ReviewTurnCanceller is an optional provider capability: abort the in-flight
// ACP turn on a parked review session without retiring the session (轮级 Cancel).
// Bridge session/cancel also clears the sandbox PromptQueue.
type ReviewTurnCanceller interface {
	CancelSessionTurn(runID, nodeID string)
}

// BridgeStatus is the sandbox bridge's own view of a parked session's turns.
type BridgeStatus = sandbox.BridgeState

// SessionBridgeInspector is an optional provider capability: read what the
// sandbox bridge itself reports for a parked session (which can disagree with
// the platform FIFO after a turn the platform gave up on) and abort the turn
// the bridge is running.
type SessionBridgeInspector interface {
	// SessionBridgeState returns ok=false when no live session is parked.
	SessionBridgeState(runID, nodeID string) (BridgeStatus, bool)
	// AbortSessionTurn cancels the bridge's running turn and waits for the
	// bridge to confirm; false when it did not.
	AbortSessionTurn(runID, nodeID string) bool
}

// LiveEventSource is an optional provider capability: read a running node's
// full event log straight from its live sandbox. The engine type-asserts for
// it so non-sandbox providers (tests) simply have no live source.
//
// ok=false with err=nil means the node is not live here (caller falls back to
// the persisted snapshot). ok=false with err!=nil means a live sandbox was
// registered but the bridge read failed — callers must surface that error
// rather than treating it as an empty timeline.
type LiveEventSource interface {
	LiveNodeEvents(ctx context.Context, runID, nodeID string) (events []models.AcpEvent, ok bool, err error)
}

// LiveEventPageSource extends LiveEventSource with cursor/limit pagination.
type LiveEventPageSource interface {
	LiveEventSource
	LiveNodeEventsPage(ctx context.Context, runID, nodeID, cursor string, limit int) (events []models.AcpEvent, nextCursor string, hasMore, ok bool, err error)
}

// RunAborter is an optional provider capability: tear down any live sessions
// (and their sandboxes) still held for a run when it ends without completing
// normally — e.g. cancelled/failed while paused for human input at a react
// node. Without this the react sandbox stays registered (busy) and is never
// reclaimed. The engine type-asserts for it, so non-sandbox providers no-op.
type RunAborter interface {
	AbortRun(runID string)
}

// RunSandboxInfo describes a per-run node sandbox so it can be recorded in the
// platform sandbox store and shown in the sandbox UI while the node runs.
type RunSandboxInfo struct {
	Name           string
	Profile        string
	RunID          string
	WorkflowID     string
	WorkflowName   string
	NodeID         string
	Host           string
	ACPPort        int
	CodeServerPort int
	RepoURL        string
	// HomeDir is the host cursor-home dir bind-mounted into the container; the
	// store keeps it so it can be removed when the sandbox is finally destroyed.
	HomeDir string
	// Token is the run-scoped secret also injected as container PASSWORD for
	// code-server; SandboxProxy uses it for IDE auto-login.
	Token string
}

// SandboxRegistry records and clears live per-run node sandboxes. It is
// implemented by the platform SandboxService so per-run (ephemeral) node
// sandboxes appear in the same sandbox list as interactive test sandboxes.
// Optional: when nil, run sandboxes stay invisible (legacy behavior).
type SandboxRegistry interface {
	RegisterRunSandbox(info RunSandboxInfo)
	UnregisterRunSandbox(name string)
}

// RunSandboxBeginner is an optional registry capability: record a per-run node
// sandbox as "creating" BEFORE the (slow) gateway provisioning completes, so it
// is visible in the sandbox list and the node's live log as "starting" instead
// of a 404 during the cold-start window. On success the row is adopted by
// RegisterRunSandbox (real id + "running"); on failure it is removed by
// UnregisterRunSandbox. When the registry does not implement this, run sandboxes
// only appear once running (legacy behavior).
type RunSandboxBeginner interface {
	BeginRunSandbox(info RunSandboxInfo)
}

// RunSandboxRetirer is an optional registry capability: instead of destroying a
// finished run's node sandbox immediately, keep the container alive and set an
// idle-TTL deadline so it can be inspected (terminal / IDE / ACP / container
// logs) for debugging before the sweeper reclaims it. The provider type-asserts
// for it and falls back to UnregisterRunSandbox + immediate destroy when absent.
type RunSandboxRetirer interface {
	RetireRunSandbox(name string)
}

// RunSandboxLogArchiver is an optional capability (SandboxService / provider
// forwarder): before AggregateRunFailure, best-effort archive live container
// logs so CAPA failures after retire still have sandbox_logs.
type RunSandboxLogArchiver interface {
	ArchiveRunSandboxLogs(ctx context.Context, runID string) (archived int, degradeNote string)
}

// SandboxRegistrar is the optional provider capability used to inject the
// registry after construction (the SandboxService is built after the
// provider in main). main type-asserts the provider for it.
type SandboxRegistrar interface {
	SetSandboxRegistry(r SandboxRegistry)
}

// NewProvider builds the multi-backend ProviderRegistry. The name argument is
// kept for backward compatibility; GRASP_EXEC_PROVIDER is deprecated and
// routing is driven by each Agent's acpBackend field.
func NewProvider(name string, host *mcp.Host, opts Options) ExecProvider {
	WarnDeprecatedExecProvider(name)
	return NewProviderRegistry(host, opts)
}
