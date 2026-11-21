package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/internal/provider"
)

// session is one `pi --mode rpc` process and the session file it works on.
type session struct {
	proc         *process
	opts         provider.OpenOptions
	env          []string
	onEventFrame func(json.RawMessage)
	cleanup      func()

	cwd, fsRoot string
	sessionFile string
	model       string

	// turnMu serializes Prompt; one turn runs on the session at a time.
	turnMu sync.Mutex

	mu        sync.Mutex
	turn      *activeTurn
	text      sanitizer
	cumUsage  map[string]provider.TokenUsage
	openTools map[string]bool

	closeOnce sync.Once
}

// activeTurn is the running turn as seen through events. A turn spans every
// input sent into it: the prompt, then each steer, which may join the run or
// start another one when it arrives as the run ends.
type activeTurn struct {
	done     chan struct{}
	posted   int // inputs sent into the turn
	inflight int // steers not yet answered
	users    int // user messages the agent took in
	steers   []string
	ended    bool
	checking bool
	aborted  bool
	settled  bool
	errText  string
	usage    map[string]provider.TokenUsage
}

func newSession(opts provider.OpenOptions, env []string, onEvent func(json.RawMessage), cleanup func()) *session {
	fsRoot := opts.FSRoot
	if fsRoot == "" {
		fsRoot = opts.Cwd
	}
	return &session{opts: opts, env: env, onEventFrame: onEvent, cleanup: cleanup, cwd: opts.Cwd, fsRoot: fsRoot,
		cumUsage: map[string]provider.TokenUsage{}, openTools: map[string]bool{}}
}

// handshake reads the session the process opened.
func (s *session) handshake(ctx context.Context) error {
	r, err := s.proc.call(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		if errors.Is(err, errExited) {
			return fmt.Errorf("pi rpc exited during startup: %s", s.proc.exitText())
		}
		return err
	}
	var state struct {
		SessionFile string `json:"sessionFile"`
		Model       *struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
		} `json:"model"`
	}
	if err := json.Unmarshal(r.Data, &state); err != nil || state.SessionFile == "" {
		return fmt.Errorf("pi get_state: no session file")
	}
	s.sessionFile = state.SessionFile
	if state.Model != nil {
		s.model = state.Model.Provider + "/" + state.Model.ID
	}
	return nil
}

// SessionID is the session file, which a new process resumes.
func (s *session) SessionID() string { return s.sessionFile }
func (s *session) CWD() string       { return s.cwd }
func (s *session) FSRoot() string    { return s.fsRoot }

func (s *session) Info() provider.AgentInfo {
	return provider.AgentInfo{Name: string(provider.Pi), Title: "Pi", ModelID: s.model, ModelName: s.model}
}

func (s *session) Capabilities() provider.Capabilities {
	return provider.Capabilities{Steer: true, Resume: true}
}

func (s *session) ReportsUsage() bool { return true }

func (s *session) CumulativeUsage() map[string]provider.TokenUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]provider.TokenUsage, len(s.cumUsage))
	for k, v := range s.cumUsage {
		out[k] = v
	}
	return out
}

func (s *session) AgentPIDs() []int { return []int{s.proc.pid()} }

// ConfigFiles: Pi reads its settings, model definitions and logins at start.
func (s *session) ConfigFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".pi", "agent")
	return []string{filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json"), filepath.Join(dir, "auth.json")}
}

// Prompt runs one turn. A cancelled ctx aborts the run and waits for the
// agent to stop; an agent that does not is killed.
func (s *session) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	text, err := withAttachments(text, images)
	if err != nil {
		return s.finishFailed(ctx, err)
	}
	t := &activeTurn{done: make(chan struct{}), posted: 1, usage: map[string]provider.TokenUsage{}}
	s.mu.Lock()
	s.turn = t
	s.text = sanitizer{}
	s.mu.Unlock()
	if _, err := s.proc.call(ctx, map[string]any{"type": "prompt", "message": text}); err != nil {
		s.endTurn()
		return s.finishFailed(ctx, err)
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		s.abort(t)
		return s.finishTurn(ctx, t, true)
	case <-s.proc.done:
		s.endTurn()
		return s.finishFailed(ctx, fmt.Errorf("pi rpc exited during the turn: %s", s.proc.exitText()))
	}
	s.mu.Lock()
	aborted := t.aborted
	s.mu.Unlock()
	return s.finishTurn(ctx, t, aborted)
}

