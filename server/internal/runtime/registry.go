package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// errReviewUnsupported is returned (via ReactTurn.Err) when a registry-routed
// backend does not implement ReviewProvider.
var errReviewUnsupported = errors.New("当前执行后端不支持 ReAct 复审")

// Compile-time: the production provider must expose review capabilities so the
// engine's type-assert to ReviewProvider succeeds in real deployments.
var (
	_ ExecProvider   = (*ProviderRegistry)(nil)
	_ ReviewProvider = (*ProviderRegistry)(nil)
)

// ProviderRegistry routes agent/react execution to the ExecProvider matching
// each agent_profile's acpBackend field.
type ProviderRegistry struct {
	providers    map[AcpBackend]ExecProvider
	profilesRoot string
	emit         func(runID, nodeID string, events []models.AcpEvent, busy bool)
}

// NewProviderRegistry builds one provider per product backend and wires a shared event sink.
func NewProviderRegistry(host *mcp.Host, opts Options) *ProviderRegistry {
	backends := []AcpBackend{BackendCursor, BackendClaudeCode, BackendCodeBuddy, BackendTrae, BackendOpenCode, BackendCodex}
	m := map[AcpBackend]ExecProvider{}
	for _, b := range backends {
		m[b] = newBaseACPProvider(host, opts, b)
	}
	return &ProviderRegistry{providers: m, profilesRoot: opts.ProfilesRoot}
}

func (r *ProviderRegistry) Name() string { return "registry" }

func (r *ProviderRegistry) backendFor(req NodeReq) AcpBackend {
	profile := models.AgentProfile(req.Config)
	if profile == "" || r.profilesRoot == "" {
		return BackendCursor
	}
	dir, err := profileDir(r.profilesRoot, profile)
	if err != nil {
		return BackendCursor
	}
	b, err := os.ReadFile(filepath.Join(dir, "agent.json"))
	if err != nil {
		return BackendCursor
	}
	var cfg struct {
		AcpBackend string `json:"acpBackend"`
	}
	_ = json.Unmarshal(b, &cfg)
	return NormalizeBackend(cfg.AcpBackend)
}

func (r *ProviderRegistry) providerFor(req NodeReq) ExecProvider {
	b := r.backendFor(req)
	p := r.providers[b]
	if p == nil {
		return r.providers[BackendCursor]
	}
	return p
}

func (r *ProviderRegistry) RunAgent(ctx context.Context, req NodeReq) (NodeResult, error) {
	b := r.backendFor(req)
	log.Debug().Str("run", req.RunID).Str("node", req.NodeID).
		Str("acpBackend", string(b)).Str("bridge", AgentRuntimeLabel(b)).
		Msg("provider route")
	return r.providerFor(req).RunAgent(ctx, req)
}

func (r *ProviderRegistry) ReactOpen(ctx context.Context, req NodeReq) ReactTurn {
	return r.providerFor(req).ReactOpen(ctx, req)
}

func (r *ProviderRegistry) ReactReply(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) ReactTurn {
	return r.providerFor(req).ReactReply(ctx, req, history, human, images, force)
}

// ReviseInPlace forwards a post-run review edit to the backend that owns the
// node's parked session (same agent_profile routing as RunAgent/ReactReply).
func (r *ProviderRegistry) ReviseInPlace(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) ReactTurn {
	p := r.providerFor(req)
	rp, ok := p.(ReviewProvider)
	if !ok {
		return ReactTurn{Msg: "(当前执行后端不支持 ReAct 复审)", Done: false,
			Err: errReviewUnsupported}
	}
	return rp.ReviseInPlace(ctx, req, history, human, images)
}

// OfferCommitOnConfirm forwards confirm-time git wrap-up to the backend that
// owns the parked session (same agent_profile routing as ReviseInPlace).
func (r *ProviderRegistry) OfferCommitOnConfirm(ctx context.Context, req NodeReq) ReactTurn {
	p := r.providerFor(req)
	rp, ok := p.(ReviewProvider)
	if !ok {
		return ReactTurn{}
	}
	return rp.OfferCommitOnConfirm(ctx, req)
}

