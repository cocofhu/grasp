package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

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
	curModel := b.EffectiveModel()
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
	m["reasoningEffort"] = b.ReasoningEffort()
	m["serviceTier"] = b.ServiceTier()
	m["serviceTiers"] = ServiceTiers(b.EffectiveModel())
	m["chatId"] = b.id
	caps := p.Capabilities()
	m["capabilities"] = map[string]any{"steer": caps.Steer, "resume": caps.Resume}
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
	if sessionUsable(b.sess) {
		if !mcpUpgradeNeeded(b.lastMCP, mcp) {
			p := b.sess
			b.mu.Unlock()
			return p, nil
		}
		log.Printf("acp: connect 带来非空 MCP，重建此前空 MCP 会话")
		b.mu.Unlock()
		return b.connectOrTest(openSpec{cwd: cwd, fsRoot: fsRoot, mcp: mcp, auto: auto})
	}
	b.mu.Unlock()

	return b.connectOrTest(openSpec{cwd: cwd, fsRoot: fsRoot, mcp: mcp, auto: auto})
}

// openSpec is what one connect opens.
type openSpec struct {
	cwd, fsRoot string
	mcp         json.RawMessage
	auto        *bool
	// keepHistory keeps the visible chat (events, queue, finished turns).
	keepHistory bool
	// resumeID continues that native session; empty starts a new one.
	resumeID string
}

func (b *Bridge) connectOrTest(spec openSpec) (provider.Session, error) {
	if b.testConnect != nil {
		return b.testConnect(spec.cwd, spec.fsRoot, spec.mcp, spec.auto)
	}
	return b.connect(spec)
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
	return b.connect(openSpec{cwd: cwd, fsRoot: fsRoot, mcp: mcp, auto: auto})
}

func (b *Bridge) connect(spec openSpec) (provider.Session, error) {
	cwd, fsRoot, mcp := b.inheritFromDefault(spec.cwd, spec.fsRoot, spec.mcp)
	auto := spec.auto
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

	b.enqueueMu.Lock()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		b.enqueueMu.Unlock()
		return nil, ErrChatNotFound
	}
	if b.sess != nil {
		b.agentCancel()
		logging.WarnErr(b.sess.Close(), "agent session close on reconnect", nil)
		b.sess = nil
	}
	b.agentCtx, b.agentCancel = context.WithCancel(context.Background())
	b.agentGeneration++
	if auto != nil {
		b.autoPermission = *auto
	}
	ctx := b.agentCtx
	b.mu.Unlock()
	m := b.EffectiveModel()

	if !spec.keepHistory {
		b.mu.Lock()
		b.compactPreamble = ""
		b.mu.Unlock()
		b.clearPromptQueue()
		b.clearUserTurnHistory()
		b.evLog.clear()
	}
	b.enqueueMu.Unlock()

	// 握手 RPC 用 handshakeCtx；子进程必须用会话级 ctx（agentCtx）。切勿把 handshakeCtx 传给 provider.Open，
	// 否则 Connect 返回时 defer handshakeCancel 会取消 CommandContext 并 SIGKILL 刚握手成功的 Agent。
	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer handshakeCancel()

	onEvent := func(ev json.RawMessage) {
		b.Broadcast(eventEnvelope(b.markSessionReset(ev), b.touchActiveTurn(ev)))
	}
	perm := func(ctx context.Context, rpcID json.RawMessage, raw json.RawMessage) (string, error) {
		return b.permissionChooser(ctx, rpcID, raw)
	}
	autoPerm := auto != nil && *auto
	sess, err := b.agent().Open(ctx, handshakeCtx, provider.OpenOptions{
		Cwd:                cwd,
		FSRoot:             fsEff,
		Model:              m,
		ReasoningEffort:    b.ReasoningEffort(),
		ReasoningEffortSet: true,
		ServiceTier:        b.ServiceTier(),
		ServiceTierSet:     true,
		McpServers:         mcp,
		AutoPermission:     autoPerm,
		ResumeSessionID:    spec.resumeID,
	}, onEvent, perm)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	if b.closed {
		// 握手期间会话被删除：不能把 Agent 挂回已删除的会话，否则子进程无人回收。
		b.mu.Unlock()
		logging.WarnErr(sess.Close(), "agent session close for deleted chat", nil)
		return nil, ErrChatNotFound
	}
	b.sess = sess
	b.sessOpened, b.sessThaw = time.Now(), thawCount()
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

