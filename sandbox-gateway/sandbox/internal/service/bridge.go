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
// action + content 为导出主字段；id / opId / text 便于前端与旧载荷兼容。
type PromptQueueEntry struct {
	ID         string `json:"id,omitempty"`
	Action     string `json:"action,omitempty"`
	Content    string `json:"content,omitempty"`
	OpID       string `json:"opId,omitempty"`
	Text       string `json:"text,omitempty"`
	ImageCount int    `json:"imageCount,omitempty"` // 附带的图片数量，供队列面板展示
}

// Bridge 聚合 ACP 会话、权限等待与 WebSocket 广播（应用服务层）。
type Bridge struct {
	mu             sync.Mutex
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

	// 用户指定的模型（环境变量 ACP_BRIDGE_MODEL 或 -model 参数）
	model      string
	modelFixed bool // 启动参数指定时锁定，前端不可切换

	// 回合看门狗：连续无事件 turnIdle 或总时长超过 turnMax 即终止回合；0 表示不限。
	turnIdle time.Duration
	turnMax  time.Duration
}

// queuedPrompt：入队前 InMessage 核心字段（单会话 FIFO）。
type queuedPrompt struct {
	Text   string            // Content
	OpID   string            // 入站消息 id（对齐 InMessage.ID，日志 oid=）
	Action string            // 如 chat；预留与 Router 多 action 一致
	Images []acp.PromptImage // 图片 / 文件附件（base64）
	// MaxDuration 覆盖本回合的总时长上限（chat 帧 deadlineSec）；0 用 Bridge.turnMax。
	MaxDuration time.Duration
}

// PromptImage 前端上传的图片附件（base64 编码），类型别名方便 handler 层引用。
type PromptImage = acp.PromptImage

type promptTurn struct {
	cancel       context.CancelFunc
	cancelCause  context.CancelCauseFunc // 看门狗以 provider.ErrTurnTimeout 为 cause 终止回合
	fromUserStop atomic.Bool             // true 表示由 Stop 触发，而非新消息顶替或 Agent 退出
	timedOut     atomic.Bool             // true 表示由看门狗超时终止
	lastActivity atomic.Int64            // 最近一次 provider 事件（UnixNano），看门狗 idle 计时用
	opID         string      // 与 ws oid= / queue_entries 对齐，供 queue_state.running 展示
	userText     string      // 当前 session/prompt 的用户文案快照（仅 UI）
	imageCount   int         // 附带的图片数量（仅 UI 展示）
}

func NewBridge() *Bridge {
	idle, max := turnLimitsFromEnv()
	return &Bridge{
		permWait:       make(map[string]chan string),
		clients:        make(map[*websocket.Conn]*wsClient),
		autoPermission: true,
		evLog:          eventLog{},
		eventSubs:      make(map[int]func(json.RawMessage)),
		turnIdle:       idle,
		turnMax:        max,
	}
}

// SetModel 设置用户指定的模型（启动时调用，线程安全）。
// fixed=true 表示由启动参数指定，前端不可切换。
func (b *Bridge) SetModel(m string, fixed bool) {
	b.mu.Lock()
	b.model = m
	b.modelFixed = fixed
	b.mu.Unlock()
}

// Model 返回当前指定的模型名称。
func (b *Bridge) Model() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.model
}

// ModelFixed 返回模型是否被启动参数锁定。
func (b *Bridge) ModelFixed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.modelFixed
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
