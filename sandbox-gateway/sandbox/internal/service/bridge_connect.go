package service

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"backend/internal/agents"
	"backend/internal/logging"
	"backend/internal/provider"
)

// ConnectPayload 与前端 WebSocket `op: connect` 对齐（argv 由服务端固定，仅解析目录与 MCP）。
type ConnectPayload struct {
	Op             string          `json:"op"`
	Cwd            string          `json:"cwd"`
	FsRoot         string          `json:"fsRoot"`
	McpServers     json.RawMessage `json:"mcpServers"`
	AutoPermission *bool           `json:"autoPermission"`
	Params         json.RawMessage `json:"params"`
}

type connectInner struct {
	Cwd            string          `json:"cwd"`
	FsRoot         string          `json:"fsRoot"`
	McpServers     json.RawMessage `json:"mcpServers"`
	AutoPermission *bool           `json:"autoPermission"`
}

// ParseConnect 解析连接参数（支持扁平或 params 嵌套）。
func ParseConnect(raw []byte) (cwd, fsRoot string, mcp json.RawMessage, auto *bool, err error) {
	var body ConnectPayload
	if err = json.Unmarshal(raw, &body); err != nil {
		return
	}
	if len(body.Params) > 0 && string(body.Params) != "null" {
		var inner connectInner
		if err = json.Unmarshal(body.Params, &inner); err != nil {
			return
		}
		return inner.Cwd, inner.FsRoot, inner.McpServers, inner.AutoPermission, nil
	}
	return body.Cwd, body.FsRoot, body.McpServers, body.AutoPermission, nil
}

// ConnectedPayload 返回 op:connected 元数据 + PromptQueueSnapshot()（队列与 userTimeline 单一路径，避免重复字段与双算时间线）。
func (b *Bridge) ConnectedPayload(p provider.Session) map[string]any {
	info := p.Info()
	agentInfo := map[string]any{
		"name":    info.Name,
		"title":   info.Title,
		"version": info.Version,
	}
	if info.ModelID != "" {
		agentInfo["modelId"] = info.ModelID
		agentInfo["modelName"] = info.ModelName
	}
	m := map[string]any{
		"op":        "connected",
		"sessionId": p.SessionID(),
		"cwd":       p.CWD(),
		"fsRoot":    p.FSRoot(),
		"agent":     agentInfo,
	}
	b.mu.Lock()
	curModel := b.model
	b.mu.Unlock()
	if curModel == "" {
		curModel = "auto"
	}
	if info.ModelID != "" {
		m["model"] = map[string]any{
			"id":   info.ModelID,
			"name": info.ModelName,
		}
	}
	m["currentModel"] = curModel
	// 协议已预留的可选 usage：仅当该会话上报用量时携带累计量，否则完全省略（帧不变）。
	if p.ReportsUsage() {
		if u := p.CumulativeUsage(); len(u) > 0 {
			m["usage"] = u
		}
	}
	for k, v := range b.PromptQueueSnapshot() {
		m[k] = v
	}
	// 附带最近 10 轮事件日志，供前端刷新后重放恢复聊天界面
	events, totalTurns, hasMore := b.EventLogRecentTurns(10)
	m["eventLog"] = events
	m["totalTurns"] = totalTurns
	m["hasMoreTurns"] = hasMore
	return m
}

// StartDefaultAgent 在进程启动后于后台拉起全局唯一 Agent；失败仅打日志，浏览器 connect 会重试。
func (b *Bridge) StartDefaultAgent() {
	go func() {
		t := true
		_, err := b.EnsureAgent("", "", nil, &t)
		if err != nil {
			log.Printf("acp: 启动时默认 Agent 未拉起（前端 connect 将重试）: %v", err)
			return
		}
		log.Printf("acp: 已建立全局 Agent 会话（单例）")
	}()
}