// watchAgentConn 在会话不可用（长驻子进程退出 / one-shot 显式关闭）后清理会话。
// 能接回原生会话的 Agent 在限额内自动重启并保留排队消息；否则通知所有 WebSocket
// 客户端（避免仍显示「已连接」但一发消息就 broken pipe）。
func (b *Bridge) watchAgentConn(watched provider.Session) {
	<-watched.Done()
	// Invalidate before reaping: Done can mean a fatal protocol error while
	// the subprocess is still alive. Serialize queue cleanup with reconnects
	// and new messages so an old watcher cannot clear a replacement's queue.
	b.ensureMu.Lock()
	b.enqueueMu.Lock()
	b.mu.Lock()
	still := b.sess == watched
	var cancel context.CancelFunc
	generation := b.agentGeneration
	revive := still && !b.closed && resumePointer(watched) != "" && b.revivals.allow(time.Now())
	if still {
		b.sess = nil
		cancel = b.agentCancel
	}
	b.mu.Unlock()
	if still {
		if !revive {
			b.clearPromptQueue()
		}
		if cancel != nil {
			cancel()
		}
		// Keep the failed conversation available for diagnosis until an
		// explicit new Agent session clears it through Connect.
	}
	b.enqueueMu.Unlock()
	if still {
		b.BroadcastQueueState()
	}
	b.ensureMu.Unlock()

	// Close only the watched session, never the current one. ACP Close kills
	// a process whose stdout reader failed; Wait can then reap it safely.
	logging.WarnErr(watched.Close(), "agent session close after exit", nil)
	exitMsg, waitErr := watched.ExitInfo()
	if waitErr != nil {
		log.Printf("acp: Agent Wait: %v", waitErr)
	}
	if !still {
		return
	}

	// Reaping may finish after a reconnect. Hold ensureMu through the notice
	// so a stale agentExited frame cannot disconnect a newly ready session.
	b.ensureMu.Lock()
	b.mu.Lock()
	notify := b.sess == nil && b.agentGeneration == generation
	b.mu.Unlock()
	if notify && revive {
		if b.reviveLocked(watched, exitMsg) {
			b.ensureMu.Unlock()
			b.pumpPromptQueue()
			return
		}
		b.clearPromptQueue()
		b.BroadcastQueueState()
	}
	defer b.ensureMu.Unlock()
	if notify {
		b.broadcastAgentExitDebounced(exitMsg +
			" 请重新连接 Agent 后继续。")
	}
}

// reviveLocked restarts an Agent that exited on its own, on the same native
// session. The turn it was running has already failed; queued prompts run
// on the replacement.
func (b *Bridge) reviveLocked(dead provider.Session, exitMsg string) bool {
	log.Printf("acp: Agent 意外退出（%s），用原会话 %s 自动重启", exitMsg, dead.SessionID())
	sess, err := b.replaceLocked(dead)
	if err != nil {
		log.Printf("acp: Agent 自动重启失败: %v", err)
		return false
	}
	b.Broadcast(b.ConnectedPayload(sess))
	return true
}

// replaceLocked swaps the process behind the conversation without the
// client asking for it. When the native session could not be resumed, the
// next prompt_done says so (sessionReset), the same signal a one-shot agent
// gives, so the platform restores the context.
func (b *Bridge) replaceLocked(prev provider.Session) (provider.Session, error) {
	sess, resumed, err := b.reopenLocked(prev, restartContinue)
	if err != nil {
		return nil, err
	}
	if !resumed {
		log.Printf("acp: 未能接回会话 %s，已改用新会话 %s", prev.SessionID(), sess.SessionID())
		b.lostSession.Store(prev.SessionID())
	}
	return sess, nil
}

// markSessionReset adds sessionReset to the first prompt_done after a
// replacement lost the native session.
func (b *Bridge) markSessionReset(ev json.RawMessage) json.RawMessage {
	lost, _ := b.lostSession.Load().(string)
	if lost == "" || !bytes.Contains(ev, []byte(`"prompt_done"`)) {
		return ev
	}
	var frame map[string]any
	if json.Unmarshal(ev, &frame) != nil || frame["type"] != "prompt_done" {
		return ev
	}
	if !b.lostSession.CompareAndSwap(lost, "") {
		return ev
	}
	frame["sessionReset"], frame["previousSessionId"] = true, lost
	out, err := json.Marshal(frame)
	if err != nil {
		return ev
	}
	return out
}

// revivalBudget bounds automatic restarts so an Agent that dies on start
// cannot restart forever.
type revivalBudget struct {
	mu sync.Mutex
	at []time.Time
}

const (
	revivalLimit  = 3
	revivalWindow = 10 * time.Minute
)

func (r *revivalBudget) allow(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	recent := r.at[:0]
	for _, t := range r.at {
		if now.Sub(t) < revivalWindow {
			recent = append(recent, t)
		}
	}
	r.at = recent
	if len(r.at) >= revivalLimit {
		return false
	}
	r.at = append(r.at, now)
	return true
}

