package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/internal/envutil"
	"backend/internal/provider"
)

// session is one `opencode serve` process and the native session it runs.
type session struct {
	srv          *server
	opts         provider.OpenOptions
	env          []string
	onEventFrame func(json.RawMessage)
	cleanup      func()

	cwd, fsRoot string
	version     string
	// model routes every prompt; empty leaves OpenCode's configured default.
	model        modelRef
	variant      string
	displayModel string
	id           string

	// turnMu serializes Prompt; one turn runs on the session at a time.
	turnMu sync.Mutex

	mu       sync.Mutex
	turn     *activeTurn
	roles    map[string]string // message id -> role, this session only
	parts    map[string]*partState
	cumUsage map[string]provider.TokenUsage
	// openTools are tool calls shown as running; a turn that ends before
	// they report closes them as failed.
	openTools map[string]bool
	// unstarted are child sessions created but not yet reporting a status,
	// by creation time: a background task returns before its child turns
	// busy, and /session/status does not list it until then.
	unstarted map[string]time.Time

	streamCancel context.CancelFunc
	done         chan struct{}
	doneOnce     sync.Once
	closeOnce    sync.Once
}

type modelRef struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// activeTurn is the running turn as seen through events. A turn spans every
// prompt posted into it: the first, then each steer.
type activeTurn struct {
	done chan struct{}
	// since is when the turn started, in the server's millisecond clock.
	since     int64
	posted    int
	users     map[string]bool
	userOrder []string
	answered  map[string]bool
	busy      bool
	idleSeen  bool
	aborted   bool
	settled   bool
	errText   string
	usage     provider.TokenUsage
	reported  bool
}

func newSession(srv *server, opts provider.OpenOptions, env []string, onEvent func(json.RawMessage), cleanup func()) *session {
	fsRoot := opts.FSRoot
	if fsRoot == "" {
		fsRoot = opts.Cwd
	}
	return &session{
		srv: srv, opts: opts, env: env, onEventFrame: onEvent, cleanup: cleanup, cwd: opts.Cwd, fsRoot: fsRoot,
		roles: map[string]string{}, parts: map[string]*partState{}, cumUsage: map[string]provider.TokenUsage{},
		openTools: map[string]bool{}, unstarted: map[string]time.Time{}, done: make(chan struct{}),
	}
}