// abort stops the run and waits for the agent to confirm, then for the run
// to settle; an agent that does neither within the grace is killed.
func (s *session) abort(t *activeTurn) {
	ctx, cancel := context.WithTimeout(context.Background(), provider.InterruptGrace)
	defer cancel()
	if _, err := s.proc.call(ctx, map[string]any{"type": "abort"}); err != nil {
		if !errors.Is(err, errExited) {
			log.Printf("pi: 中断未在 %s 内确认，结束进程: %v", provider.InterruptGrace, err)
			s.proc.stop()
		}
		return
	}
	// The abort answers once the agent is idle.
	s.mu.Lock()
	t.aborted = true
	s.settleLocked(t)
	s.mu.Unlock()
}

// Steer sends text into the running turn. Pi queues it before the next
// model call; one that arrives as the run ends starts the next run, and the
// turn lasts until the agent took it in.
func (s *session) Steer(text string) error {
	s.mu.Lock()
	t := s.turn
	if t == nil || t.settled {
		s.mu.Unlock()
		return provider.ErrNoActiveTurn
	}
	t.posted++
	t.inflight++
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), provider.SteerTimeout)
	defer cancel()
	_, err := s.proc.call(ctx, map[string]any{"type": "prompt", "message": text, "streamingBehavior": "steer"})
	s.mu.Lock()
	t.inflight--
	if err != nil {
		t.posted--
	} else {
		t.steers = append(t.steers, text)
	}
	s.evaluateLocked(t)
	s.mu.Unlock()
	return err
}

// Cancel aborts the running run. Prompt waits for it to settle.
func (s *session) Cancel() error {
	s.mu.Lock()
	running := s.turn != nil
	s.mu.Unlock()
	if running {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), provider.InterruptGrace)
			defer cancel()
			if _, err := s.proc.call(ctx, map[string]any{"type": "abort"}); err != nil {
				log.Printf("pi: 中断失败: %v", err)
			}
		}()
	}
	return nil
}

// evaluateLocked settles the turn when the agent's run ended and took in
// every input sent into it.
func (s *session) evaluateLocked(t *activeTurn) {
	if t.settled || !t.ended {
		return
	}
	if t.aborted || t.errText != "" {
		s.settleLocked(t)
		return
	}
	if t.inflight > 0 {
		return
	}
	if t.users >= t.posted {
		s.settleLocked(t)
		return
	}
	if !t.checking {
		t.checking = true
		go s.checkQueued(t)
	}
}

// queueChecks bounds how often checkQueued asks the agent for its queue: Pi
// may still report streaming briefly after agent_end, and a query may fail.
// Vars so tests can shrink them.
var (
	queueChecks     = 50
	queueCheckDelay = 100 * time.Millisecond
)

// checkQueued resolves a run that ended with a steer not taken in: either
// the steer started the next run, or it reached the queue just after the
// agent's last check. A queued steer would only reach the model with the
// next prompt, so it is withdrawn and sent again as a run of this turn.
func (s *session) checkQueued(t *activeTurn) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var state struct {
		IsStreaming  bool `json:"isStreaming"`
		PendingCount int  `json:"pendingMessageCount"`
	}
	known := false
	for i := 0; i < queueChecks && !known; i++ {
		if i > 0 {
			time.Sleep(queueCheckDelay)
		}
		s.mu.Lock()
		idle := !t.settled && t.ended
		s.mu.Unlock()
		if !idle {
			break
		}
		r, err := s.proc.call(ctx, map[string]any{"type": "get_state"})
		known = err == nil && json.Unmarshal(r.Data, &state) == nil && !state.IsStreaming
	}
	s.mu.Lock()
	t.checking = false
	if t.settled || !t.ended {
		s.mu.Unlock()
		return
	}
	if !known {
		t.errText = "pi: 回合结束时无法确认引导是否送达"
		s.settleLocked(t)
		s.mu.Unlock()
		return
	}
	n := min(state.PendingCount, len(t.steers))
	if n == 0 {
		s.settleLocked(t)
		s.mu.Unlock()
		return
	}
	stranded := strings.Join(t.steers[len(t.steers)-n:], "\n\n")
	t.posted += 1 - n
	t.inflight++
	s.mu.Unlock()

	err := s.redeliver(ctx, stranded)
	s.mu.Lock()
	defer s.mu.Unlock()
	t.inflight--
	if err != nil && t.errText == "" {
		t.errText = fmt.Sprintf("pi: 回合结束时排队的引导无法送达: %v", err)
	}
	s.evaluateLocked(t)
}

