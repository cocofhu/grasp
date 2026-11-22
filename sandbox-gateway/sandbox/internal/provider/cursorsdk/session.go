package cursorsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"backend/internal/provider"
)

// session is one bridge and the Cursor agent it runs.
type session struct {
	proc         *process
	opts         provider.OpenOptions
	env          []string
	onEventFrame func(json.RawMessage)
	model        selection

	cwd, fsRoot string
	agentID     string

	// turnMu serializes Prompt; one turn runs on the agent at a time.
	turnMu sync.Mutex

	mu        sync.Mutex
	turn      *activeTurn
	cumUsage  map[string]provider.TokenUsage
	openTools map[string]bool
	// lost is input of a run stopped before Cursor saved it: the next turn
	// sends it again, since the conversation does not have it.
	lost []string

	closeOnce sync.Once
}

// activeTurn is the running turn as seen through bridge events.
type activeTurn struct {
	done   chan struct{}
	ended  bool
	end    runEnd
	inputs []string
	// calls sums model-call-completed until run_end reports the total.
	calls provider.TokenUsage
}

// runEnd is the bridge's run_end event.
type runEnd struct {
	Status       string    `json:"status"`
	Usage        *sdkUsage `json:"usage"`
	Error        string    `json:"error"`
	Checkpointed *bool     `json:"checkpointed"`
}

// sdkUsage counts input including cache reads and writes.
type sdkUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

func (u sdkUsage) tokens() provider.TokenUsage {
	return provider.TokenUsage{
		InputTokens:      max(0, u.InputTokens-u.CacheReadTokens-u.CacheWriteTokens),
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}

func newSession(opts provider.OpenOptions, env []string, onEvent func(json.RawMessage), proc *process, model selection) *session {
	fsRoot := opts.FSRoot
	if fsRoot == "" {
		fsRoot = opts.Cwd
	}
	return &session{proc: proc, opts: opts, env: env, onEventFrame: onEvent, model: model, cwd: opts.Cwd, fsRoot: fsRoot,
		cumUsage: map[string]provider.TokenUsage{}, openTools: map[string]bool{}}
}

// open creates the agent, or resumes the one opts names.
func (s *session) open(ctx context.Context, key string, mcp map[string]any) error {
	cmd := map[string]any{"type": "open", "cwd": s.cwd, "model": s.model, "apiKey": key}
	if s.opts.ResumeSessionID != "" {
		cmd["agentId"] = s.opts.ResumeSessionID
	}
	if len(mcp) > 0 {
		cmd["mcpServers"] = mcp
	}
	r, err := s.proc.call(ctx, cmd)
	if err != nil {
		if errors.Is(err, errExited) {
			return fmt.Errorf("Cursor SDK 桥接进程启动时退出: %s", s.proc.exitText())
		}
		return err
	}
	var opened struct {
		AgentID     string `json:"agentId"`
		Resumed     bool   `json:"resumed"`
		ResumeError string `json:"resumeError"`
	}
	if err := json.Unmarshal(r.Data, &opened); err != nil || opened.AgentID == "" {
		return errors.New("Cursor SDK 没有返回 agent id")
	}
	if s.opts.ResumeSessionID != "" && !opened.Resumed {
		log.Printf("cursor: 接回 agent %s 失败，改开新会话: %s", s.opts.ResumeSessionID, opened.ResumeError)
	}
	s.agentID = opened.AgentID
	return nil
}

// SessionID is the Cursor agent id, which a new bridge resumes.
func (s *session) SessionID() string { return s.agentID }
func (s *session) CWD() string       { return s.cwd }
func (s *session) FSRoot() string    { return s.fsRoot }

func (s *session) Info() provider.AgentInfo {
	return provider.AgentInfo{Name: string(provider.Cursor), Title: "Cursor", ModelID: s.model.ID, ModelName: s.model.label()}
}

func (s *session) Capabilities() provider.Capabilities {
	return provider.Capabilities{Steer: true, Resume: true}
}

func (s *session) ReportsUsage() bool { return true }

func (s *session) CumulativeUsage() map[string]provider.TokenUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return provider.CopyUsage(s.cumUsage)
}

func (s *session) AgentPIDs() []int { return []int{s.proc.pid()} }

// ConfigFiles: the agent reads the user's MCP servers, rules and CLI config
// when it starts.
func (s *session) ConfigFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".cursor")
	return []string{filepath.Join(dir, "mcp.json"), filepath.Join(dir, "cli-config.json")}
}

// Prompt runs one turn. A cancelled ctx cancels the run and waits for it to
// stop; a bridge that does not stop in time is killed.
func (s *session) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	text, err := provider.WithAttachments(text, images)
	if err != nil {
		return s.finishFailed(ctx, err)
	}
	s.mu.Lock()
	// The replayed input rides on this turn, so a run Cursor drops again
	// replays it again.
	t := &activeTurn{done: make(chan struct{}), inputs: append(slices.Clone(s.lost), text)}
	message := withLost(s.lost, text)
	s.turn = t
	s.mu.Unlock()
	if _, err := s.proc.call(ctx, map[string]any{"type": "send", "text": message}); err != nil {
		s.endTurn()
		if errors.Is(err, errExited) {
			err = fmt.Errorf("Cursor SDK 桥接进程已退出: %s", s.proc.exitText())
		}
		return s.finishFailed(ctx, err)
	}
	s.mu.Lock()
	s.lost = nil
	s.mu.Unlock()
	select {
	case <-t.done:
		return s.finishTurn(ctx, t)
	case <-ctx.Done():
		s.cancelRun(t)
		return s.finishTurn(ctx, t)
	case <-s.proc.done:
		s.endTurn()
		return s.finishFailed(ctx, fmt.Errorf("Cursor SDK 桥接进程在回合中退出: %s", s.proc.exitText()))
	}
}

