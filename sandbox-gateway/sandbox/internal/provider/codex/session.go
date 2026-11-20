package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os/exec"
	"strings"
	"sync"
	"time"

	"backend/internal/acp"
	"backend/internal/provider"
)

const clientName = "grasp"

// interruptGrace bounds how long a cancelled turn may take to report
// turn/completed before the app-server is presumed hung and stopped. A var so
// tests can shrink it.
var interruptGrace = 15 * time.Second

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
		cumUsage: map[string]provider.TokenUsage{}}
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
		// A resumed thread keeps its recorded model and reasoning effort
		// unless the model is named; naming it applies this process's -c
		// settings, so a changed or cleared effort takes effect.
		if _, named := resume["model"]; !named {
			if model := s.effectiveModel(ctx); model != "" {
				resume["model"] = model
			}
		}
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

// effectiveModel is the model a new thread would use: the configured one,
// else the catalog default. Empty when Codex reports neither.
func (s *session) effectiveModel(ctx context.Context) string {
	if raw, err := s.conn.Call(ctx, "config/read", map[string]any{}); err == nil {
		var res struct {
			Config struct {
				Model string `json:"model"`
			} `json:"config"`
		}
		if json.Unmarshal(raw, &res) == nil && res.Config.Model != "" {
			return res.Config.Model
		}
	}
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
	s.mu.Unlock()
	if t == nil || t.id == "" {
		return provider.ErrNoActiveTurn
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	_, err := s.conn.Call(ctx, "turn/steer", map[string]any{
		"threadId": s.threadID, "expectedTurnId": t.id, "input": textInput(text),
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
	t := s.turn
	s.mu.Unlock()
	if t == nil || t.id == "" {
		return nil
	}
	go s.interrupt(t.id)
	return nil
}

func (s *session) interrupt(turnID string) {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	if _, err := s.conn.Call(ctx, "turn/interrupt", map[string]any{"threadId": s.threadID, "turnId": turnID}); err != nil {
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
	if len(images) > 0 {
		files, err := provider.MaterializeAttachments(images)
		if err != nil {
			s.emitError(err.Error())
			s.emitPromptDone("failed", nil, false)
			return provider.TurnResult{StopReason: "failed"}, err
		}
		text = provider.AppendAttachmentRefs(text, files)
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

	startCtx, cancelStart := context.WithTimeout(ctx, rpcTimeout)
	raw, err := s.conn.Call(startCtx, "turn/start", map[string]any{"threadId": s.threadID, "input": textInput(text)})
	cancelStart()
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
	case <-ctx.Done():
		s.interrupt(t.id)
		select {
		case end = <-t.done:
		case <-time.After(interruptGrace):
			log.Printf("codex: turn %s 未在 %s 内响应中断，结束 app-server", t.id, interruptGrace)
			_ = s.conn.Close()
			end = turnEnd{status: "interrupted"}
		case <-s.conn.Done():
			end = turnEnd{status: "interrupted"}
		}
	case <-s.conn.Done():
		return s.finishFailed(ctx, errors.New("codex app-server exited during the turn"))
	}
	return s.finishTurn(ctx, t, end)
}

// finishTurn settles usage and stop reason, emits prompt_done and returns the
// error contract the bridge expects for each cancel cause.
func (s *session) finishTurn(ctx context.Context, t *activeTurn, end turnEnd) (provider.TurnResult, error) {
	usage := t.usage()
	partial := end.status != "completed"
	s.addCumulative(usage)
	res := provider.TurnResult{Usage: usage, UsagePartial: partial}
	cause := context.Cause(ctx)
	switch end.status {
	case "completed":
		res.StopReason = "end_turn"
		s.emitPromptDone(res.StopReason, usage, partial)
		return res, nil
	case "interrupted":
		switch {
		case errors.Is(cause, provider.ErrTurnRecover):
			// The bridge continues this turn; it must not look finished.
			res.StopReason = "recover"
			return res, cause
		case errors.Is(cause, provider.ErrTurnTimeout):
			res.StopReason = provider.TimeoutStopReason(cause)
			s.emitError(cause.Error())
			s.emitPromptDone(res.StopReason, usage, partial)
			return res, cause
		}
		res.StopReason = "cancelled"
		s.emitPromptDone(res.StopReason, usage, partial)
		return res, context.Canceled
	default:
		res.StopReason = "failed"
		msg := s.diagnostic(firstNonEmpty(end.errText, t.errText, "Codex 本轮执行失败"))
		s.emitError(msg)
		s.emitPromptDone(res.StopReason, usage, partial)
		return res, errors.New(msg)
	}
}

func (s *session) finishFailed(ctx context.Context, err error) (provider.TurnResult, error) {
	if ctx.Err() != nil {
		s.emitPromptDone("cancelled", nil, false)
		return provider.TurnResult{StopReason: "cancelled"}, context.Canceled
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