func sessionUsable(sess provider.Session) bool {
	if sess == nil {
		return false
	}
	select {
	case <-sess.Done():
		return false
	default:
		return true
	}
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

// restartMode says how a replacement agent process relates to the chat.
type restartMode int

const (
	// restartFresh starts a new conversation and clears the visible chat.
	restartFresh restartMode = iota
	// restartNewContext starts a new conversation and keeps the visible chat.
	restartNewContext
	// restartContinue replaces only the process: the native session is resumed
	// when the agent supports it, and the visible chat is kept either way.
	restartContinue
)

// RestartAgent 结束当前子进程并重新 handshake + session/new，保留 cwd / fsRoot / MCP / 自动授权设置。
func (b *Bridge) RestartAgent() (provider.Session, error) {
	b.ensureMu.Lock()
	defer b.ensureMu.Unlock()
	session, _, err := b.restartAgentLocked(restartFresh)
	return session, err
}

// RestartChatContext resets the provider session while preserving visible
// events and accepted prompts in this bridge chat.
func (b *Bridge) RestartChatContext() (provider.Session, error) {
	return b.restartChatContext("", true)
}

func (b *Bridge) restartChatContext(preamble string, markBoundary bool) (provider.Session, error) {
	b.ensureMu.Lock()
	defer b.ensureMu.Unlock()
	session, _, err := b.restartAgentLocked(restartNewContext)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.compactPreamble = preamble
	b.mu.Unlock()
	if markBoundary {
		// The platform supplies the new context in its next prompt. A later
		// console command must not replay the earlier archived prompts.
		b.Broadcast(eventEnvelope(map[string]any{"type": "context_boundary"}, ""))
	}
	return session, nil
}

// SwitchModelSettings updates model, effort and speed tier as one operation.
// A nil tier keeps the current one. A rejected restart restores every choice,
// and concurrent changes cannot interleave. resumed reports that the new
// process continues the native conversation, so the agent still has its
// context.
func (b *Bridge) SwitchModelSettings(model, effort string, serviceTier *string) (session provider.Session, resumed bool, err error) {
	b.ensureMu.Lock()
	defer b.ensureMu.Unlock()
	effectiveModel := strings.TrimSpace(model)
	if effectiveModel == "" && b.mgr != nil {
		effectiveModel = b.mgr.DefaultModel()
	}
	if serviceTier != nil && !ServiceTierSelectable(effectiveModel, *serviceTier) {
		return nil, false, ErrServiceTierUnavailable
	}
	b.mu.Lock()
	previousModel, previousEffort, previousTier := b.model, b.reasoningEffort, b.serviceTier
	b.model, b.reasoningEffort = strings.TrimSpace(model), strings.TrimSpace(effort)
	if serviceTier != nil {
		b.serviceTier = strings.TrimSpace(*serviceTier)
	}
	b.mu.Unlock()
	if !ServiceTierSelectable(b.EffectiveModel(), b.rawServiceTier()) {
		b.mu.Lock()
		b.serviceTier = ""
		b.mu.Unlock()
	}
	session, resumed, err = b.restartAgentLocked(restartContinue)
	if err != nil {
		b.mu.Lock()
		b.model, b.reasoningEffort, b.serviceTier = previousModel, previousEffort, previousTier
		b.mu.Unlock()
	}
	return session, resumed, err
}

func (b *Bridge) restartAgentLocked(mode restartMode) (provider.Session, bool, error) {
	b.mu.Lock()
	prev := b.sess
	b.mu.Unlock()
	return b.reopenLocked(prev, mode)
}

// reopenLocked opens a replacement for prev (which may already be dead) in
// its directories, with the chat's MCP servers and permission mode. The
// caller holds ensureMu.
func (b *Bridge) reopenLocked(prev provider.Session, mode restartMode) (provider.Session, bool, error) {
	b.mu.Lock()
	mcp := append(json.RawMessage(nil), b.lastMCP...)
	auto := b.autoPermission
	b.mu.Unlock()
	var cwd, fsRoot string
	if prev != nil {
		cwd, fsRoot = prev.CWD(), prev.FSRoot()
	}
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, false, err
		}
		cwd = wd
	}
	if fsRoot == "" {
		fsRoot = cwd
	}
	if len(mcp) == 0 {
		mcp = json.RawMessage(`[]`)
	}
	spec := openSpec{cwd: cwd, fsRoot: fsRoot, mcp: mcp, auto: &auto, keepHistory: mode != restartFresh}
	if mode == restartContinue {
		spec.resumeID = resumePointer(prev)
	}
	log.Printf("acp: 重建 Agent（cwd=%q fsRoot=%q resume=%q）", cwd, fsRoot, spec.resumeID)
	session, err := b.connectOrTest(spec)
	if err != nil {
		return nil, false, err
	}
	return session, spec.resumeID != "" && session.SessionID() == spec.resumeID, nil
}

// resumePointer is prev's native session when a new process can continue it.
func resumePointer(prev provider.Session) string {
	if prev == nil || !prev.Capabilities().Resume {
		return ""
	}
	if id := prev.SessionID(); id != provider.SessionPending {
		return id
	}
	return ""
}
