package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/rs/zerolog/log"
)

// ErrNoDeliveryChannel is returned by Deliver when no enabled channel for the
// project is configured as the cron delivery target.
var ErrNoDeliveryChannel = errors.New("项目未配置定时任务推送渠道")

// ErrNoRunNotifyTarget is returned by DeliverRunNotify when no enabled channel
// has a usable IM target session for the project (CronDeliver flag not required).
var ErrNoRunNotifyTarget = errors.New("项目未配置可用的渠道推送目标")

// Reply/Work equivalent orchestration (physical dual sandbox NOT required):
//
//   - Manager (= Reply): the unique QQ egress — immediate ACK, queue ACK,
//     on-demand progress, terminal reports, and cron push coordination.
//   - ChannelBridge / PmTurnRunner / cron sandbox (= Work): execute turns and
//     emit internal progress/results; must not bypass Manager to Send on QQ.
//
// Speak priority: user ACK/final > on-demand progress > cron push > unchanged.

const (
	ackProcessingPrefix = "收到，正在处理："
	ackSummaryRunes     = 40
	queueAckPrefix      = "已收到，排队中"
	queueFullText       = "队列已满，请稍候"
	failReplyPrefix     = "处理失败："
	// convQueueDepth is the per-conversation pending FIFO capacity (in-flight
	// turn is not counted). The next inbound after 16 pending is rejected.
	convQueueDepth = 16
)

// queuedInbound is a message waiting behind an in-flight turn for the same
// conversation key (project|scene|conversationID).
type queuedInbound struct {
	ctx context.Context
	rc  *runningChannel
	in  InboundMessage
}

// convQueue serializes turns for one conversation: at most one in-flight
// handler plus a bounded FIFO of pending inbound messages.
type convQueue struct {
	mu      sync.Mutex
	pending []queuedInbound
	busy    bool
}

// Manager owns the lifecycle of channel adapters and is the Reply-side unique
// QQ egress. Configs are supplied by a loader (backed by the DB) and applied
// idempotently: adapters are started, stopped, or restarted based on a config
// fingerprint so admin edits hot-reload without a server restart.
type Manager struct {
	bridge    *ChannelBridge
	factories map[string]AdapterFactory
	decrypt   func(enc string) (string, error)
	loader    func() ([]models.ChannelConfig, error)

	applyMu sync.Mutex // serializes Apply/Reload
	mu      sync.Mutex
	running map[string]*runningChannel // keyed by config ID

	convMu     sync.Mutex
	convQueues map[string]*convQueue

	pushMu     sync.Mutex
	pushQueues map[string]*pushQueue

	baseCtx context.Context

	runtimeMu sync.Mutex
	runtime   map[string]runtimeInfo

	// Test hooks (production leaves these nil/zero):
	// handleFunc overrides bridge.Handle when set (no progress callback).
	handleFunc func(ctx context.Context, rc ResolvedChannel, in InboundMessage) (Reply, error)
	// handleFuncWithProgress overrides bridge.Handle when set.
	handleFuncWithProgress func(ctx context.Context, rc ResolvedChannel, in InboundMessage, onProgress func(ProgressEvent)) (Reply, error)
}

type runningChannel struct {
	cfg         models.ChannelConfig
	fingerprint string
	adapter     Adapter
	cancel      context.CancelFunc
}

// NewManager builds a manager. factories maps channel type → adapter factory;
// decrypt reverses the stored app secret.
func NewManager(bridge *ChannelBridge, factories map[string]AdapterFactory, decrypt func(string) (string, error)) *Manager {
	return &Manager{
		bridge:     bridge,
		factories:  factories,
		decrypt:    decrypt,
		running:    map[string]*runningChannel{},
		convQueues: map[string]*convQueue{},
		pushQueues: map[string]*pushQueue{},
		runtime:    map[string]runtimeInfo{},
		baseCtx:    context.Background(),
	}
}

type runtimeInfo struct {
	State  string
	Detail string
}

