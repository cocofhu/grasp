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
	// strays counts reasons Pi may hold input no turn accounts for: input
	// it took in after its turn ended, or steers still queued when a turn was
	// stopped or failed (Pi keeps its queue across an abort and hands it to
	// the next run). The next holder of turnMu withdraws it first.
	strays int

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
	return provider.CopyUsage(s.cumUsage)
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
	text, err := provider.WithAttachments(text, images)
	if err != nil {
		return s.finishFailed(ctx, err)
	}
	if err := s.dropStrays(ctx); err != nil {
		return s.finishFailed(ctx, fmt.Errorf("pi: 无法撤回上一回合结束后送达的引导: %w", err))
	}
	t := &activeTurn{done: make(chan struct{}), posted: 1, usage: map[string]provider.TokenUsage{}}
	s.mu.Lock()
	s.turn = t
	s.text = sanitizer{}
	s.mu.Unlock()
	if err := s.sendPrompt(ctx, text); err != nil {
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

// sendPrompt starts the turn's run. Pi may still be closing the previous run
// after its agent_end; a prompt it refuses as busy is sent again once it
// reports idle.
func (s *session) sendPrompt(ctx context.Context, text string) error {
	cmd := map[string]any{"type": "prompt", "message": text}
	_, err := s.proc.call(ctx, cmd)
	if err == nil || !strings.Contains(err.Error(), "Agent is already processing") {
		return err
	}
	if _, idle := s.idleState(ctx, func() bool { return ctx.Err() == nil }); !idle {
		return err
	}
	_, err = s.proc.call(ctx, cmd)
	return err
}

// agentState is the part of get_state that tells whether the agent is idle.
type agentState struct {
	IsStreaming  bool `json:"isStreaming"`
	PendingCount int  `json:"pendingMessageCount"`
}

// idleState asks the agent for its state until it reports it is not
// streaming, at most queueChecks times and while keep holds.
func (s *session) idleState(ctx context.Context, keep func() bool) (agentState, bool) {
	var state agentState
	for i := 0; i < queueChecks; i++ {
		if i > 0 {
			time.Sleep(queueCheckDelay)
		}
		if !keep() {
			return state, false
		}
		r, err := s.proc.call(ctx, map[string]any{"type": "get_state"})
		if err == nil && json.Unmarshal(r.Data, &state) == nil && !state.IsStreaming {
			return state, true
		}
	}
	return state, false
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
//
// Pi answers a prompt once its preflight decided it, and always answers.
// An answer slower than steerTimeout is not a refusal: the input is
// reported as part of the turn, which stays open until the answer arrives
// and fails if Pi refused it, so it is neither run twice nor dropped.
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
	answer := make(chan steerAnswer, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), steerAnswerLimit)
		defer cancel()
		r, err := s.proc.call(ctx, map[string]any{"type": "prompt", "message": text, "streamingBehavior": "steer"})
		answer <- steerAnswer{r, err}
	}()
	select {
	case a := <-answer:
		return s.steerAnswered(t, text, a, false)
	case <-time.After(steerTimeout):
		log.Printf("pi: 引导未在 %s 内应答，回合等待其结果", steerTimeout)
		go func() { _ = s.steerAnswered(t, text, <-answer, true) }()
		return nil
	}
}

// steerTimeout bounds how long Steer waits for Pi's answer; past it the turn
// waits for the answer, at most steerAnswerLimit, after which an agent that
// never answered is taken to have refused. Vars so tests can shrink them.
var (
	steerTimeout     = provider.SteerTimeout
	steerAnswerLimit = 10 * provider.SteerTimeout
)

type steerAnswer struct {
	r   response
	err error
}

// steerAnswered applies Pi's answer to a steer. A late refusal fails the
// turn it was reported in. A turn settles with steers unanswered only when
// it was stopped or failed; input Pi takes in after that belongs to no turn
// and is withdrawn.
func (s *session) steerAnswered(t *activeTurn, text string, a steerAnswer, late bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.inflight--
	if a.err != nil {
		t.posted--
		switch {
		case t.settled:
			log.Printf("pi: 回合结束后引导被拒绝，未送达: %v", a.err)
		case late && t.errText == "":
			t.errText = fmt.Sprintf("pi: 引导未送达: %v", a.err)
		}
		s.evaluateLocked(t)
		return a.err
	}
	t.steers = append(t.steers, text)
	if t.settled {
		s.strays++
		go s.withdrawStrays()
	}
	s.evaluateLocked(t)
	return nil
}

// withdrawStrays withdraws stray input unless a turn that started first
// already did. Holding turnMu keeps it from touching a newer turn.
func (s *session) withdrawStrays() {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.dropStrays(ctx); err != nil {
		log.Printf("pi: 撤回回合结束后送达的引导失败，下一回合开始前重试: %v", err)
	}
}

// dropStrays undoes input Pi took in after its turn ended: a run it started
// is aborted, and the session is reloaded, which keeps the history but drops
// Pi's queue (the RPC has no command that clears it). The caller holds turnMu.
func (s *session) dropStrays(ctx context.Context) error {
	s.mu.Lock()
	n := s.strays
	s.mu.Unlock()
	if n == 0 {
		return nil
	}
	r, err := s.proc.call(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		return err
	}
	var state agentState
	if err := json.Unmarshal(r.Data, &state); err != nil {
		return err
	}
	if state.IsStreaming {
		if _, err := s.proc.call(ctx, map[string]any{"type": "abort"}); err != nil {
			return err
		}
	}
	if state.IsStreaming || state.PendingCount > 0 {
		if err := s.reloadSession(ctx); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.strays -= n
	s.mu.Unlock()
	log.Printf("pi: 回合结束后才送达的 %d 条引导已撤回", n)
	return nil
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

// queueChecks bounds how often idleState asks the agent for its state: Pi
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
	state, known := s.idleState(ctx, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !t.settled && t.ended
	})
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

// redeliver clears the agent's queue and sends the withdrawn input as a
// prompt.
func (s *session) redeliver(ctx context.Context, text string) error {
	if err := s.reloadSession(ctx); err != nil {
		return err
	}
	_, err := s.proc.call(ctx, map[string]any{"type": "prompt", "message": text, "streamingBehavior": "steer"})
	return err
}

// reloadSession reopens the agent's own session file: the history stays,
// queued messages are dropped.
func (s *session) reloadSession(ctx context.Context) error {
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
	return nil
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
		provider.AddUsage(s.cumUsage, usage)
	}
	errText := t.errText
	if (interrupted || errText != "") && t.users < t.posted {
		s.strays++
	}
	s.mu.Unlock()
	if interrupted {
		return provider.EndInterrupted(ctx, s.emit, s.sessionFile, usage)
	}
	res := provider.TurnResult{Usage: usage}
	if errText != "" {
		res.StopReason = "failed"
		res.UsagePartial = len(usage) > 0
		msg := provider.RedactDiagnostic(errText, s.env)
		s.emitError(msg)
		s.emitPromptDone(res.StopReason, usage, res.UsagePartial)
		return res, errors.New(msg)
	}
	res.StopReason = "end_turn"
	s.emitPromptDone(res.StopReason, usage, false)
	return res, nil
}

func (s *session) finishFailed(ctx context.Context, err error) (provider.TurnResult, error) {
	return provider.EndFailed(ctx, s.emit, s.sessionFile, provider.RedactDiagnostic(err.Error(), s.env))
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
