package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	_ ExecProvider         = (*ProviderRegistry)(nil)
	_ ReviewProvider       = (*ProviderRegistry)(nil)
	_ LiveMarkerScanner    = (*ProviderRegistry)(nil)
	_ LiveBaselinePreparer = (*ProviderRegistry)(nil)
	_ VisitorLaneProvider  = (*ProviderRegistry)(nil)
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

func (r *ProviderRegistry) PrepareLiveBaseline(ctx context.Context, runID, nodeID string) error {
	for _, p := range r.providers {
		if rp, ok := p.(ReviewProvider); ok && rp.HasLiveSession(runID, nodeID) {
			if prep, ok := p.(LiveBaselinePreparer); ok {
				return prep.PrepareLiveBaseline(ctx, runID, nodeID)
			}
			return errors.New("当前执行后端不支持 Live 源码基线")
		}
	}
	return errors.New("Live 预览会话不可用,请先恢复节点会话后重试")
}

func (r *ProviderRegistry) LiveMarkerSIDs(ctx context.Context, runID, nodeID string) ([]string, bool, error) {
	for _, p := range r.providers {
		if scanner, ok := p.(LiveMarkerScanner); ok {
			sids, parked, err := scanner.LiveMarkerSIDs(ctx, runID, nodeID)
			if parked || err != nil {
				return sids, parked, err
			}
		}
	}
	return nil, false, nil
}

func (r *ProviderRegistry) InstallLiveGuard(ctx context.Context, runID, nodeID string) {
	for _, p := range r.providers {
		if scanner, ok := p.(LiveMarkerScanner); ok {
			scanner.InstallLiveGuard(ctx, runID, nodeID)
		}
	}
}