// RuntimeState returns the process-local connection state for a channel id.
func (m *Manager) RuntimeState(id string) (state, detail string) {
	if m == nil {
		return "", ""
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	info := m.runtime[id]
	return info.State, info.Detail
}

func (m *Manager) setRuntime(id, state, detail string) {
	if m == nil || strings.TrimSpace(id) == "" {
		return
	}
	m.runtimeMu.Lock()
	m.runtime[id] = runtimeInfo{State: state, Detail: detail}
	m.runtimeMu.Unlock()
}

// SetLoader registers the DB-backed config source used by Reload/ApplyOnBoot.
func (m *Manager) SetLoader(loader func() ([]models.ChannelConfig, error)) {
	m.loader = loader
}

// ApplyOnBoot loads persisted configs and starts enabled adapters.
func (m *Manager) ApplyOnBoot() {
	m.Reload()
}

// Reload re-reads configs from the loader and applies them.
func (m *Manager) Reload() {
	if m.loader == nil {
		return
	}
	cfgs, err := m.loader()
	if err != nil {
		log.Warn().Err(err).Msg("channel manager: load configs failed")
		return
	}
	m.Apply(cfgs)
}

// Apply reconciles running adapters against the desired config set. Adapters
// are started asynchronously so a slow connect never blocks the caller
// (admin write / boot).
func (m *Manager) Apply(cfgs []models.ChannelConfig) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	desired := map[string]models.ChannelConfig{}
	for _, c := range cfgs {
		if !c.Enabled {
			continue
		}
		desired[c.ID] = c
	}

	// Stop removed or changed adapters.
	m.mu.Lock()
	var toStop []*runningChannel
	for id, rc := range m.running {
		want, ok := desired[id]
		if !ok || fingerprint(want) != rc.fingerprint {
			toStop = append(toStop, rc)
			delete(m.running, id)
		}
	}
	m.mu.Unlock()
	for _, rc := range toStop {
		m.stopChannel(rc)
	}

	// Start new / changed adapters.
	for id, cfg := range desired {
		m.mu.Lock()
		_, already := m.running[id]
		m.mu.Unlock()
		if already {
			continue
		}
		m.startChannel(cfg)
	}
}

// StopAll tears down every running adapter (shutdown path).
func (m *Manager) StopAll() {
	m.mu.Lock()
	all := make([]*runningChannel, 0, len(m.running))
	for _, rc := range m.running {
		all = append(all, rc)
	}
	m.running = map[string]*runningChannel{}
	m.mu.Unlock()
	for _, rc := range all {
		m.stopChannel(rc)
	}
}

