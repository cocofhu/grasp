package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"backend/internal/acp"
	"backend/internal/provider"
)

const clientName = "grasp"

// rpcTimeout bounds request/response calls that do not wait for a turn.
var rpcTimeout = 30 * time.Second

// session is one app-server process and its thread.
type session struct {
	conn    *acp.Conn
	opts    provider.OpenOptions
	env     []string
	onEvent func(json.RawMessage)
	cleanup func()

	cwd, fsRoot string
	// model is the display model: the connection's chat model, else what the
	// thread reported.
	model    string
	threadID string

	// turnMu serializes Prompt; one turn runs on the thread at a time.
	turnMu sync.Mutex

	mu       sync.Mutex
	turn     *activeTurn
	cumUsage map[string]provider.TokenUsage
	// subagents are the threads this thread spawned that may still run.
	subagents map[string]bool

	closeOnce sync.Once
}

// activeTurn is the running turn as seen through notifications.
type activeTurn struct {
	id   string
	done chan turnEnd
	// streamed holds items whose text arrived as deltas; their completed
	// item is not emitted again.
	streamed map[string]bool
	// baseline is the thread total before this turn's first model request;
	// total the latest thread total.
	baseline, total *tokenTotals
	errText         string
}

type turnEnd struct {
	status  string
	errText string
}

func newSession(conn *acp.Conn, opts provider.OpenOptions, env []string, onEvent func(json.RawMessage), cleanup func()) *session {
	fsRoot := opts.FSRoot
	if fsRoot == "" {
		fsRoot = opts.Cwd
	}
	s := &session{conn: conn, opts: opts, env: env, onEvent: onEvent, cleanup: cleanup, cwd: opts.Cwd, fsRoot: fsRoot,
		cumUsage: map[string]provider.TokenUsage{}, subagents: map[string]bool{}}
	conn.SetNotificationHandler(s.onNotification)
	conn.SetRequestHandler(s.onRequest)
	go func() { <-conn.Done(); s.release() }()
	return s
}

func (s *session) handshake(ctx context.Context, opts provider.OpenOptions) error {
	if _, err := s.conn.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": clientName, "title": "Grasp", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		return fmt.Errorf("codex initialize: %w", err)
	}
	if err := s.conn.Notify("initialized", nil); err != nil {
		return fmt.Errorf("codex initialized: %w", err)
	}
	params := map[string]any{"cwd": s.cwd, "approvalPolicy": "never", "sandbox": sandboxMode(opts.AutoPermission)}
	if s.model == "" && opts.Model != "" && opts.Model != "auto" {
		params["model"] = opts.Model
	}
	if opts.ResumeSessionID != "" {
		resume := maps.Clone(params)
		resume["threadId"] = opts.ResumeSessionID
		s.applyProcessSettings(ctx, resume)
		err := s.openThread(ctx, "thread/resume", resume)
		var rpcErr *acp.RPCError
		if err == nil || !errors.As(err, &rpcErr) {
			return err
		}
		// The thread is gone (e.g. its rollout did not survive a cold restore).
		// A fresh thread reports a new SessionID, which tells the bridge.
		log.Printf("codex: thread/resume %s 被拒绝，改用新线程: %v", opts.ResumeSessionID, err)
	}
	return s.openThread(ctx, "thread/start", params)
}

// applyProcessSettings makes a resumed thread run with this process's
// settings. thread/resume keeps the model, reasoning effort and service tier
// recorded in the thread: naming the model re-derives model and effort from
// the -c overrides, and the tier must be passed itself.
func (s *session) applyProcessSettings(ctx context.Context, resume map[string]any) {
	var res struct {
		Config struct {
			Model       string `json:"model"`
			ServiceTier string `json:"service_tier"`
		} `json:"config"`
	}
	if raw, err := s.conn.Call(ctx, "config/read", map[string]any{}); err == nil {
		_ = json.Unmarshal(raw, &res)
	}
	if res.Config.ServiceTier != "" {
		resume["serviceTier"] = res.Config.ServiceTier
	}
	if _, named := resume["model"]; named {
		return
	}
	model := res.Config.Model
	if model == "" {
		model = s.defaultModel(ctx)
	}
	if model != "" {
		resume["model"] = model
	}
}

// defaultModel is the catalog's default model, or empty.
func (s *session) defaultModel(ctx context.Context) string {
	raw, err := s.conn.Call(ctx, "model/list", map[string]any{})
	if err != nil {
		log.Printf("codex: 无法确定当前模型，接回的线程沿用原设置: %v", err)
		return ""
	}
	var res struct {
		Data []struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"isDefault"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &res)
	for _, m := range res.Data {
		if m.IsDefault {
			return m.ID
		}
	}
	return ""
}