// ReconcileOnConfirm forwards the confirm-time reconcile + summary pair to the
// backend that owns the parked session (same routing as OfferCommitOnConfirm).
func (r *ProviderRegistry) ReconcileOnConfirm(ctx context.Context, req NodeReq) ReactTurn {
	p := r.providerFor(req)
	rp, ok := p.(ReviewProvider)
	if !ok {
		return ReactTurn{}
	}
	return rp.ReconcileOnConfirm(ctx, req)
}

// HasLiveSession reports whether any backend holds a parked review session for
// (runID, nodeID). Sessions are parked on the backend that ran the producer, so
// we fan out like LiveNodeEvents.
func (r *ProviderRegistry) HasLiveSession(runID, nodeID string) bool {
	for _, p := range r.providers {
		if rp, ok := p.(ReviewProvider); ok && rp.HasLiveSession(runID, nodeID) {
			return true
		}
	}
	return false
}

// RetireSession closes a parked review session on every backend that holds one
// for (runID, nodeID). Idempotent no-op when none are parked.
func (r *ProviderRegistry) RetireSession(runID, nodeID string) {
	for _, p := range r.providers {
		if rp, ok := p.(ReviewProvider); ok {
			rp.RetireSession(runID, nodeID)
		}
	}
}

// CancelSessionTurn aborts an in-flight review turn on every backend that
// supports ReviewTurnCanceller (keeps the session parked).
func (r *ProviderRegistry) CancelSessionTurn(runID, nodeID string) {
	for _, p := range r.providers {
		if cp, ok := p.(ReviewTurnCanceller); ok {
			cp.CancelSessionTurn(runID, nodeID)
		}
	}
}

func (r *ProviderRegistry) LiveNodeEvents(ctx context.Context, runID, nodeID string) ([]models.AcpEvent, bool, error) {
	for _, p := range r.providers {
		if src, ok := p.(LiveEventSource); ok {
			ev, hit, err := src.LiveNodeEvents(ctx, runID, nodeID)
			if err != nil {
				return nil, false, err
			}
			if hit {
				return ev, true, nil
			}
		}
	}
	return nil, false, nil
}

func (r *ProviderRegistry) LiveNodeEventsPage(ctx context.Context, runID, nodeID, cursor string, limit int) ([]models.AcpEvent, string, bool, bool, error) {
	for _, p := range r.providers {
		if src, ok := p.(LiveEventPageSource); ok {
			ev, next, more, hit, err := src.LiveNodeEventsPage(ctx, runID, nodeID, cursor, limit)
			if err != nil {
				return nil, "", false, false, err
			}
			if hit {
				return ev, next, more, true, nil
			}
		}
	}
	return nil, "", false, false, nil
}

func (r *ProviderRegistry) AbortRun(runID string) {
	for _, p := range r.providers {
		if ab, ok := p.(RunAborter); ok {
			ab.AbortRun(runID)
		}
	}
}

func (r *ProviderRegistry) SetEventSink(fn func(runID, nodeID string, events []models.AcpEvent, busy bool)) {
	r.emit = fn
	for _, p := range r.providers {
		if sink, ok := p.(interface {
			SetEventSink(func(runID, nodeID string, events []models.AcpEvent, busy bool))
		}); ok {
			sink.SetEventSink(fn)
		}
	}
}

func (r *ProviderRegistry) SetSandboxRegistry(reg SandboxRegistry) {
	for _, p := range r.providers {
		if sr, ok := p.(SandboxRegistrar); ok {
			sr.SetSandboxRegistry(reg)
		}
	}
}

// ArchiveRunSandboxLogs forwards to the shared sandbox registry when it
// implements RunSandboxLogArchiver (production SandboxService).
func (r *ProviderRegistry) ArchiveRunSandboxLogs(ctx context.Context, runID string) (int, string) {
	for _, p := range r.providers {
		if a, ok := p.(RunSandboxLogArchiver); ok {
			return a.ArchiveRunSandboxLogs(ctx, runID)
		}
	}
	return 0, "执行后端未暴露沙箱日志归档"
}