func (m *Manager) startChannel(cfg models.ChannelConfig) {
	factory, ok := m.factories[cfg.Type]
	if !ok {
		log.Warn().Str("type", cfg.Type).Str("id", cfg.ID).Msg("channel manager: no adapter factory")
		return
	}
	secret := ""
	if strings.TrimSpace(cfg.AppSecretEnc) != "" {
		dec, err := m.decrypt(cfg.AppSecretEnc)
		if err != nil {
			log.Warn().Err(err).Str("id", cfg.ID).Msg("channel manager: decrypt secret failed; skipping")
			m.setRuntime(cfg.ID, ConnStateAuthFailed, "凭证解密失败，无法建立长连接。")
			return
		}
		secret = dec
	}
	adapter, err := factory(AdapterConfig{
		ID: cfg.ID, Type: cfg.Type, Name: cfg.Name, ProjectID: cfg.ProjectID,
		AppID: cfg.AppID, AppSecret: secret, Config: cfg.Config,
		HasSpoken: func(scene Scene, conversationID string) bool {
			return m.bridge != nil && m.bridge.HasSpoken(cfg.ProjectID, SyntheticUserID(cfg.Type, scene, conversationID))
		},
	})
	if err != nil {
		log.Warn().Err(err).Str("id", cfg.ID).Msg("channel manager: build adapter failed")
		m.setRuntime(cfg.ID, ConnStateAuthFailed, "App ID / App Secret 校验失败，长连接未建立。")
		return
	}
	if sa, ok := adapter.(StatefulAdapter); ok {
		sa.SetStateHandler(func(state, detail string) {
			m.setRuntime(cfg.ID, state, detail)
		})
	}
	ctx, cancel := context.WithCancel(m.baseCtx)
	rc := &runningChannel{cfg: cfg, fingerprint: fingerprint(cfg), adapter: adapter, cancel: cancel}
	m.mu.Lock()
	m.running[cfg.ID] = rc
	m.mu.Unlock()

	go func() {
		handler := func(ctx context.Context, in InboundMessage) {
			m.dispatch(ctx, rc, in)
		}
		if err := adapter.Start(ctx, handler); err != nil {
			log.Warn().Err(err).Str("id", cfg.ID).Str("type", cfg.Type).Msg("channel adapter start failed")
			cancel()
			m.mu.Lock()
			if cur := m.running[cfg.ID]; cur == rc {
				delete(m.running, cfg.ID)
			}
			m.mu.Unlock()
			if errors.Is(err, ErrAdapterAuth) {
				m.setRuntime(cfg.ID, ConnStateAuthFailed, "App ID / App Secret 校验失败，长连接未建立。")
			} else {
				m.setRuntime(cfg.ID, ConnStateDisconnected,
					"长连接已断开。请确认自建应用在线且同一 App ID 无第二条连接互踢。")
			}
			return
		}
		m.setRuntime(cfg.ID, ConnStateConnected, "长连接在线")
		log.Info().Str("id", cfg.ID).Str("type", cfg.Type).Str("project", cfg.ProjectID).Msg("channel adapter started")
	}()
}

func (m *Manager) stopChannel(rc *runningChannel) {
	if rc == nil {
		return
	}
	rc.cancel()
	if err := rc.adapter.Stop(); err != nil {
		log.Warn().Err(err).Str("id", rc.cfg.ID).Msg("channel adapter stop error")
	}
	m.setRuntime(rc.cfg.ID, ConnStateDisconnected, "长连接已断开。请确认自建应用在线且同一 App ID 无第二条连接互踢。")
	log.Info().Str("id", rc.cfg.ID).Str("type", rc.cfg.Type).Msg("channel adapter stopped")
}

// convKey builds the per-channel conversation queue key. channelID is required
// so two bots in the same project that share a scene:conversation address do
// not serialize on one FIFO (run_notify fan-out / multi-bot edge cases).
func convKey(channelID, projectID string, scene Scene, conversationID string) string {
	return channelID + "|" + projectID + "|" + string(scene) + "|" + conversationID
}

// IsConversationBusy reports whether a user turn is in-flight or the user FIFO
// is non-empty for the channel conversation. Shared by cron push coordination.
func (m *Manager) IsConversationBusy(channelID, projectID string, scene Scene, conversationID string) bool {
	q := m.convQueueFor(convKey(channelID, projectID, scene, conversationID))
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.busy || len(q.pending) > 0
}

// dispatch serializes messages per conversation via a bounded in-process FIFO.
// Idle + empty queue: immediate processing ACK then Work. Busy: enqueue with a
// per-message queue ACK (ahead count); dequeue sends another processing ACK.
// Full queue: reject with a visible reply (never silently drop).
func (m *Manager) dispatch(ctx context.Context, rc *runningChannel, in InboundMessage) {
	key := convKey(rc.cfg.ID, rc.cfg.ProjectID, in.Scene, in.ConversationID)
	q := m.convQueueFor(key)

	q.mu.Lock()
	if q.busy {
		if len(q.pending) >= convQueueDepth {
			q.mu.Unlock()
			m.sendOutbound(ctx, rc, OutboundMessage{
				Scene: in.Scene, ConversationID: in.ConversationID,
				ReplyToMessageID: in.MessageID, Text: queueFullText,
			})
			return
		}
		// Ahead = in-flight turn + already-queued messages.
		ahead := 1 + len(q.pending)
		q.pending = append(q.pending, queuedInbound{ctx: ctx, rc: rc, in: in})
		q.mu.Unlock()
		m.sendOutbound(ctx, rc, OutboundMessage{
			Scene: in.Scene, ConversationID: in.ConversationID,
			ReplyToMessageID: in.MessageID, Text: queueAckTextFor(ahead, in.Text),
		})
		return
	}
	q.busy = true
	q.mu.Unlock()

	// This goroutine owns the busy cycle: run the idle-first turn, then drain.
	m.runTurn(ctx, rc, in, true /* withProcessingAck */)
	m.drainConvQueue(q, key)
}

