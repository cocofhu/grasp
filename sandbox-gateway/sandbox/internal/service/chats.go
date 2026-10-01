package service

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/correl"
	"backend/internal/logging"

	"github.com/gorilla/websocket"
)

// DefaultChatID 是进程启动即存在、不可删除的默认会话（平台 /ws 不带 chat 参数时落到这里）。
const DefaultChatID = "default"

// DefaultMaxChats 是 SANDBOX_MAX_CHATS 未设置时的会话上限（含 default）。
const DefaultMaxChats = 8

const maxChatTitleLen = 64

var (
	ErrChatNotFound   = errors.New("chat not found")
	ErrTooManyChats   = errors.New("too many chats")
	ErrDefaultChatDel = errors.New("default chat cannot be deleted")
)

// ChatInfo 是 /api/chats 列表项。
type ChatInfo struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	Busy      bool      `json:"busy"`
	Connected bool      `json:"connected"`
	CreatedAt time.Time `json:"createdAt"`
}

// ChatManager 按 chat id 持有多个相互独立的 Bridge（同一 provider，各自的 Agent 会话、
// 队列、事件日志与 WebSocket 客户端）。会话只存于进程内存，WS 断开不销毁，Delete 才销毁。
type ChatManager struct {
	mu           sync.Mutex
	chats        map[string]*Bridge
	order        []string
	maxChats     int
	defaultModel string
}

// NewChatManager 创建只含 default 会话的管理器。
func NewChatManager() *ChatManager {
	m := &ChatManager{
		chats:    make(map[string]*Bridge),
		maxChats: maxChatsFromEnv(),
	}
	def := NewBridge()
	def.mgr = m
	m.chats[DefaultChatID] = def
	m.order = []string{DefaultChatID}
	return m
}

func maxChatsFromEnv() int {
	v := strings.TrimSpace(os.Getenv("SANDBOX_MAX_CHATS"))
	if v == "" {
		return DefaultMaxChats
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		log.Printf("acp-bridge: SANDBOX_MAX_CHATS=%q 无效，使用默认 %d", v, DefaultMaxChats)
		return DefaultMaxChats
	}
	return n
}

// SetDefaultModel 设置未显式选择模型的会话所用的默认模型（-model / ACP_BRIDGE_MODEL）。
func (m *ChatManager) SetDefaultModel(model string) {
	m.mu.Lock()
	m.defaultModel = strings.TrimSpace(model)
	m.mu.Unlock()
}

// DefaultModel 返回默认模型；空表示由 CLI 自选（auto）。
func (m *ChatManager) DefaultModel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.defaultModel
}

// MaxChats 返回会话上限（含 default）。
func (m *ChatManager) MaxChats() int { return m.maxChats }

// Default 返回默认会话。
func (m *ChatManager) Default() *Bridge {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chats[DefaultChatID]
}

// Get 按 id 取会话；空 id 视为 default。
func (m *ChatManager) Get(id string) (*Bridge, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultChatID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.chats[id]
	return b, ok
}

// StartDefaultAgent 在后台拉起 default 会话的 Agent；其余会话在首个 connect 时懒启动。
func (m *ChatManager) StartDefaultAgent() {
	m.Default().StartDefaultAgent()
}

// List 按创建顺序返回所有会话。
func (m *ChatManager) List() []ChatInfo {
	m.mu.Lock()
	list := make([]*Bridge, 0, len(m.order))
	titles := make([]string, 0, len(m.order))
	for _, id := range m.order {
		b := m.chats[id]
		list = append(list, b)
		titles = append(titles, b.title)
	}
	m.mu.Unlock()

	out := make([]ChatInfo, 0, len(list))
	for i, b := range list {
		out = append(out, b.info(titles[i]))
	}
	return out
}

// Info 返回单个会话的列表项。
func (m *ChatManager) Info(b *Bridge) ChatInfo {
	m.mu.Lock()
	title := b.title
	m.mu.Unlock()
	return b.info(title)
}

func (b *Bridge) info(title string) ChatInfo {
	b.turnMu.Lock()
	busy := b.activeTurn != nil
	b.turnMu.Unlock()
	b.mu.Lock()
	connected := b.sess != nil
	model := b.model
	b.mu.Unlock()
	return ChatInfo{
		ID:        b.id,
		Title:     title,
		Model:     model,
		Busy:      busy,
		Connected: connected,
		CreatedAt: b.createdAt,
	}
}

