package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/internal/acp"
	"backend/internal/provider"

	"github.com/gorilla/websocket"
)

// MaxPromptQueueItems 每会话「待发送」上限。
const MaxPromptQueueItems = 32

// PromptQueueEntry：队列条目与 InMessage 展示字段：
// action + content 为主字段；id / opId / text 供前端队列面板展示。
type PromptQueueEntry struct {
	ID         string `json:"id,omitempty"`
	Action     string `json:"action,omitempty"`
	Content    string `json:"content,omitempty"`
	OpID       string `json:"opId,omitempty"`
	Text       string `json:"text,omitempty"`
	ImageCount int    `json:"imageCount,omitempty"` // 附带的图片数量，供队列面板展示
}

// Bridge 聚合单个聊天会话的 ACP 会话、权限等待与 WebSocket 广播（应用服务层）。
// 多个会话由 ChatManager 按 chat id 管理；default 会话即历史上的全局单例。
type Bridge struct {
	// 会话元数据（id 创建后不变；title 由 ChatManager 在 mu 下修改）
	id        string
	title     string
	createdAt time.Time
	// 所属 ChatManager；nil 表示独立使用（单测）
	mgr *ChatManager

	mu sync.Mutex
	// closed 在会话被 ChatManager.Delete 后置位：不再接受 WebSocket，也不再拉起 Agent。
	closed         bool
	sess           provider.Session
	agentCtx       context.Context
	agentCancel    context.CancelFunc
	permWait       map[string]chan string
	autoPermission bool
	// 每连接 *wsClient 一把写锁，禁止并发 Write、禁止双 map 维护。
	clients map[*websocket.Conn]*wsClient

	// Agent 退出广播去重（多标签、快速重连时 watch 与 Broadcast 可能短时间重复）
	exitNoticeMu      sync.Mutex
	lastExitBroadcast time.Time

	// EnsureAgent 与启动时拉起串行，避免并发双起子进程
	ensureMu sync.Mutex

	// 最近一次 session/new 使用的 MCP 列表（JSON），重启 Agent 时复用
	lastMCP json.RawMessage

	// testConnect, when non-nil, replaces Connect inside EnsureAgent (unit tests only).
	testConnect func(cwd, fsRoot string, mcp json.RawMessage, auto *bool) (provider.Session, error)

	// 当前 session/prompt 一轮：取消时结束 Conn.Call 等待，并配合 session/cancel 通知 Agent
	turnMu     sync.Mutex
	activeTurn *promptTurn

	// 等待中的用户消息（Agent 正回复时后续发送先入队，按 FIFO 逐个 session/prompt）
	queueMu     sync.Mutex
	promptQueue []queuedPrompt

	// 多 WebSocket / 多 goroutine 同时 Chat 时，入队 + 触发泵送必须全局串行，否则两条消息可能交错 append/pump
	enqueueMu sync.Mutex

	// 已完成的用户轮次（新 Agent / Connect 时清空；刷新时与 queue 一并下发 userTimeline）
	userDoneBuf userDoneBuffer

	// 会话级事件日志（内存全量）；刷新页面后随 connected 下发，前端重放以恢复聊天界面
	evLog eventLog

	// 会话事件订阅者；用于 QQ 等外部通道复用同一套 session/update 广播。
	eventSubMu     sync.Mutex
	eventSubNextID int
	eventSubs      map[int]func(json.RawMessage)

	// 本会话选择的模型；空表示跟随 ChatManager 的默认模型（-model / ACP_BRIDGE_MODEL）。
	model string

	// 回合看门狗：连续 turnIdle 没有任何活动（输出事件、CPU、IO）或总时长超过 turnMax 即终止回合；0 表示不限。
	turnIdle time.Duration
	turnMax  time.Duration
	// 活性判定：Agent 进程树每个采样周期的 CPU / IO 达到阈值即算有活动。sampler 为 nil 时只看输出事件。
	sampler     processSampler
	livenessCPU time.Duration
	livenessIO  int64
}

// queuedPrompt：入队前 InMessage 核心字段（单会话 FIFO）。
type queuedPrompt struct {
	Text   string            // Content
	OpID   string            // 入站消息 id（对齐 InMessage.ID，日志 oid=）
	Action string            // 如 chat；预留与 Router 多 action 一致
	Images []acp.PromptImage // 图片 / 文件附件（base64）
	// MaxDuration 覆盖本回合的总时长上限（chat 帧 deadlineSec）；0 用 Bridge.turnMax。
	MaxDuration time.Duration
	// IdleTimeout 覆盖本回合的无动作时限（chat 帧 idleSec）；0 用 Bridge.turnIdle。
	IdleTimeout time.Duration
}

// PromptImage 前端上传的图片附件（base64 编码），类型别名方便 handler 层引用。
type PromptImage = acp.PromptImage