// drainConvQueue runs pending messages in arrival order until the queue is
// empty, then flushes any silent cron push queue for this conversation.
func (m *Manager) drainConvQueue(q *convQueue, key string) {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.busy = false
			q.mu.Unlock()
			_ = m.flushPushQueue(key)
			return
		}
		next := q.pending[0]
		q.pending = q.pending[1:]
		q.mu.Unlock()
		// Dequeue: another processing ACK before Work.
		m.runTurn(next.ctx, next.rc, next.in, true /* withProcessingAck */)
	}
}

// runTurn executes one PM turn and sends the final or failure reply.
// withProcessingAck emits the required ≤1s ACK before dispatching Work.
func (m *Manager) runTurn(ctx context.Context, rc *runningChannel, in InboundMessage, withProcessingAck bool) {
	if withProcessingAck {
		m.sendOutbound(ctx, rc, OutboundMessage{
			Scene: in.Scene, ConversationID: in.ConversationID,
			ReplyToMessageID: in.MessageID, Text: processingAckTextFor(rc.cfg.Type, in.Text),
		})
	}

	resolved := ResolvedChannel{
		ID: rc.cfg.ID, Type: rc.cfg.Type, ProjectID: rc.cfg.ProjectID,
		AgentName:   rc.cfg.AgentName,
		EnabledMcps: rc.cfg.EnabledMcps,
		TurnTimeout: time.Duration(rc.cfg.TurnTimeoutSeconds) * time.Second,
		Caps:        SessionCapsFromConfig(rc.cfg.Config),
	}
	onProgress := func(ev ProgressEvent) {
		text := FormatProgressTextFor(rc.cfg.Type, ev)
		if text == "" {
			return
		}
		m.sendOutbound(ctx, rc, OutboundMessage{
			Scene: in.Scene, ConversationID: in.ConversationID,
			ReplyToMessageID: in.MessageID, Text: text,
		})
	}
	reply, err := m.handleTurn(ctx, resolved, in, onProgress)
	if err != nil {
		log.Warn().Err(err).Str("channel", rc.cfg.ID).Msg("channel turn failed")
		m.sendOutbound(ctx, rc, OutboundMessage{
			Scene: in.Scene, ConversationID: in.ConversationID,
			ReplyToMessageID: in.MessageID, Text: failReplyPrefix + friendlyErr(err),
		})
		return
	}
	m.sendOutbound(ctx, rc, OutboundMessage{
		Scene: in.Scene, ConversationID: in.ConversationID, ReplyToMessageID: in.MessageID,
		Text: reply.Text, ImageURLs: reply.ImageURLs,
	})
}

func (m *Manager) handleTurn(ctx context.Context, rc ResolvedChannel, in InboundMessage, onProgress func(ProgressEvent)) (Reply, error) {
	if m.handleFuncWithProgress != nil {
		return m.handleFuncWithProgress(ctx, rc, in, onProgress)
	}
	if m.handleFunc != nil {
		return m.handleFunc(ctx, rc, in)
	}
	return m.bridge.Handle(ctx, rc, in, onProgress)
}

func (m *Manager) sendOutbound(ctx context.Context, rc *runningChannel, out OutboundMessage) {
	if rc == nil || rc.adapter == nil {
		return
	}
	if err := rc.adapter.Send(ctx, out); err != nil {
		log.Warn().Err(err).Str("channel", rc.cfg.ID).Str("text", truncateRunes(out.Text, 40)).
			Msg("channel outbound send failed")
	}
}