// Create 新建会话（Agent 懒启动）；model 为空表示跟随默认模型。
func (m *ChatManager) Create(title, model string) (*Bridge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.chats) >= m.maxChats {
		return nil, ErrTooManyChats
	}
	id := "c_" + correl.ID()
	for m.chats[id] != nil {
		id = "c_" + correl.ID()
	}
	title = normalizeChatTitle(title)
	b := NewBridge()
	b.id = id
	b.title = title
	b.mgr = m
	b.model = strings.TrimSpace(model)
	m.chats[id] = b
	m.order = append(m.order, id)
	log.Printf("acp-bridge: 新建会话 chat=%s title=%q model=%q", id, title, b.model)
	return b, nil
}

// Rename 修改会话标题。
func (m *ChatManager) Rename(id, title string) (*Bridge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.chats[id]
	if !ok {
		return nil, ErrChatNotFound
	}
	b.title = normalizeChatTitle(title)
	return b, nil
}

// Delete 结束会话的 Agent、断开它的 WebSocket 客户端并移除；default 不可删。
func (m *ChatManager) Delete(id string) error {
	if id == DefaultChatID {
		return ErrDefaultChatDel
	}
	m.mu.Lock()
	b, ok := m.chats[id]
	if ok {
		delete(m.chats, id)
		for i, v := range m.order {
			if v == id {
				m.order = append(m.order[:i:i], m.order[i+1:]...)
				break
			}
		}
	}
	m.mu.Unlock()
	if !ok {
		return ErrChatNotFound
	}
	b.shutdown()
	log.Printf("acp-bridge: 已删除会话 chat=%s", id)
	return nil
}

func normalizeChatTitle(t string) string {
	t = strings.TrimSpace(t)
	if r := []rune(t); len(r) > maxChatTitleLen {
		t = string(r[:maxChatTitleLen])
	}
	return t
}

// inheritFromDefault 让新会话复用 default 会话的工作目录与 MCP（浏览器 connect 不带这些），
// 使各 Tab 的 Agent 拥有与平台注入一致的工具与工作区。default 自身或显式传入时原样返回。
func (b *Bridge) inheritFromDefault(cwd, fsRoot string, mcp json.RawMessage) (string, string, json.RawMessage) {
	if b.mgr == nil || b.id == DefaultChatID {
		return cwd, fsRoot, mcp
	}
	def := b.mgr.Default()
	if def == nil {
		return cwd, fsRoot, mcp
	}
	def.mu.Lock()
	defer def.mu.Unlock()
	if p := def.sess; p != nil {
		if cwd == "" {
			cwd = p.CWD()
		}
		if fsRoot == "" {
			fsRoot = p.FSRoot()
		}
	}
	if mcpEmpty(mcp) && !mcpEmpty(def.lastMCP) {
		mcp = append(json.RawMessage(nil), def.lastMCP...)
	}
	return cwd, fsRoot, mcp
}

// shutdown 取消进行中的回合、清空队列、关闭 Agent，并断开本会话的所有 WebSocket。
func (b *Bridge) shutdown() {
	b.clearPromptQueue()
	b.turnMu.Lock()
	var stopTurn func()
	if t := b.activeTurn; t != nil {
		t.fromUserStop.Store(true)
		stopTurn = t.cancel
	}
	b.turnMu.Unlock()

	b.mu.Lock()
	b.closed = true
	sess := b.sess
	b.sess = nil
	cancel := b.agentCancel
	conns := make([]*wsClient, 0, len(b.clients))
	for _, wc := range b.clients {
		conns = append(conns, wc)
	}
	b.clients = make(map[*websocket.Conn]*wsClient)
	b.mu.Unlock()

	if sess != nil {
		logging.WarnErr(sess.Cancel(), "agent cancel on chat delete", nil)
	}
	if stopTurn != nil {
		stopTurn()
	}
	if cancel != nil {
		cancel()
	}
	if sess != nil {
		logging.WarnErr(sess.Close(), "agent session close on chat delete", nil)
	}
	for _, wc := range conns {
		logging.WarnErr(wc.conn.Close(), "ws close on chat delete", nil)
	}
}