// handshake waits for the server, subscribes to its events and opens the
// native session: the resumed one when it still exists, otherwise a new one.
func (s *session) handshake(ctx context.Context, resumeID string) error {
	version, err := s.srv.ready(ctx)
	if err != nil {
		return err
	}
	s.version = version
	streamCtx, cancel := context.WithCancel(context.Background())
	s.streamCancel = cancel
	connected := make(chan struct{})
	streamErr := make(chan error, 1)
	go func() {
		err := s.srv.events(streamCtx, connected, s.onEvent)
		streamErr <- err
		select {
		case <-streamCtx.Done():
		case <-s.srv.done:
		default:
			log.Printf("opencode: 事件流中断，结束 serve 进程: %v", err)
			s.srv.stop()
		}
	}()
	go func() {
		<-s.srv.done
		s.markDone()
	}()
	select {
	case <-connected:
	case err := <-streamErr:
		return fmt.Errorf("opencode event stream: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
	if resumeID != "" {
		err := s.srv.call(ctx, http.MethodGet, sprintfPath("/session/%s", resumeID), nil, nil)
		if err == nil {
			s.id = resumeID
			return nil
		}
		if !isNotFound(err) {
			return fmt.Errorf("opencode resume session: %w", err)
		}
		log.Printf("opencode: 会话 %s 已不存在，改开新会话", resumeID)
	}
	var created struct {
		ID string `json:"id"`
	}
	// Grasp owns the title, which also spares OpenCode's title-generation
	// request. Every permission is granted, as with --dangerously-skip-
	// permissions; the question tool is denied because no one can answer it.
	err = s.srv.call(ctx, http.MethodPost, "/session", map[string]any{
		"title": "Grasp",
		"permission": []map[string]string{
			{"permission": "*", "pattern": "*", "action": "allow"},
			{"permission": "question", "pattern": "*", "action": "deny"},
		},
	}, &created)
	if err != nil {
		return fmt.Errorf("opencode create session: %w", err)
	}
	if created.ID == "" {
		return errors.New("opencode create session: no session id")
	}
	s.id = created.ID
	return nil
}

func (s *session) SessionID() string { return s.id }
func (s *session) CWD() string       { return s.cwd }
func (s *session) FSRoot() string    { return s.fsRoot }

func (s *session) Info() provider.AgentInfo {
	model := s.displayModel
	if model == "" && s.model.ModelID != "" {
		model = s.model.ProviderID + "/" + s.model.ModelID
	}
	return provider.AgentInfo{Name: string(provider.OpenCode), Title: "OpenCode", Version: s.version, ModelID: model, ModelName: model}
}

// Capabilities: input posted while a turn runs joins it at the next step,
// and a session id continues the same conversation in a new server.
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

// AgentPIDs is the server; the commands it runs are its descendants.
func (s *session) AgentPIDs() []int { return []int{s.srv.pid()} }

// ConfigFiles: the server reads its configuration and logins at start.
func (s *session) ConfigFiles() []string {
	configHome := envutil.Get(s.env, "XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Dir(configRoot)
	}
	dataHome := envutil.Get(s.env, "XDG_DATA_HOME")
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	dir := filepath.Join(configHome, "opencode")
	return []string{
		filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc"), filepath.Join(dir, "config.json"),
		filepath.Join(dataHome, "opencode", "auth.json"),
	}
}

// Prompt runs one turn. A cancelled ctx aborts the turn and waits for the
// server to confirm it; a server that does not is stopped.
func (s *session) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	text, err := provider.WithAttachments(text, images)
	if err != nil {
		s.emitError(err.Error())
		s.emitPromptDone("failed", nil, false)
		return provider.TurnResult{StopReason: "failed"}, err
	}
	t := &activeTurn{done: make(chan struct{}), since: time.Now().UnixMilli(), posted: 1,
		users: map[string]bool{}, answered: map[string]bool{}}
	s.mu.Lock()
	s.turn = t
	s.mu.Unlock()
	if err := s.post(ctx, text); err != nil {
		if rejected := (*httpError)(nil); !errors.As(err, &rejected) {
			// No answer: the server may have taken the prompt. Whatever it
			// started must stop before the next turn may run.
			s.halt()
		}
		s.endTurn()
		return s.finishFailed(ctx, err)
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		s.abort(s.id)
		select {
		case <-t.done:
		case <-s.done:
		case <-time.After(provider.InterruptGrace):
			log.Printf("opencode: 会话 %s 未在 %s 内响应中断，结束 serve 进程", s.id, provider.InterruptGrace)
			s.srv.stop()
		}
		s.halt()
		return s.finishTurn(ctx, t, true)
	case <-s.done:
		s.endTurn()
		return s.finishFailed(ctx, fmt.Errorf("opencode serve exited during the turn: %s", s.srv.exitText()))
	}
	s.mu.Lock()
	aborted, failed := t.aborted, t.errText != ""
	s.mu.Unlock()
	if !aborted && !failed {
		if err := s.awaitSubagents(ctx); errors.Is(err, errSubagentHostExited) {
			s.endTurn()
			return s.finishFailed(ctx, fmt.Errorf("%w: %s", err, s.srv.exitText()))
		} else if err != nil {
			return s.finishTurn(ctx, t, true)
		}
	}
	return s.finishTurn(ctx, t, aborted)
}

// Steer posts text into the running turn; OpenCode picks it up at the next
// step and the turn ends only after it was answered.
func (s *session) Steer(text string) error {
	s.mu.Lock()
	t := s.turn
	if t == nil || t.settled {
		s.mu.Unlock()
		return provider.ErrNoActiveTurn
	}
	t.posted++
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), provider.SteerTimeout)
	defer cancel()
	if err := s.post(ctx, text); err != nil {
		s.mu.Lock()
		t.posted--
		s.settleLocked(t)
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *session) post(ctx context.Context, text string) error {
	body := map[string]any{"parts": []map[string]string{{"type": "text", "text": text}}}
	if s.model.ModelID != "" {
		body["model"] = s.model
	}
	if s.variant != "" {
		body["variant"] = s.variant
	}
	return s.srv.call(ctx, http.MethodPost, sprintfPath("/session/%s/prompt_async", s.id), body, nil)
}