// Deliver pushes cron/proactive text to a project's configured delivery channel.
// Legacy entry: treats body as "changed" and routes through coordinated egress.
// Without AgentName it cannot pick among multiple channels — returns ErrNoDeliveryChannel.
// Implements services.ChannelDeliverer.
func (m *Manager) Deliver(projectID, text string) error {
	return m.DeliverCron(services.CronDelivery{
		ProjectID: projectID,
		Category:  "cron",
		Kind:      string(CronResultChanged),
		Text:      text,
	})
}

// DeliverCron is the coordinated cron→QQ egress: busy conversations silent-enqueue
// (no enqueue side-chat); idle sends immediately. Routes by job AgentName → the
// Channel bound to that Agent (respecting that Channel's CronDeliver flag).
// Implements services.ChannelDeliverer.
func (m *Manager) DeliverCron(d services.CronDelivery) error {
	target, scene, conv, err := m.lookupDeliveryTarget(d.ProjectID, d.AgentName)
	if err != nil {
		return err
	}
	_ = target
	kind := CronResultKind(strings.TrimSpace(d.Kind))
	switch kind {
	case CronResultChanged, CronResultUnchanged, CronResultFailed:
	default:
		kind = ClassifyCronResult(d.Text)
	}
	text := FormatCronPush(d.Category, kind, d.Text)
	key := convKey(target.cfg.ID, d.ProjectID, scene, conv)
	item := CronPushItem{
		ProjectID: d.ProjectID,
		AgentName: d.AgentName,
		ChannelID: target.cfg.ID,
		Scene:     scene,
		Conv:      conv,
		Category:  d.Category,
		Kind:      kind,
		// Keep raw formatted text (incl. image URLs); flushPushQueue splits on send.
		Text:     text,
		Enqueued: time.Now(),
	}

	// Always enqueue then flush: idle path still goes through flushPushQueue so
	// a concurrent user ACK between IsConversationBusy and Send cannot insert
	// cron text ahead of the user turn (TOCTOU). Busy path stays silent — no
	// "已入队"旁白. flushPushQueue re-checks busy before each Send.
	m.enqueuePush(key, item)
	return m.flushPushQueue(key)
}

// DeliverRunNotify fans out a Run lifecycle notification to the explicit
// channelIDs list. Unlike DeliverCron it does NOT require CronDeliver=true —
// each Enabled channel with a non-empty CronDeliverTarget (session address) works.
// Text is sent as-is (no FormatCronPush). Implements services.RunNotifyDeliverer.
// Empty channelIDs or no valid targets → services.ErrRunNotifyNoTarget.
// Individual invalid/disabled targets are skipped; failures do not abort others.
func (m *Manager) DeliverRunNotify(projectID, text string, channelIDs []string) error {
	channelIDs = services.NormalizeNotifyChannelIDs(channelIDs)
	if len(channelIDs) == 0 {
		return services.ErrRunNotifyNoTarget
	}
	sent := 0
	var lastErr error
	for _, id := range channelIDs {
		target, scene, conv, err := m.lookupRunNotifyTargetByID(projectID, id)
		if err != nil {
			log.Info().Err(err).Str("project", projectID).Str("channel", id).
				Msg("run-notify: skip invalid/disabled channel target")
			continue
		}
		_ = target
		key := convKey(id, projectID, scene, conv)
		item := CronPushItem{
			ProjectID: projectID,
			ChannelID: id,
			Scene:     scene,
			Conv:      conv,
			Category:  "run_notify",
			Kind:      CronResultChanged, // priority: treat actionable Run alerts like "changed"
			Text:      text,
			Enqueued:  time.Now(),
		}
		m.enqueuePush(key, item)
		if ferr := m.flushPushQueue(key); ferr != nil {
			log.Warn().Err(ferr).Str("project", projectID).Str("channel", id).
				Msg("run-notify: send failed")
			lastErr = ferr
			continue
		}
		sent++
	}
	if sent == 0 {
		if lastErr != nil {
			return lastErr
		}
		return services.ErrRunNotifyNoTarget
	}
	return lastErr
}