func (s *session) openThread(ctx context.Context, method string, params map[string]any) error {
	raw, err := s.conn.Call(ctx, method, params)
	if err != nil {
		return fmt.Errorf("codex %s: %w", method, err)
	}
	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.Thread.ID == "" {
		return fmt.Errorf("codex %s: no thread in response", method)
	}
	s.threadID = res.Thread.ID
	s.conn.SetLogSID(s.threadID)
	if s.model == "" {
		s.model = res.Model
	}
	return nil
}

// sandboxMode never asks for approval; it decides what commands may touch.
func sandboxMode(auto bool) string {
	if auto {
		return "danger-full-access"
	}
	return "workspace-write"
}

func (s *session) SessionID() string { return s.threadID }
func (s *session) CWD() string       { return s.cwd }
func (s *session) FSRoot() string    { return s.fsRoot }

func (s *session) Info() provider.AgentInfo {
	return provider.AgentInfo{Name: string(provider.Codex), Title: string(provider.Codex), ModelID: s.model, ModelName: s.model}
}

func (s *session) ReportsUsage() bool { return true }

func (s *session) CumulativeUsage() map[string]provider.TokenUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cumUsage) == 0 {
		return nil
	}
	out := make(map[string]provider.TokenUsage, len(s.cumUsage))
	for k, v := range s.cumUsage {
		out[k] = v
	}
	return out
}

// AgentPIDs is the app-server; the commands it runs are its descendants.
func (s *session) AgentPIDs() []int {
	if pid := s.conn.PID(); pid > 0 {
		return []int{pid}
	}
	return nil
}

// Capabilities: turn/steer joins the running turn; thread/resume reopens the
// thread in a new app-server.
func (s *session) Capabilities() provider.Capabilities {
	return provider.Capabilities{Steer: true, Resume: true}
}

// Steer appends input to the running turn (turn/steer).
func (s *session) Steer(text string) error {
	s.mu.Lock()
	t := s.turn
	var id string
	if t != nil {
		id = t.id
	}
	s.mu.Unlock()
	if id == "" {
		return provider.ErrNoActiveTurn
	}
	ctx, cancel := context.WithTimeout(context.Background(), provider.SteerTimeout)
	defer cancel()
	_, err := s.conn.Call(ctx, "turn/steer", map[string]any{
		"threadId": s.threadID, "expectedTurnId": id, "input": textInput(text),
	})
	if err != nil && !s.turnActive(t) {
		return provider.ErrNoActiveTurn
	}
	return err
}

func (s *session) turnActive(t *activeTurn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn == t
}

// Cancel interrupts the running turn. Prompt waits for its turn/completed.
func (s *session) Cancel() error {
	s.mu.Lock()
	var id string
	if s.turn != nil {
		id = s.turn.id
	}
	s.mu.Unlock()
	if id != "" {
		go s.interrupt(s.threadID, id)
	}
	return nil
}

func (s *session) interrupt(threadID, turnID string) {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	if _, err := s.conn.Call(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}); err != nil {
		log.Printf("codex: turn/interrupt %s: %v", turnID, err)
	}
}

func (s *session) Close() error {
	err := s.conn.Close()
	s.release()
	return err
}

func (s *session) release() {
	s.closeOnce.Do(func() {
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}

func (s *session) Done() <-chan struct{} { return s.conn.Done() }

// ConfigFiles: the app-server reads its login and config once at start.
func (s *session) ConfigFiles() []string {
	home := configRoot
	for _, kv := range s.env {
		if v, ok := strings.CutPrefix(kv, "CODEX_HOME="); ok && v != "" {
			home = v
		}
	}
	return []string{filepath.Join(home, "auth.json"), filepath.Join(home, "config.toml")}
}

// ExitInfo reaps the app-server and composes the operator-facing banner.
func (s *session) ExitInfo() (string, error) {
	waitErr := s.conn.Wait()
	var sb strings.Builder
	sb.WriteString("Codex app-server 已退出。")
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			fmt.Fprintf(&sb, " 退出码=%d。", ee.ExitCode())
		} else {
			fmt.Fprintf(&sb, " %v。", waitErr)
		}
	}
	if re := s.conn.ReadErr(); re != nil {
		fmt.Fprintf(&sb, " stdout: %v。", re)
	}
	return sb.String(), waitErr
}