// EnsureAgent 若已有会话则直接返回；否则用 agents.Current() 建连（忽略浏览器传入的 argv）。
// 特例：启动时 StartDefaultAgent 常以空 MCP 建连，平台随后 connect 会带上
// artifact-store 等 mcpServers——若仍复用空会话，ACP session/new 永远看不到工具。
// 因此「空 → 非空」MCP 升级会重建会话。
func (b *Bridge) EnsureAgent(cwd, fsRoot string, mcp json.RawMessage, auto *bool) (provider.Session, error) {
	b.ensureMu.Lock()
	defer b.ensureMu.Unlock()

	if auto == nil {
		t := true
		auto = &t
	}

	b.mu.Lock()
	if b.sess != nil {
		if !mcpUpgradeNeeded(b.lastMCP, mcp) {
			p := b.sess
			b.mu.Unlock()
			return p, nil
		}
		log.Printf("acp: connect 带来非空 MCP，重建此前空 MCP 会话")
		b.mu.Unlock()
		return b.connectOrTest(cwd, fsRoot, mcp, auto)
	}
	b.mu.Unlock()

	return b.connectOrTest(cwd, fsRoot, mcp, auto)
}

func (b *Bridge) connectOrTest(cwd, fsRoot string, mcp json.RawMessage, auto *bool) (provider.Session, error) {
	if b.testConnect != nil {
		return b.testConnect(cwd, fsRoot, mcp, auto)
	}
	return b.Connect(cwd, fsRoot, mcp, auto)
}

// mcpEmpty reports whether the MCP payload is absent / null / empty list.
func mcpEmpty(m json.RawMessage) bool {
	s := strings.TrimSpace(string(m))
	return s == "" || s == "null" || s == "[]" || s == "{}"
}

// mcpUpgradeNeeded is true when an existing empty MCP session should be rebuilt
// to pick up a non-empty connect-time mcpServers list.
func mcpUpgradeNeeded(current, incoming json.RawMessage) bool {
	return mcpEmpty(current) && !mcpEmpty(incoming)
}

// mcpServerSummary returns count + names for CAPA A6 connect logs (no headers/URLs).
func mcpServerSummary(mcp json.RawMessage) (count int, names []string) {
	if mcpEmpty(mcp) {
		return 0, nil
	}
	var arr []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(mcp, &arr); err == nil && (len(arr) > 0 || string(mcp) == "[]") {
		for _, s := range arr {
			if n := strings.TrimSpace(s.Name); n != "" {
				names = append(names, n)
			}
		}
		return len(arr), names
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(mcp, &obj); err == nil {
		for k := range obj {
			names = append(names, k)
		}
		sort.Strings(names)
		return len(names), names
	}
	return 0, nil
}

// Connect 启动或替换 Agent 会话（由 AGENT_PROVIDER 选定的 provider 负责拉起对应 transport）。
func (b *Bridge) Connect(cwd, fsRoot string, mcp json.RawMessage, auto *bool) (provider.Session, error) {
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = wd
	}
	fsEff := fsRoot
	if fsEff == "" {
		fsEff = cwd
	}
	count, names := mcpServerSummary(mcp)
	log.Printf("acp: 实际使用目录 cwd=%q fsRoot=%q（前端留空时已用服务端当前目录或默认与 cwd 相同） mcp_servers_count=%d names=%v", cwd, fsEff, count, names)

	b.mu.Lock()
	if b.sess != nil {
		b.agentCancel()
		logging.WarnErr(b.sess.Close(), "agent session close on reconnect", nil)
		b.sess = nil
	}
	b.agentCtx, b.agentCancel = context.WithCancel(context.Background())
	if auto != nil {
		b.autoPermission = *auto
	}
	ctx := b.agentCtx
	m := b.model
	b.mu.Unlock()

	b.clearPromptQueue()
	b.clearUserTurnHistory()
	b.evLog.clear()

	// 握手 RPC 用 handshakeCtx；子进程必须用会话级 ctx（agentCtx）。切勿把 handshakeCtx 传给 provider.Open，
	// 否则 Connect 返回时 defer handshakeCancel 会取消 CommandContext 并 SIGKILL 刚握手成功的 Agent。
	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer handshakeCancel()

	onEvent := func(ev json.RawMessage) {
		b.Broadcast(eventEnvelope(ev, b.touchActiveTurn()))
	}
	perm := func(ctx context.Context, rpcID json.RawMessage, raw json.RawMessage) (string, error) {
		return b.permissionChooser(ctx, rpcID, raw)
	}
	autoPerm := auto != nil && *auto
	sess, err := agents.Current().Open(ctx, handshakeCtx, provider.OpenOptions{
		Cwd:            cwd,
		FSRoot:         fsEff,
		Model:          m,
		McpServers:     mcp,
		AutoPermission: autoPerm,
	}, onEvent, perm)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.sess = sess
	if len(mcp) > 0 && string(mcp) != "null" {
		b.lastMCP = append(json.RawMessage(nil), mcp...)
	} else {
		b.lastMCP = json.RawMessage(`[]`)
	}
	b.mu.Unlock()
	b.exitNoticeMu.Lock()
	b.lastExitBroadcast = time.Time{}
	b.exitNoticeMu.Unlock()
	go b.watchAgentConn(sess)
	b.BroadcastQueueState()
	return sess, nil
}