// HasRunNotifyTarget reports whether any of the listed channelIDs is an Enabled
// channel with a usable QQ session address (independent of CronDeliver).
func (m *Manager) HasRunNotifyTarget(projectID string, channelIDs []string) bool {
	for _, id := range services.NormalizeNotifyChannelIDs(channelIDs) {
		if _, _, _, err := m.lookupRunNotifyTargetByID(projectID, id); err == nil {
			return true
		}
	}
	return false
}

func (m *Manager) flushPushQueue(key string) error {
	items := m.takePushQueue(key)
	if len(items) == 0 {
		return nil
	}
	var firstErr error
	for i, item := range items {
		// Re-check busy: a new user message may have arrived.
		// Re-queue current AND all remaining — never drop the tail.
		if m.IsConversationBusy(item.ChannelID, item.ProjectID, item.Scene, item.Conv) {
			m.requeuePushAll(key, items[i:])
			return firstErr
		}
		var target *runningChannel
		var err error
		if item.Category == "run_notify" {
			if item.ChannelID != "" {
				target, _, _, err = m.lookupRunNotifyTargetByID(item.ProjectID, item.ChannelID)
			} else {
				err = ErrNoRunNotifyTarget
			}
		} else {
			target, _, _, err = m.lookupDeliveryTarget(item.ProjectID, item.AgentName)
		}
		if err != nil {
			log.Warn().Err(err).Str("project", item.ProjectID).Str("category", item.Category).
				Str("channel", item.ChannelID).Str("agent", item.AgentName).
				Msg("push flush: no delivery channel")
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		stripped, urls := splitImageURLs(item.Text)
		ctx, cancel := context.WithTimeout(m.baseCtx, 60*time.Second)
		if err := target.adapter.Send(ctx, OutboundMessage{
			Scene: item.Scene, ConversationID: item.Conv, Text: stripped, ImageURLs: urls,
		}); err != nil {
			log.Warn().Err(err).Str("project", item.ProjectID).Msg("cron push flush send failed")
			if firstErr == nil {
				firstErr = err
			}
		}
		cancel()
	}
	return firstErr
}

// OnlineReporter is implemented by adapters that expose subscribe/handshake state.
type OnlineReporter interface {
	Online() bool
}

// IsOnline reports whether the running adapter has an established connection.
// Missing adapters are offline. Adapters without OnlineReporter are online
// once Start has succeeded (QQ gateway).
func (m *Manager) IsOnline(channelID string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	rc := m.running[channelID]
	m.mu.Unlock()
	if rc == nil || rc.adapter == nil {
		return false
	}
	if r, ok := rc.adapter.(OnlineReporter); ok {
		return r.Online()
	}
	return true
}

// requeuePushAll puts remaining flush items back ahead of anything that arrived
// while the queue was taken, re-applying merge/depth via enqueuePush so pending
// never silently exceeds pushQueueDepth.
func (m *Manager) requeuePushAll(key string, items []CronPushItem) {
	if len(items) == 0 {
		return
	}
	arrived := m.drainPushQueueRaw(key)
	combined := append(append([]CronPushItem(nil), items...), arrived...)
	for _, item := range combined {
		m.enqueuePush(key, item)
	}
}

// drainPushQueueRaw clears pending without priority reordering (used by requeue).
func (m *Manager) drainPushQueueRaw(key string) []CronPushItem {
	q := m.pushQueueFor(key)
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}
	out := append([]CronPushItem(nil), q.pending...)
	q.pending = nil
	return out
}

