package opencode

import (
	"context"
	"crypto/rand"
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
	return provider.CopyUsage(s.cumUsage)
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
		return s.finishFailed(ctx, err)
	}
	t := &activeTurn{done: make(chan struct{}), since: time.Now().UnixMilli(), posted: 1,
		users: map[string]bool{}, answered: map[string]bool{}}
	s.mu.Lock()
	s.turn = t
	s.mu.Unlock()
	if err := s.post(ctx, text, ""); err != nil {
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
		grace := time.After(provider.InterruptGrace)
		go s.abort(context.Background(), s.id)
		select {
		case <-t.done:
		case <-s.done:
		case <-grace:
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
		if err := s.awaitSubagents(ctx); errors.Is(err, provider.ErrSubagentHostExited) {
			s.endTurn()
			return s.finishFailed(ctx, fmt.Errorf("opencode serve exited while subagents were running: %s", s.srv.exitText()))
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
	id := newMessageID()
	ctx, cancel := context.WithTimeout(context.Background(), steerTimeout)
	defer cancel()
	err := s.post(ctx, text, id)
	if rejected := (*httpError)(nil); err != nil && !errors.As(err, &rejected) {
		// No answer: the server may have taken it.
		err = s.steerLanded(id)
	}
	if err != nil {
		s.mu.Lock()
		t.posted--
		s.settleLocked(t)
		s.mu.Unlock()
		return err
	}
	return nil
}

// steerTimeout bounds the steer post; steerCheckTimeout the read that
// settles an unanswered one. Together they stay within provider.SteerTimeout
// plus the margin the platform allows. Vars so tests can shrink them.
var (
	steerTimeout      = provider.SteerTimeout
	steerCheckTimeout = 5 * time.Second
)

// steerLanded settles a steer post that got no answer by whether the
// session holds its message, so input the server took is never run again
// as the next turn.
func (s *session) steerLanded(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), steerCheckTimeout)
	defer cancel()
	err := s.srv.call(ctx, http.MethodGet, fmt.Sprintf("/session/%s/message/%s", url.PathEscape(s.id), url.PathEscape(id)), nil, nil)
	if isNotFound(err) {
		return errors.New("opencode: 引导未送达")
	}
	if err != nil {
		return fmt.Errorf("无法确认引导是否送达: %w", err)
	}
	return nil
}

// newMessageID is a message id in OpenCode's own format, which orders a
// session's messages by creation: "msg_", 12 hex digits of the millisecond
// clock times 4096 plus a counter, then 14 random base62 characters. The
// server runs on this clock, so the id sorts after the messages before it.
func newMessageID() string {
	msgClock.Lock()
	ms := time.Now().UnixMilli()
	if ms != msgClock.ms {
		msgClock.ms, msgClock.n = ms, 0
	}
	msgClock.n++
	stamp := ms*0x1000 + msgClock.n
	msgClock.Unlock()
	const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	tail := make([]byte, 14)
	_, _ = rand.Read(tail)
	for i, b := range tail {
		tail[i] = base62[int(b)%len(base62)]
	}
	return fmt.Sprintf("msg_%012x%s", stamp&0xffffffffffff, tail)
}

var msgClock struct {
	sync.Mutex
	ms, n int64
}

// post sends text into the session. messageID, when set, names the user
// message so its arrival can be checked.
func (s *session) post(ctx context.Context, text, messageID string) error {
	body := map[string]any{"parts": []map[string]string{{"type": "text", "text": text}}}
	if messageID != "" {
		body["messageID"] = messageID
	}
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
		go s.abort(context.Background(), s.id)
	}
	return nil
}

func (s *session) abort(ctx context.Context, id string) {
	if err := s.srv.call(ctx, http.MethodPost, sprintfPath("/session/%s/abort", id), nil, nil); err != nil {
		log.Printf("opencode: 中断会话 %s 失败: %v", id, err)
	}
}

// halt aborts this session's run and every subagent still running, and
// returns once the server reports all of them idle. Aborts repeat on each
// poll, so a lost request is retried; a server that does not confirm within
// InterruptGrace is stopped, and the bridge revives it on the native session
// like a crash.
func (s *session) halt() {
	ctx, cancel := context.WithTimeout(context.Background(), provider.InterruptGrace)
	defer cancel()
	for {
		self, children, err := s.running(ctx)
		if err == nil && !self && len(children) == 0 {
			return
		}
		if self || err != nil {
			s.abort(ctx, s.id)
		}
		for _, id := range children {
			s.abort(ctx, id)
		}
		select {
		case <-s.srv.done:
			return
		case <-ctx.Done():
			log.Printf("opencode: 会话 %s 及其子 Agent 未在 %s 内停止，结束 serve 进程", s.id, provider.InterruptGrace)
			s.srv.stop()
			return
		case <-time.After(provider.HaltPoll):
		}
	}
}

// awaitSubagents holds a finished turn while a child session (subagent) of
// this session is still busy.
func (s *session) awaitSubagents(ctx context.Context) error {
	return provider.AwaitSubagents(ctx, provider.SubagentWait{
		CallID: "subagents-" + s.id,
		Running: func(ctx context.Context) (int, error) {
			_, children, err := s.running(ctx)
			return len(children), err
		},
		Halt:     s.halt,
		HostDone: s.done,
		Emit:     s.emitUpdate,
	})
}

// childStartGrace bounds how long a created child that has reported no
// status yet counts as running.
const childStartGrace = 30 * time.Second

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
		provider.AddUsage(s.cumUsage, usage)
	}
	errText := t.errText
	s.mu.Unlock()
	if interrupted {
		return provider.EndInterrupted(ctx, s.emit, s.id, usage)
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

func (s *session) finishFailed(ctx context.Context, err error) (provider.TurnResult, error) {
	return provider.EndFailed(ctx, s.emit, s.id, provider.RedactDiagnostic(err.Error(), s.env))
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