// watchAgentConn 在会话不可用（长驻子进程退出 / one-shot 显式关闭）后清理会话并通知所有
// WebSocket 客户端（避免仍显示「已连接」但一发消息就 broken pipe）。
func (b *Bridge) watchAgentConn(watched provider.Session) {
	<-watched.Done()

	// ExitInfo 内部先 Wait 回收子进程（再 Close，避免 Kill 与 Wait 竞态）。
	exitMsg, waitErr := watched.ExitInfo()
	if waitErr != nil {
		log.Printf("acp: Agent Wait: %v", waitErr)
	}

	b.mu.Lock()
	still := b.sess == watched
	var cancel context.CancelFunc
	if still {
		b.sess = nil
		cancel = b.agentCancel
	}
	b.mu.Unlock()
	if !still {
		return
	}
	b.clearPromptQueue()
	b.clearUserTurnHistory()
	b.evLog.clear()
	b.BroadcastQueueState()
	if cancel != nil {
		cancel()
	}
	logging.WarnErr(watched.Close(), "agent session close after exit", nil)

	b.broadcastAgentExitDebounced(exitMsg +
		" （刚显示「会话就绪」就失败，一般是 Agent 鉴权或运行环境问题，不是本页 WebSocket 未连上。）")
}

const agentExitBroadcastMinInterval = 3 * time.Second

func (b *Bridge) broadcastAgentExitDebounced(msg string) {
	b.exitNoticeMu.Lock()
	defer b.exitNoticeMu.Unlock()
	if !b.lastExitBroadcast.IsZero() && time.Since(b.lastExitBroadcast) < agentExitBroadcastMinInterval {
		log.Printf("acp: 已抑制短时间内重复的 Agent 退出通知")
		return
	}
	b.lastExitBroadcast = time.Now()
	b.Broadcast(map[string]any{
		"op":          "error",
		"message":     msg,
		"agentExited": true,
	})
}

// RestartAgent 结束当前子进程并重新 handshake + session/new，保留 cwd / fsRoot / MCP / 自动授权设置。
func (b *Bridge) RestartAgent() (provider.Session, error) {
	b.ensureMu.Lock()
	defer b.ensureMu.Unlock()

	b.mu.Lock()
	var cwd, fsRoot string
	var mcp json.RawMessage
	if p := b.sess; p != nil {
		cwd = p.CWD()
		fsRoot = p.FSRoot()
		if len(b.lastMCP) > 0 {
			mcp = append(json.RawMessage(nil), b.lastMCP...)
		}
	}
	auto := b.autoPermission
	b.mu.Unlock()

	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = wd
	}
	if fsRoot == "" {
		fsRoot = cwd
	}
	if len(mcp) == 0 {
		mcp = json.RawMessage(`[]`)
	}
	ap := auto
	log.Printf("acp: 收到重启请求，将重建 Agent（cwd=%q fsRoot=%q）", cwd, fsRoot)
	return b.Connect(cwd, fsRoot, mcp, &ap)
}