// Cancel aborts the running turn. Prompt waits for the server to settle it.
func (s *session) Cancel() error {
	s.mu.Lock()
	running := s.turn != nil
	s.mu.Unlock()
	if running {
		go s.abort(s.id)
	}
	return nil
}

func (s *session) abort(id string) {
	if err := s.srv.call(context.Background(), http.MethodPost, sprintfPath("/session/%s/abort", id), nil, nil); err != nil {
		log.Printf("opencode: 中断会话 %s 失败: %v", id, err)
	}
}

// halt aborts this session's run and every subagent still running, and
// returns once the server reports all of them idle. Aborts repeat on each
// poll, so a lost request is retried; a server that does not confirm within
// InterruptGrace is stopped, and the bridge revives it on the native session
// like a crash.
func (s *session) halt() {
	deadline := time.After(provider.InterruptGrace)
	for {
		self, children, err := s.running(context.Background())
		if err == nil && !self && len(children) == 0 {
			return
		}
		if self || err != nil {
			s.abort(s.id)
		}
		for _, id := range children {
			s.abort(id)
		}
		select {
		case <-s.srv.done:
			return
		case <-deadline:
			log.Printf("opencode: 会话 %s 及其子 Agent 未在 %s 内停止，结束 serve 进程", s.id, provider.InterruptGrace)
			s.srv.stop()
			return
		case <-time.After(haltPoll):
		}
	}
}

// subagentPoll is how often a finished turn re-checks the subagents it waits
// for. A var so tests can shrink it.
var subagentPoll = time.Second

// haltPoll is how often a halt re-checks that what it interrupted stopped.
const haltPoll = 100 * time.Millisecond

