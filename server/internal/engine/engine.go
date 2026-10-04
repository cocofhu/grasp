// Package engine implements the finite-state-machine orchestrator: nodes
// are states, edges are transitions (success / failure / rollback). It
// drives node executors, evaluates guards, persists per-node StateRuns and
// the run-level state trace, and on failure can roll back to a checkpoint
// (restoring the variable snapshot and injecting error context).
package engine

import (
	"sync"
	"sync/atomic"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

// Engine orchestrates workflow runs.
type Engine struct {
	db       *gorm.DB
	provider runtime.ExecProvider
	host     *mcp.Host
	store    mcp.Store
	broker   *Broker
	// sem bounds the number of runs actively executing (in the "running"
	// state). Its capacity is the effective max_concurrent_runs and can be
	// changed at runtime via SetMaxConcurrent (settings page).
	sem *sema
	// wake signals the dispatcher to (re)drain the queued-run backlog. Buffered
	// size 1 so signals coalesce and senders never block.
	wake chan struct{}
	// stop closes to end the dispatcher goroutine (tests / process shutdown).
	stop     chan struct{}
	stopOnce sync.Once

	// autoRetryMax caps how many times a node that fails with retryable=true
	// (and has no explicit failure/rollback edge) is auto-retried from the
	// failure position before the run is failed. 0 disables auto-retry.
	// Live-tunable from the settings page via SetAutoRetryMax; the raw engine
	// defaults to disabled so tests that don't wire settings keep the old
	// fail-fast behavior (production enables it via SettingsService.ApplyOnBoot,
	// whose config default is 3). Stored as Int64 to avoid int→int32 narrowing
	// (CodeQL #7); only negative inputs are clamped to 0.
	autoRetryMax atomic.Int64

	mu     sync.Mutex
	tokens map[string]string // runID -> MCP token (in-memory; same process)

	// resumeMu guards resumeLocks; resumeLocks serializes human-resume
	// operations (gate resume / react reply) per "runID:nodeID" so a
	// double-submit (e.g. a page refresh re-enabling the confirm button while
	// the first reply is still being processed by the slow sandbox agent)
	// cannot both pass the "already done" guard and advance the FSM twice.
	// liveMu serializes Live session state transitions.
	liveMu sync.Mutex

	resumeMu    sync.Mutex
	resumeLocks map[string]*sync.Mutex

	// execMu guards execRuns / execGen. execRuns is the set of runs that
	// currently have a live execute() goroutine. The FSM keeps exactly one
	// active node per run, so a run must be driven by at most one goroutine.
	// execGen is a per-run ownership token: Cancel/ResumeFrom may force-clear a
	// zombie slot (agent still blocked in RunAgent after the run was cancelled)
	// by bumping the generation so the late driver's deferred endExecute cannot
	// delete a newer driver's claim.
	execMu   sync.Mutex
	execRuns map[string]bool
	execGen  map[string]uint64

	// haltMu guards halted: when true the dispatcher stops admitting work and
	// agent/react nodes finish as cancelled instead of advancing the FSM.
	haltMu sync.RWMutex
	halted bool

	// projectVarsLookup, when set, returns project-level workflow variables for
	// a workflow so StartRun can seed them into vars.* before Graph.Variables
	// and launch inputs overlay. Optional (nil = no project seed).
	projectVarsLookup func(workflowID string) []models.ProjectVariable

	// auditRecorder, when set, receives project-scoped lifecycle events
	// (run terminal states, gate decisions). Fail-open is the recorder's job.
	auditRecorder func(rec services.AuditRecord)

	// issues resolves preview-issue lifecycle on gate resume. Optional in tests
	// (nil falls back to a short-lived IssueService on e.db).
	issues IssueResolver

	// gateAuto is an optional async observer for human_gate / proposal_select /
	// app_preview pauses (PM auto-invoke). Engine never blocks on it.
	gateAuto GateAutoInvoker

	// shareRevoker invalidates unused GateShareLinks when a gate/run ends.
	shareRevoker ShareLinkRevoker

	// runNotify is an optional async observer for confirmed waiting_human /
	// node-scoped failed transitions (QQ Run notify). Engine never blocks on it.
	runNotify RunNotifier

	// reviewSess holds per parked producer session FIFO + single worker for
	// node-inline review, gate hot-revise and visitor lanes.
	reviewSess chatsession.Registry[*reviewSession] // key: laneSessionKey

	// visitorMu guards visitorLanes: share-link visitor lanes holding (or
	// about to hold) a sandbox chat, for the per-link cap and idle sweep.
	visitorMu    sync.Mutex
	visitorLanes map[string]*visitorLaneState // key: runID|producerID|lane
	visitorSweep sync.Once

	// pageMu guards pageSessions: the page_* session id of each running turn
	// that has a sender.
	pageMu       sync.Mutex
	pageSessions map[string]*pageSession

	// skills looks up Agents for same-project agent_profile runtime gate.
	skills SkillLookup

	// blobs externalizes PromptImage bytes (optional in unit tests).
	blobs blob.Store
}

// SetBlobStore wires attachment externalization for StartRun / react turns.
func (e *Engine) SetBlobStore(store blob.Store) { e.blobs = store }

// New builds an engine.
func New(db *gorm.DB, provider runtime.ExecProvider, host *mcp.Host, store mcp.Store, maxRuns int) *Engine {
	if maxRuns <= 0 {
		maxRuns = 5
	}
	e := &Engine{
		db: db, provider: provider, host: host, store: store,
		broker: NewBroker(), sem: newSema(maxRuns),
		wake: make(chan struct{}, 1), stop: make(chan struct{}),
		tokens: map[string]string{}, resumeLocks: map[string]*sync.Mutex{},
		execRuns: map[string]bool{}, execGen: map[string]uint64{},
	}

	if sink, ok := provider.(interface {
		SetEventSink(func(runID, nodeID string, events []models.AcpEvent, busy bool))
	}); ok {
		sink.SetEventSink(e.publishAcp)
	}

	host.SetAfterWriteArtifact(e.syncAfterPrimaryArtifactWrite)
	host.SetArtifactPreviewHook(e.setReactPreviewArtifact)
	e.reconcileInterrupted()

	go e.dispatch()
	return e
}

// SetMaxConcurrent changes the live cap on simultaneously executing runs and
// nudges the dispatcher so a raised limit immediately admits more queued runs.
func (e *Engine) SetMaxConcurrent(n int) {
	e.sem.SetLimit(n)
	e.signalDispatch()
}

// SkillLookup is the narrow Agent catalog port used by the agent_profile
// project gate. Composition root injects *services.AgentService.
type SkillLookup interface {
	Get(name string) (services.Agent, bool)
}

// IssueResolver is the narrow preview-issue port used on gate resume.
// Composition root injects *services.IssueService.
type IssueResolver interface {
	MarkResolvedByNode(runID, nodeID string) error
}

// SetAgents wires the Agent catalog used by the agent_profile project gate.
func (e *Engine) SetAgents(skills SkillLookup) { e.skills = skills }

// MaxConcurrent returns the current concurrency cap.
func (e *Engine) MaxConcurrent() int { return e.sem.Limit() }

// SetProjectVarsLookup wires the lookup used by StartRun to seed project-level
// workflow variables into the run's vars.* namespace.
func (e *Engine) SetProjectVarsLookup(fn func(workflowID string) []models.ProjectVariable) {
	e.projectVarsLookup = fn
}

// ShareLinkRevoker invalidates unused temporary approval links.
type ShareLinkRevoker interface {
	RevokeUnusedForRun(runID string)
	RevokeUnusedForGate(runID, nodeID string, iteration int)
	RevokeUnusedForNode(runID, nodeID string)
}

// SetShareRevoker wires GateShareLink invalidation on resume/cancel/finish.
func (e *Engine) SetShareRevoker(r ShareLinkRevoker) {
	e.shareRevoker = r
}

// SetAuditRecorder wires project audit recording for run lifecycle / gate events.
func (e *Engine) SetAuditRecorder(fn func(rec services.AuditRecord)) {
	e.auditRecorder = fn
}

func (e *Engine) recordAudit(rec services.AuditRecord) {
	if e == nil || e.auditRecorder == nil {
		return
	}
	e.auditRecorder(rec)
}

// SetIssueService wires the preview-issue service used on gate resume.
func (e *Engine) SetIssueService(s IssueResolver) {
	e.issues = s
}

func (e *Engine) issueService() IssueResolver {
	if e.issues != nil {
		return e.issues
	}
	return services.NewIssueService(e.db)
}

// SetAutoRetryMax changes the live cap on how many times a failed node is
// auto-retried from its failure position (see autoRetryMax). Negative clamps to
// 0 (disabled). Non-negative values are stored as int64 with no MaxInt32 product
// clamp (CodeQL #7). Driven by the settings page / config.
func (e *Engine) SetAutoRetryMax(n int) {
	if n < 0 {
		n = 0
	}
	e.autoRetryMax.Store(int64(n))
}

// AutoRetryMax returns the current per-node auto-retry cap (0 = disabled).
func (e *Engine) AutoRetryMax() int { return int(e.autoRetryMax.Load()) }