type promptTurn struct {
	cancel       context.CancelFunc
	cancelCause  context.CancelCauseFunc // 看门狗以 provider.ErrTurnTimeout 为 cause 终止回合
	fromUserStop atomic.Bool             // true 表示由 Stop 触发，而非新消息顶替或 Agent 退出
	timedOut     atomic.Bool             // true 表示由看门狗超时终止
	recover      atomic.Bool             // true 表示这次空闲只杀进程、同会话再续跑一次
	continued    atomic.Bool             // true 表示已经续跑过，下一次空闲才是真正超时
	lastCause    string                  // 最近一次看门狗取消原因，写在 cancel 之前
	lastErr      error                   // 同 lastCause 的 error 形式（区分 stuck / timeout）
	loopRecover  atomic.Bool             // true 表示这次续跑是因为原地打转（同一工具调用反复执行）
	loop         toolLoop                // 连续相同工具调用计数
	loopC        chan struct{}           // 检测到原地打转时通知看门狗
	lastActivity atomic.Int64            // 最近一次 provider 事件（UnixNano），看门狗 idle 计时用
	started      time.Time               // 本轮用户消息开始时间；续跑不重置总时长
	opID         string                  // 与 ws oid= / queue_entries 对齐，供 queue_state.running 展示
	userText     string                  // 当前 session/prompt 的用户文案快照（仅 UI）
	imageCount   int                     // 附带的图片数量（仅 UI 展示）
}

func NewBridge() *Bridge {
	idle, max := turnLimitsFromEnv()
	liveCPU, liveIO := livenessThresholdsFromEnv()
	return &Bridge{
		sampler:        procSampler{root: "/proc"},
		livenessCPU:    liveCPU,
		livenessIO:     liveIO,
		id:             DefaultChatID,
		createdAt:      time.Now(),
		permWait:       make(map[string]chan string),
		clients:        make(map[*websocket.Conn]*wsClient),
		autoPermission: true,
		evLog:          eventLog{},
		eventSubs:      make(map[int]func(json.RawMessage)),
		turnIdle:       idle,
		turnMax:        max,
	}
}

// ID 返回会话 id（default 为默认会话）。
func (b *Bridge) ID() string { return b.id }

// SetModel 设置本会话选择的模型；空串表示跟随默认模型。下次建连生效。
func (b *Bridge) SetModel(m string) {
	b.mu.Lock()
	b.model = strings.TrimSpace(m)
	b.mu.Unlock()
}

// Model 返回本会话显式选择的模型；空表示跟随默认。
func (b *Bridge) Model() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.model
}

// EffectiveModel 返回建连实际使用的模型：会话选择 → 默认模型 → 空（由 CLI 自选，即 auto）。
func (b *Bridge) EffectiveModel() string {
	b.mu.Lock()
	m := b.model
	b.mu.Unlock()
	if m != "" || b.mgr == nil {
		return m
	}
	return b.mgr.DefaultModel()
}

func (b *Bridge) Session() provider.Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sess
}

// AgentLogPrefix 返回与 acp Conn 一致的 sid= 前缀，便于与 stdio 侧日志交叉检索。
func (b *Bridge) AgentLogPrefix() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sess == nil {
		return "sid=-"
	}
	return "sid=" + b.sess.SessionID()
}

// PromptQueueInfo 返回当前等待中的用户消息条数、容量与副本（不含正在执行中的那条）。
func (b *Bridge) PromptQueueInfo() (waiting, capacity int, entries []PromptQueueEntry) {
	b.queueMu.Lock()
	defer b.queueMu.Unlock()
	waiting = len(b.promptQueue)
	capacity = MaxPromptQueueItems
	entries = make([]PromptQueueEntry, len(b.promptQueue))
	for i, p := range b.promptQueue {
		entries[i] = promptQueueEntryFromQueued(p)
	}
	return
}

func promptQueueEntryFromQueued(p queuedPrompt) PromptQueueEntry {
	action := strings.TrimSpace(p.Action)
	if action == "" {
		action = "chat"
	}
	id := strings.TrimSpace(p.OpID)
	return PromptQueueEntry{
		ID:         id,
		Action:     action,
		Content:    p.Text,
		Text:       p.Text,
		OpID:       id,
		ImageCount: len(p.Images),
	}
}

// runningForClient 当前正在处理的一条；正文由前端截断展示。
func runningForClient(opID, userText string, imageCount int) map[string]any {
	id := strings.TrimSpace(opID)
	m := map[string]any{
		"id":     id,
		"opId":   id,
		"action": "chat",
		"text":   strings.TrimSpace(userText),
	}
	if imageCount > 0 {
		m["imageCount"] = imageCount
	}
	return m
}

// queueSnapshot：仅未开始的等待项在 queue_entries；running 为当前执行条。
func (b *Bridge) queueSnapshot() map[string]any {
	waiting, capacity, entries := b.PromptQueueInfo()
	b.turnMu.Lock()
	busy := b.activeTurn != nil
	var running map[string]any
	if t := b.activeTurn; t != nil && (strings.TrimSpace(t.userText) != "" || t.imageCount > 0) {
		running = runningForClient(t.opID, t.userText, t.imageCount)
	}
	b.turnMu.Unlock()
	m := map[string]any{
		"busy":           busy,
		"queue_length":   waiting,
		"queue_capacity": capacity,
		"queue_entries":  entries,
	}
	if running != nil {
		m["running"] = running
	}
	return m
}

func (b *Bridge) PromptQueueSnapshot() map[string]any {
	m := b.queueSnapshot()
	m["userTimeline"] = b.UserTimelineForClient()
	return m
}