// lookupDeliveryTarget finds the CronDeliver channel bound to agentName.
// Empty agentName refuses the legacy "latest in project" ambiguity.
func (m *Manager) lookupDeliveryTarget(projectID, agentName string) (*runningChannel, Scene, string, error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return nil, "", "", ErrNoDeliveryChannel
	}
	m.mu.Lock()
	var target *runningChannel
	for _, rc := range m.running {
		if rc.cfg.ProjectID != projectID {
			continue
		}
		if strings.TrimSpace(rc.cfg.AgentName) != agentName {
			continue
		}
		if rc.cfg.CronDeliver && strings.TrimSpace(rc.cfg.CronDeliverTarget) != "" {
			target = rc
			break
		}
	}
	m.mu.Unlock()
	if target == nil {
		return nil, "", "", ErrNoDeliveryChannel
	}
	scene, conv := parseTarget(target.cfg.CronDeliverTarget)
	if conv == "" {
		return nil, "", "", ErrNoDeliveryChannel
	}
	return target, scene, conv, nil
}

// lookupRunNotifyTargetByID finds an Enabled channel by id with a usable
// session address (CronDeliverTarget), without requiring CronDeliver=true.
func (m *Manager) lookupRunNotifyTargetByID(projectID, channelID string) (*runningChannel, Scene, string, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, "", "", ErrNoRunNotifyTarget
	}
	m.mu.Lock()
	rc := m.running[channelID]
	m.mu.Unlock()
	if rc == nil || !rc.cfg.Enabled || rc.cfg.ProjectID != projectID {
		return nil, "", "", ErrNoRunNotifyTarget
	}
	if strings.TrimSpace(rc.cfg.CronDeliverTarget) == "" {
		return nil, "", "", ErrNoRunNotifyTarget
	}
	scene, conv := parseTarget(rc.cfg.CronDeliverTarget)
	if conv == "" {
		return nil, "", "", ErrNoRunNotifyTarget
	}
	return rc, scene, conv, nil
}

func (m *Manager) convQueueFor(key string) *convQueue {
	m.convMu.Lock()
	defer m.convMu.Unlock()
	q, ok := m.convQueues[key]
	if !ok {
		q = &convQueue{}
		m.convQueues[key] = q
	}
	return q
}

func processingAckText(userText string) string {
	return ackProcessingPrefix + truncateRunes(userText, ackSummaryRunes)
}

func processingAckTextFor(channelType, userText string) string {
	if channelType == "feishu" || channelType == "dingtalk" {
		return "已收到，正在处理：" + truncateRunes(userText, ackSummaryRunes)
	}
	return processingAckText(userText)
}

func queueAckTextFor(ahead int, userText string) string {
	summary := truncateRunes(userText, ackSummaryRunes)
	if ahead > 0 {
		return fmt.Sprintf("%s（前方 %d 条）：%s", queueAckPrefix, ahead, summary)
	}
	return fmt.Sprintf("%s：%s", queueAckPrefix, summary)
}

func parseTarget(target string) (Scene, string) {
	parts := strings.SplitN(strings.TrimSpace(target), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return Scene(parts[0]), parts[1]
}

func fingerprint(c models.ChannelConfig) string {
	b, _ := json.Marshal(struct {
		Type, ProjectID, AgentName, AppID, AppSecretEnc, CronDeliverTarget string
		Enabled, CronDeliver, IsPrimary                                    bool
		TurnTimeoutSeconds                                                 int
		EnabledMcps                                                        []string
		Config                                                             map[string]any
	}{
		Type: c.Type, ProjectID: c.ProjectID, AgentName: c.AgentName, AppID: c.AppID,
		AppSecretEnc: c.AppSecretEnc, CronDeliverTarget: c.CronDeliverTarget,
		Enabled: c.Enabled, CronDeliver: c.CronDeliver, IsPrimary: c.IsPrimary,
		TurnTimeoutSeconds: c.TurnTimeoutSeconds, EnabledMcps: c.EnabledMcps, Config: c.Config,
	})
	return string(b)
}

func friendlyErr(err error) string {
	msg := err.Error()
	// Truncate on runes so a multi-byte (e.g. Chinese) message is never cut
	// mid-character into invalid UTF-8.
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "…"
	}
	return msg
}