// Prompt runs one turn and blocks until turn/completed. A cancelled ctx
// interrupts the turn instead of stopping the process.
func (s *session) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	text, err := provider.WithAttachments(text, images)
	if err != nil {
		s.emitError(err.Error())
		s.emitPromptDone("failed", nil, false)
		return provider.TurnResult{StopReason: "failed"}, err
	}
	t := &activeTurn{done: make(chan turnEnd, 1), streamed: map[string]bool{}}
	s.mu.Lock()
	s.turn = t
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.turn == t {
			s.turn = nil
		}
		s.mu.Unlock()
	}()

	// Not bound to ctx: once sent, the app-server may already run the turn,
	// so a cancel must wait for its id, interrupt it and wait for the end.
	startCtx, cancelStart := context.WithTimeout(context.Background(), rpcTimeout)
	raw, err := s.conn.Call(startCtx, "turn/start", map[string]any{"threadId": s.threadID, "input": textInput(text)})
	cancelStart()
	if errors.Is(err, context.DeadlineExceeded) {
		// Unanswered: the turn may be running. Stopping the app-server is
		// the only way to be sure it is not; the bridge revives the thread.
		log.Printf("codex: turn/start 未在 %s 内应答，结束 app-server", rpcTimeout)
		_ = s.conn.Close()
	}
	if err != nil {
		return s.finishFailed(ctx, err)
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(raw, &started)
	s.mu.Lock()
	if t.id == "" {
		t.id = started.Turn.ID
	}
	s.mu.Unlock()

	var end turnEnd
	select {
	case end = <-t.done:
		if end.status == "completed" {
			if err := s.awaitSubagents(ctx, t); errors.Is(err, errSubagentHostExited) {
				end = turnEnd{status: "failed", errText: err.Error()}
			} else if err != nil {
				end = turnEnd{status: "interrupted"}
			}
		}
	case <-ctx.Done():
		s.interrupt(s.threadID, t.id)
		select {
		case end = <-t.done:
		case <-time.After(provider.InterruptGrace):
			log.Printf("codex: turn %s 未在 %s 内响应中断，结束 app-server", t.id, provider.InterruptGrace)
			_ = s.conn.Close()
			end = turnEnd{status: "interrupted"}
		case <-s.conn.Done():
			end = turnEnd{status: "interrupted"}
		}
		s.haltSubagents()
	case <-s.conn.Done():
		return s.finishFailed(ctx, errors.New("codex app-server exited during the turn"))
	}
	return s.finishTurn(ctx, t, end)
}

// subagentPoll is how often a finished turn re-checks the subagents it waits
// for. A var so tests can shrink it.
var subagentPoll = time.Second

// haltPoll is how often a halt re-checks that what it interrupted stopped.
const haltPoll = 100 * time.Millisecond

// awaitSubagents holds a completed turn until every subagent the thread
// spawned has stopped, so prompt_done never reports work as finished while
// background agents still change the workspace. It fails when ctx ends or the
// app-server exits first.
func (s *session) awaitSubagents(ctx context.Context, t *activeTurn) error {
	running := s.runningSubagents(ctx)
	if len(running) == 0 {
		return nil
	}
	callID := "subagents-" + t.id
	s.emitUpdate(map[string]any{"sessionUpdate": "tool_call", "toolCallId": callID, "status": "in_progress",
		"title": fmt.Sprintf("等待 %d 个子 Agent 完成", len(running))})
	tick := time.NewTicker(subagentPoll)
	defer tick.Stop()
	for len(running) > 0 {
		select {
		case <-ctx.Done():
			s.haltSubagents()
			s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": callID, "status": "failed"})
			return ctx.Err()
		case <-s.conn.Done():
			s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": callID, "status": "failed"})
			return errSubagentHostExited
		case <-tick.C:
			running = s.runningSubagents(ctx)
		}
	}
	s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": callID, "status": "completed"})
	return nil
}

var errSubagentHostExited = errors.New("codex app-server exited while subagents were running")

// haltSubagents interrupts every tracked subagent still running and returns
// once none is. Interrupts repeat on each poll, so a lost request is retried;
// subagents that do not stop within InterruptGrace take the app-server down,
// and the bridge revives the thread like a crash.
func (s *session) haltSubagents() {
	deadline := time.After(provider.InterruptGrace)
	for {
		running := s.runningSubagents(context.Background())
		if len(running) == 0 {
			return
		}
		for _, id := range running {
			if turn := s.activeTurnOf(id); turn != "" {
				s.interrupt(id, turn)
			}
		}
		select {
		case <-s.conn.Done():
			return
		case <-deadline:
			log.Printf("codex: 子 Agent %v 未在 %s 内停止，结束 app-server", running, provider.InterruptGrace)
			_ = s.conn.Close()
			return
		case <-time.After(haltPoll):
		}
	}
}

// activeTurnOf is the id of the thread's turn in progress, or "".
func (s *session) activeTurnOf(threadID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	raw, err := s.conn.Call(ctx, "thread/turns/list", map[string]any{"threadId": threadID, "limit": 1, "itemsView": "notLoaded"})
	if err != nil {
		log.Printf("codex: 读取子 Agent %s 的回合失败: %v", threadID, err)
		return ""
	}
	var page struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &page) != nil || len(page.Data) == 0 || page.Data[0].Status != "inProgress" {
		return ""
	}
	return page.Data[0].ID
}