// redeliver clears the agent's queue by reloading its own session, which
// keeps the history, and sends the withdrawn input as a prompt.
func (s *session) redeliver(ctx context.Context, text string) error {
	r, err := s.proc.call(ctx, map[string]any{"type": "switch_session", "sessionPath": s.sessionFile})
	if err != nil {
		return err
	}
	var res struct {
		Cancelled bool `json:"cancelled"`
	}
	if json.Unmarshal(r.Data, &res) == nil && res.Cancelled {
		return errors.New("session reload was cancelled")
	}
	_, err = s.proc.call(ctx, map[string]any{"type": "prompt", "message": text, "streamingBehavior": "steer"})
	return err
}

func (s *session) settleLocked(t *activeTurn) {
	if t.settled {
		return
	}
	t.settled = true
	close(t.done)
}

// finishTurn settles usage and stop reason, emits prompt_done and returns the
// error contract the bridge expects for each cancel cause.
func (s *session) finishTurn(ctx context.Context, t *activeTurn, interrupted bool) (provider.TurnResult, error) {
	s.endTurn()
	s.mu.Lock()
	var usage map[string]provider.TokenUsage
	if len(t.usage) > 0 {
		usage = t.usage
		for model, u := range t.usage {
			c := s.cumUsage[model]
			c.InputTokens += u.InputTokens
			c.OutputTokens += u.OutputTokens
			c.CacheReadTokens += u.CacheReadTokens
			c.CacheWriteTokens += u.CacheWriteTokens
			s.cumUsage[model] = c
		}
	}
	errText := t.errText
	s.mu.Unlock()
	res := provider.TurnResult{Usage: usage, UsagePartial: interrupted}
	cause := context.Cause(ctx)
	switch {
	case interrupted && errors.Is(cause, provider.ErrTurnRecover):
		res.StopReason = "recover"
		return res, cause
	case interrupted && errors.Is(cause, provider.ErrTurnTimeout):
		res.StopReason = provider.TimeoutStopReason(cause)
		s.emitError(cause.Error())
		s.emitPromptDone(res.StopReason, usage, true)
		return res, cause
	case interrupted:
		res.StopReason = "cancelled"
		s.emitPromptDone(res.StopReason, usage, true)
		return res, context.Canceled
	case errText != "":
		res.StopReason = "failed"
		msg := provider.RedactDiagnostic(errText, s.env)
		s.emitError(msg)
		s.emitPromptDone(res.StopReason, usage, false)
		return res, errors.New(msg)
	}
	res.StopReason = "end_turn"
	s.emitPromptDone(res.StopReason, usage, false)
	return res, nil
}

func (s *session) finishFailed(ctx context.Context, err error) (provider.TurnResult, error) {
	if ctx.Err() != nil {
		s.emitPromptDone("cancelled", nil, false)
		return provider.TurnResult{StopReason: "cancelled"}, context.Canceled
	}
	msg := provider.RedactDiagnostic(err.Error(), s.env)
	s.emitError(msg)
	s.emitPromptDone("failed", nil, false)
	return provider.TurnResult{StopReason: "failed"}, errors.New(msg)
}

// endTurn detaches the turn, flushes held-back text and closes the tool
// calls it left running.
func (s *session) endTurn() {
	s.mu.Lock()
	s.turn = nil
	rest := s.text.flush()
	open := make([]string, 0, len(s.openTools))
	for id := range s.openTools {
		open = append(open, id)
	}
	clear(s.openTools)
	s.mu.Unlock()
	if rest != "" {
		s.emitChunk("agent_message_chunk", rest)
	}
	sort.Strings(open)
	for _, id := range open {
		s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "failed"})
	}
}

func (s *session) Done() <-chan struct{} { return s.proc.done }

func (s *session) Close() error {
	s.proc.stop()
	s.release()
	return nil
}

func (s *session) release() {
	s.closeOnce.Do(func() {
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}

// ExitInfo reaps the process and composes the operator-facing banner.
func (s *session) ExitInfo() (string, error) {
	<-s.proc.done
	s.release()
	msg := "Pi 已退出。"
	if text := s.proc.exitText(); text != "" {
		msg += " " + provider.RedactDiagnostic(text, s.env) + "。"
	}
	return msg, s.proc.waitErr
}

func withAttachments(text string, images []provider.PromptImage) (string, error) {
	if len(images) == 0 {
		return text, nil
	}
	files, err := provider.MaterializeAttachments(images)
	if err != nil {
		return "", err
	}
	return provider.AppendAttachmentRefs(text, files), nil
}