// awaitSubagents holds a finished turn while a child session (subagent) of
// this session is still busy, so prompt_done never reports work as finished
// while background agents still change the workspace. It fails when ctx ends
// or the server exits first.
func (s *session) awaitSubagents(ctx context.Context) error {
	_, children, err := s.running(ctx)
	if err != nil {
		log.Printf("opencode: 读取子会话失败，不等待子 Agent: %v", err)
	}
	busy := len(children)
	if busy == 0 {
		return nil
	}
	callID := "subagents-" + s.id
	s.emitUpdate(map[string]any{"sessionUpdate": "tool_call", "toolCallId": callID, "status": "in_progress",
		"title": fmt.Sprintf("等待 %d 个子 Agent 完成", busy)})
	tick := time.NewTicker(subagentPoll)
	defer tick.Stop()
	for busy > 0 {
		select {
		case <-ctx.Done():
			s.halt()
			s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": callID, "status": "failed"})
			return ctx.Err()
		case <-s.done:
			s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": callID, "status": "failed"})
			return errSubagentHostExited
		case <-tick.C:
			// Unknown is not idle: keep waiting on the last known count.
			if _, children, err := s.running(ctx); err != nil {
				log.Printf("opencode: 子 Agent 状态未知，继续等待: %v", err)
			} else {
				busy = len(children)
			}
		}
	}
	s.emitUpdate(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": callID, "status": "completed"})
	return nil
}

// childStartGrace bounds how long a created child that has reported no
// status yet counts as running.
const childStartGrace = 30 * time.Second

var errSubagentHostExited = errors.New("opencode serve exited while subagents were running")

// running reports whether this session is still running and which of its
// child sessions are.
func (s *session) running(ctx context.Context) (self bool, children []string, err error) {
	var all []struct {
		ID string `json:"id"`
	}
	if err := s.srv.call(ctx, http.MethodGet, sprintfPath("/session/%s/children", s.id), nil, &all); err != nil {
		return false, nil, err
	}
	var status map[string]struct {
		Type string `json:"type"`
	}
	if err := s.srv.call(ctx, http.MethodGet, "/session/status", nil, &status); err != nil {
		return false, nil, err
	}
	st, ok := status[s.id]
	self = ok && st.Type != "idle"
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, child := range all {
		created, unstarted := s.unstarted[child.ID]
		if st, ok := status[child.ID]; (ok && st.Type != "idle") || (unstarted && time.Since(created) < childStartGrace) {
			children = append(children, child.ID)
		}
	}
	return self, children, nil
}

// finishTurn settles usage and stop reason, emits prompt_done and returns the
// error contract the bridge expects for each cancel cause.
func (s *session) finishTurn(ctx context.Context, t *activeTurn, interrupted bool) (provider.TurnResult, error) {
	s.endTurn()
	s.mu.Lock()
	var usage map[string]provider.TokenUsage
	if t.reported {
		usage = map[string]provider.TokenUsage{"default": t.usage}
		c := s.cumUsage["default"]
		c.InputTokens += t.usage.InputTokens
		c.OutputTokens += t.usage.OutputTokens
		c.CacheReadTokens += t.usage.CacheReadTokens
		c.CacheWriteTokens += t.usage.CacheWriteTokens
		s.cumUsage["default"] = c
	}
	errText := t.errText
	s.mu.Unlock()
	if interrupted {
		return s.finishInterrupted(ctx, usage)
	}
	res := provider.TurnResult{Usage: usage}
	switch {
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

// endTurn detaches the turn, so late events of an aborted run stay out of
// the transcript, settles its parts and closes the tool calls it left running.
func (s *session) endTurn() {
	s.mu.Lock()
	s.turn = nil
	for id := range s.parts {
		s.parts[id] = settledPart
	}
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
	msg := provider.RedactDiagnostic(err.Error(), s.env)
	s.emitError(msg)
	s.emitPromptDone("failed", nil, false)
	return provider.TurnResult{StopReason: "failed"}, errors.New(msg)
}

func (s *session) Done() <-chan struct{} { return s.done }

func (s *session) markDone() { s.doneOnce.Do(func() { close(s.done) }) }

func (s *session) Close() error {
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.srv.stop()
	s.release()
	return nil
}

func (s *session) release() {
	s.closeOnce.Do(func() {
		if s.streamCancel != nil {
			s.streamCancel()
		}
		s.markDone()
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}

// ExitInfo reaps the server and composes the operator-facing banner.
func (s *session) ExitInfo() (string, error) {
	<-s.srv.done
	s.release()
	msg := "OpenCode serve 已退出。"
	if text := s.srv.exitText(); text != "" {
		msg += " " + provider.RedactDiagnostic(text, s.env) + "。"
	}
	if s.srv.waitErr != nil && !errors.Is(s.srv.waitErr, io.EOF) {
		return msg, s.srv.waitErr
	}
	return msg, nil
}
func parseModel(model string) modelRef {
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		return modelRef{}
	}
	providerID, modelID, ok := strings.Cut(model, "/")
	if !ok {
		return modelRef{}
	}
	return modelRef{ProviderID: providerID, ModelID: modelID}
}

func sprintfPath(format, id string) string { return fmt.Sprintf(format, url.PathEscape(id)) }