// runningSubagents asks the app-server which tracked subagents are still
// active and forgets the others. A thread the app-server refuses to read is
// not waited for; one whose state is unknown (no answer) still is.
func (s *session) runningSubagents(ctx context.Context) []string {
	s.mu.Lock()
	ids := slices.Sorted(maps.Keys(s.subagents))
	s.mu.Unlock()
	var running []string
	for _, id := range ids {
		active, err := s.threadActive(ctx, id)
		var refused *acp.RPCError
		switch {
		case errors.As(err, &refused):
			log.Printf("codex: 子 Agent %s 不可读取，不再等待: %v", id, err)
		case err != nil:
			log.Printf("codex: 子 Agent %s 状态未知，继续等待: %v", id, err)
			active = true
		}
		if active {
			running = append(running, id)
			continue
		}
		s.mu.Lock()
		delete(s.subagents, id)
		s.mu.Unlock()
	}
	return running
}

func (s *session) threadActive(ctx context.Context, threadID string) (bool, error) {
	rctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	raw, err := s.conn.Call(rctx, "thread/read", map[string]any{"threadId": threadID})
	if err != nil {
		return false, err
	}
	var out struct {
		Thread struct {
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, err
	}
	return out.Thread.Status.Type == "active", nil
}

// finishTurn settles usage and stop reason, emits prompt_done and returns the
// error contract the bridge expects for each cancel cause.
func (s *session) finishTurn(ctx context.Context, t *activeTurn, end turnEnd) (provider.TurnResult, error) {
	// Late notifications for this turn may still be applied under s.mu.
	s.mu.Lock()
	usage, errText := t.usage(), t.errText
	s.mu.Unlock()
	partial := end.status != "completed"
	s.addCumulative(usage)
	res := provider.TurnResult{Usage: usage, UsagePartial: partial}
	switch end.status {
	case "completed":
		res.StopReason = "end_turn"
		s.emitPromptDone(res.StopReason, usage, partial)
		return res, nil
	case "interrupted":
		return s.finishInterrupted(ctx, usage)
	default:
		res.StopReason = "failed"
		msg := s.diagnostic(firstNonEmpty(end.errText, errText, "Codex 本轮执行失败"))
		s.emitError(msg)
		s.emitPromptDone(res.StopReason, usage, partial)
		return res, errors.New(msg)
	}
}

// finishInterrupted ends a turn stopped before it finished, by the cause its
// ctx was cancelled with.
func (s *session) finishInterrupted(ctx context.Context, usage map[string]provider.TokenUsage) (provider.TurnResult, error) {
	in := provider.InterruptionFor(context.Cause(ctx))
	if in.Diagnostic != "" {
		s.emitError(in.Diagnostic)
	}
	partial := len(usage) > 0
	if in.Final {
		s.emitPromptDone(in.StopReason, usage, partial)
	}
	return provider.TurnResult{StopReason: in.StopReason, Usage: usage, UsagePartial: partial}, in.Err
}

func (s *session) finishFailed(ctx context.Context, err error) (provider.TurnResult, error) {
	if ctx.Err() != nil {
		return s.finishInterrupted(ctx, nil)
	}
	msg := s.diagnostic(err.Error())
	s.emitError(msg)
	s.emitPromptDone("failed", nil, false)
	return provider.TurnResult{StopReason: "failed"}, errors.New(msg)
}

func (s *session) addCumulative(usage map[string]provider.TokenUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for model, u := range usage {
		c := s.cumUsage[model]
		c.InputTokens += u.InputTokens
		c.OutputTokens += u.OutputTokens
		c.CacheReadTokens += u.CacheReadTokens
		c.CacheWriteTokens += u.CacheWriteTokens
		s.cumUsage[model] = c
	}
}

// onRequest answers server requests. Approval policy "never" means the
// app-server does not ask for approvals; anything it does ask is declined so
// a turn can never block on a prompt no one will answer.
func (s *session) onRequest(_ context.Context, method string, id json.RawMessage, _ json.RawMessage) error {
	log.Printf("codex: 拒绝 app-server 请求 %s（无人值守会话）", method)
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return s.conn.RespondRaw(id, map[string]any{"decision": "decline"})
	case "execCommandApproval", "applyPatchApproval":
		return s.conn.RespondRaw(id, map[string]any{"decision": "denied"})
	case "mcpServer/elicitation/request":
		return s.conn.RespondRaw(id, map[string]any{"action": "decline"})
	default:
		return s.conn.RespondError(id, -32601, "not supported by this client: "+method)
	}
}

// diagnostic is the user-facing form of a Codex error: auth failures get
// actionable text, credentials are hidden.
func (s *session) diagnostic(text string) string {
	return provider.RedactDiagnostic(rewriteCodexAuthError(text), s.env)
}

func textInput(text string) []any {
	return []any{map[string]any{"type": "text", "text": text}}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