// withLost puts input Cursor dropped ahead of the new message.
func withLost(lost []string, text string) string {
	if len(lost) == 0 {
		return text
	}
	return "The previous request was stopped before it reached this conversation, so here it is again:\n\n" +
		strings.Join(lost, "\n\n") + "\n\nNew message:\n\n" + text
}

// cancelRun asks the bridge to cancel the run and waits for it to end; a
// bridge that does not end it within the grace is killed.
func (s *session) cancelRun(t *activeTurn) {
	ctx, cancel := context.WithTimeout(context.Background(), provider.InterruptGrace)
	defer cancel()
	if _, err := s.proc.call(ctx, map[string]any{"type": "cancel"}); err != nil && !errors.Is(err, errExited) {
		log.Printf("cursor: 中断失败: %v", err)
	}
	select {
	case <-t.done:
	case <-s.proc.done:
	case <-ctx.Done():
		log.Printf("cursor: 回合未在 %s 内停止，结束桥接进程", provider.InterruptGrace)
		s.proc.stop()
	}
}

// Steer sends text into the running run. Cursor either takes it into the
// run, queues it as a follow-up run the turn then includes, or hands it back.
func (s *session) Steer(text string) error {
	s.mu.Lock()
	t := s.turn
	s.mu.Unlock()
	if t == nil {
		return provider.ErrNoActiveTurn
	}
	ctx, cancel := context.WithTimeout(context.Background(), provider.SteerTimeout)
	defer cancel()
	r, err := s.proc.call(ctx, map[string]any{"type": "steer", "text": text})
	if err != nil {
		return err
	}
	var ack struct {
		Outcome string `json:"outcome"`
	}
	_ = json.Unmarshal(r.Data, &ack)
	switch ack.Outcome {
	case "complete_delivered", "queued":
		s.mu.Lock()
		t.inputs = append(t.inputs, text)
		s.mu.Unlock()
		return nil
	case "no_turn":
		return provider.ErrNoActiveTurn
	default:
		return provider.ErrSteerDeclined
	}
}

// Cancel cancels the running run; Prompt waits for it to end.
func (s *session) Cancel() error {
	s.mu.Lock()
	running := s.turn != nil
	s.mu.Unlock()
	if running {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), provider.InterruptGrace)
			defer cancel()
			if _, err := s.proc.call(ctx, map[string]any{"type": "cancel"}); err != nil {
				log.Printf("cursor: 中断失败: %v", err)
			}
		}()
	}
	return nil
}

// finishTurn settles usage and stop reason, emits prompt_done and returns the
// error contract the bridge expects for each cancel cause.
func (s *session) finishTurn(ctx context.Context, t *activeTurn) (provider.TurnResult, error) {
	s.endTurn()
	s.mu.Lock()
	end, ended := t.end, t.ended
	u := t.calls
	if end.Usage != nil {
		u = end.Usage.tokens()
	}
	var usage map[string]provider.TokenUsage
	if u != (provider.TokenUsage{}) {
		usage = map[string]provider.TokenUsage{s.model.ID: u}
		provider.AddUsage(s.cumUsage, usage)
	}
	if end.Status == "cancelled" && end.Checkpointed != nil && !*end.Checkpointed {
		s.lost = append(s.lost, t.inputs...)
	}
	s.mu.Unlock()
	if ctx.Err() != nil || !ended || end.Status == "cancelled" {
		if ctx.Err() == nil {
			ctx = cancelledContext()
		}
		return provider.EndInterrupted(ctx, s.emit, s.agentID, usage)
	}
	res := provider.TurnResult{Usage: usage}
	if end.Status != "finished" {
		res.StopReason = "failed"
		msg := provider.RedactDiagnostic(strings.TrimSpace("Cursor 回合失败: "+end.Error), s.env)
		s.emit(provider.ErrorFrame(msg))
		res.UsagePartial = len(usage) > 0
		s.emit(provider.PromptDoneFrame(s.agentID, res.StopReason, usage, res.UsagePartial))
		return res, errors.New(msg)
	}
	res.StopReason = "end_turn"
	s.emit(provider.PromptDoneFrame(s.agentID, res.StopReason, usage, false))
	return res, nil
}

// cancelledContext reports a run Cursor cancelled on its own as a plain stop.
func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func (s *session) finishFailed(ctx context.Context, err error) (provider.TurnResult, error) {
	return provider.EndFailed(ctx, s.emit, s.agentID, provider.RedactDiagnostic(err.Error(), s.env))
}

// endTurn detaches the turn and closes the tool calls it left running.
func (s *session) endTurn() {
	s.mu.Lock()
	s.turn = nil
	open := make([]string, 0, len(s.openTools))
	for id := range s.openTools {
		open = append(open, id)
	}
	clear(s.openTools)
	s.mu.Unlock()
	sort.Strings(open)
	for _, id := range open {
		s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "failed"})
	}
}

func (s *session) Done() <-chan struct{} { return s.proc.done }

func (s *session) Close() error {
	s.closeOnce.Do(s.proc.stop)
	return nil
}

// ExitInfo reaps the process and composes the operator-facing banner.
func (s *session) ExitInfo() (string, error) {
	<-s.proc.done
	msg := "Cursor 已退出。"
	if text := s.proc.exitText(); text != "" {
		msg += " " + provider.RedactDiagnostic(text, s.env) + "。"
	}
	return msg, s.proc.waitErr
}