func (r *ProviderRegistry) backendFor(req NodeReq) (AcpBackend, error) {
	profile := models.AgentProfile(req.Config)
	if profile == "" {
		return "", errors.New("节点未绑定 Agent")
	}
	if r.profilesRoot == "" {
		return "", errors.New("未配置 Agent 根目录")
	}
	dir, err := profileDir(r.profilesRoot, profile)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "agent.json"))
	if err != nil {
		return "", fmt.Errorf("读取 Agent %q 配置失败: %w", profile, err)
	}
	var cfg struct {
		AcpBackend string `json:"acpBackend"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf("解析 Agent %q 配置失败: %w", profile, err)
	}
	backend, err := ParseBackend(cfg.AcpBackend)
	if err != nil {
		return "", fmt.Errorf("Agent %q: %w", profile, err)
	}
	return backend, nil
}

func (r *ProviderRegistry) providerFor(req NodeReq) (ExecProvider, error) {
	b, err := r.backendFor(req)
	if err != nil {
		return nil, err
	}
	p := r.providers[b]
	if p == nil {
		return nil, fmt.Errorf("执行后端 %q 不可用", b)
	}
	return p, nil
}

func (r *ProviderRegistry) RunAgent(ctx context.Context, req NodeReq) (NodeResult, error) {
	p, err := r.providerFor(req)
	if err != nil {
		return NodeResult{}, err
	}
	log.Debug().Str("run", req.RunID).Str("node", req.NodeID).
		Str("acpBackend", p.Name()).Str("bridge", AgentRuntimeLabel(AcpBackend(p.Name()))).
		Msg("provider route")
	return p.RunAgent(ctx, req)
}

func (r *ProviderRegistry) ReactOpen(ctx context.Context, req NodeReq) ReactTurn {
	p, err := r.providerFor(req)
	if err != nil {
		return ReactTurn{SetupErr: err, Msg: "(" + err.Error() + ")"}
	}
	return p.ReactOpen(ctx, req)
}

func (r *ProviderRegistry) ReactReply(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) ReactTurn {
	p, err := r.providerFor(req)
	if err != nil {
		return ReactTurn{SetupErr: err, Msg: "(" + err.Error() + ")"}
	}
	return p.ReactReply(ctx, req, history, human, images, force)
}

// ReviseInPlace forwards a post-run review edit to the backend that owns the
// node's parked session (same agent_profile routing as RunAgent/ReactReply).
func (r *ProviderRegistry) ReviseInPlace(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) ReactTurn {
	p, err := r.providerFor(req)
	if err != nil {
		return ReactTurn{Err: err}
	}
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
	p, err := r.providerFor(req)
	if err != nil {
		return ReactTurn{}
	}
	rp, ok := p.(ReviewProvider)
	if !ok {
		return ReactTurn{}
	}
	return rp.OfferCommitOnConfirm(ctx, req)
}

// ReconcileOnConfirm forwards the confirm-time reconcile + summary pair to the
// backend that owns the parked session (same routing as OfferCommitOnConfirm).
func (r *ProviderRegistry) ReconcileOnConfirm(ctx context.Context, req NodeReq) ReactTurn {
	p, err := r.providerFor(req)
	if err != nil {
		return ReactTurn{}
	}
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

// VisitorTurn runs a share-link visitor turn on the backend holding the node's
// parked session (visitor chats branch from that sandbox).
func (r *ProviderRegistry) VisitorTurn(ctx context.Context, req NodeReq, lane, prelude, human string, images []models.PromptImage, onProgress func([]models.AcpEvent, bool)) ReactTurn {
	for _, p := range r.providers {
		rp, ok := p.(ReviewProvider)
		if !ok || !rp.HasLiveSession(req.RunID, req.NodeID) {
			continue
		}
		if vp, ok := p.(VisitorLaneProvider); ok {
			return vp.VisitorTurn(ctx, req, lane, prelude, human, images, onProgress)
		}
	}
	return ReactTurn{Msg: "(" + ErrNoParkedSession.Error() + ")", Err: ErrNoParkedSession}
}

// CancelVisitorTurn fans out to every backend; only the lane's owner acts.
func (r *ProviderRegistry) CancelVisitorTurn(runID, nodeID, lane string) {
	for _, p := range r.providers {
		if vp, ok := p.(VisitorLaneProvider); ok {
			vp.CancelVisitorTurn(runID, nodeID, lane)
		}
	}
}

// RetireVisitorLane fans out to every backend; idempotent.
func (r *ProviderRegistry) RetireVisitorLane(runID, nodeID, lane string) {
	for _, p := range r.providers {
		if vp, ok := p.(VisitorLaneProvider); ok {
			vp.RetireVisitorLane(runID, nodeID, lane)
		}
	}
}

// VisitorChatID returns the bridge chat serving a lane on whichever backend holds it.
func (r *ProviderRegistry) VisitorChatID(runID, nodeID, lane string) string {
	for _, p := range r.providers {
		if ci, ok := p.(interface {
			VisitorChatID(runID, nodeID, lane string) string
		}); ok {
			if id := ci.VisitorChatID(runID, nodeID, lane); id != "" {
				return id
			}
		}
	}
	return ""
}

// SessionBridgeState returns the first backend's bridge view for a parked
// session (sessions live on the backend that ran the producer).
func (r *ProviderRegistry) SessionBridgeState(runID, nodeID string) (BridgeStatus, bool) {
	for _, p := range r.providers {
		if bi, ok := p.(SessionBridgeInspector); ok {
			if st, hit := bi.SessionBridgeState(runID, nodeID); hit {
				return st, true
			}
		}
	}
	return BridgeStatus{}, false
}

// AbortSessionTurn aborts the bridge turn on the backend holding the session.
func (r *ProviderRegistry) AbortSessionTurn(runID, nodeID string) bool {
	for _, p := range r.providers {
		if bi, ok := p.(SessionBridgeInspector); ok {
			if _, hit := bi.SessionBridgeState(runID, nodeID); hit {
				return bi.AbortSessionTurn(runID, nodeID)
			}
		}
	}
	return true
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
