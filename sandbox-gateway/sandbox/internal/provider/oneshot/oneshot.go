// Package oneshot drives one-shot agent CLIs: a fresh subprocess is spawned per
// turn and multi-turn continuity is achieved by resuming a session id. It maps
// each CLI's streaming stdout into WSP session/update frames and presents a
// long-lived-looking provider.Session to the bridge (Done closes only on Close).
package oneshot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"backend/internal/provider"
)

// MsgKind is the unified message taxonomy every codec maps its output into.
type MsgKind int

const (
	KindText MsgKind = iota
	KindThinking
	KindToolUse
	KindToolResult
	KindStatus
	KindError
	KindLog
)

// Msg is one normalized event parsed from a CLI's stdout.
type Msg struct {
	Kind       MsgKind
	Text       string
	ToolCallID string
	ToolTitle  string
	ToolStatus string
	RawInput   json.RawMessage
}

// ParseResult is what a codec extracts from a single stdout line.
type ParseResult struct {
	Msgs       []Msg
	SessionID  string
	Usage      map[string]provider.TokenUsage
	StopReason string
}

// Codec captures the per-CLI specifics: how to launch it and how to parse its
// streaming output. Everything else (process mgmt, mapping, resume, lifecycle)
// is handled by the engine.
type Codec interface {
	AgentName() provider.Name
	Bin() string
	Runtime() string
	ConfigRoot() string
	ReportsUsage() bool
	// PromptViaStdin reports whether the prompt is fed on stdin (vs. argv).
	PromptViaStdin() bool
	// Args builds the full argv (including bin) for one turn.
	Args(opts provider.OpenOptions, prompt, resumeID string) []string
	// AuthEnv normalizes credentials into the CLI's native env vars.
	AuthEnv(env []string) []string
	// Models returns an optional model catalog (nil => auto).
	Models(ctx context.Context) ([]provider.Model, error)
	// ParseLine parses one stdout line into normalized events.
	ParseLine(line []byte) ParseResult
}

// StdinEncoder is an optional Codec extension: when the prompt is delivered on
// stdin (PromptViaStdin), a codec may implement this to control the exact bytes
// written (e.g. a stream-json user envelope). Codecs that do not implement it
// get the raw prompt text. Images are passed through when the codec supports
// them (see ImageCapable); otherwise the engine rejects the turn earlier.
type StdinEncoder interface {
	StdinBytes(prompt string, images []provider.PromptImage) []byte
}

// ImageCapable is an optional Codec extension for CLIs that can accept image
// attachments on a turn (today: stream-json PromptStdinJSON / Claude family).
type ImageCapable interface {
	SupportsImages() bool
}

// LineParser parses a CLI's stdout stream for ONE turn. A plain Codec satisfies
// it directly (ParseLine); stateful CLIs return a fresh instance per turn.
type LineParser interface {
	ParseLine(line []byte) ParseResult
}

// StatefulCodec is an optional Codec extension for CLIs whose stream needs
// cross-line state within a single turn (active model, delta de-duplication,
// text-sanitizer buffers). The engine calls NewTurnParser once per turn so the
// singleton codec never leaks state between concurrent sessions or turns.
type StatefulCodec interface {
	NewTurnParser(opts provider.OpenOptions) LineParser
}

// WholeOutputCodec is an optional Codec extension for CLIs that emit a single
// (usually pretty-printed) JSON document rather than line-delimited JSON. The
// engine buffers all of stdout and hands it to ParseAll once the process closes
// stdout, instead of scanning line by line.
type WholeOutputCodec interface {
	ParseAll(stdout []byte) ParseResult
}

// SessionInitializer is an optional Codec extension for CLIs whose session is
// an externally-managed handle (e.g. a session file path) that must be
// allocated before the first turn rather than discovered from output. When no
// resume pointer was supplied the engine calls InitSession once at Open and
// uses the returned id as the resume pointer for every turn.
type SessionInitializer interface {
	InitSession(opts provider.OpenOptions) (string, error)
}

// LogFileCodec is an optional Codec extension for CLIs that write their
// machine-readable side channel (session id, structured errors) to a
// --log-file instead of stdout. The engine allocates a temp file, passes its
// path to ArgsWithLog, line-scans stdout as usual, and after the process exits
// hands the log bytes to ParseLogFile for post-run extraction.
type LogFileCodec interface {
	ArgsWithLog(opts provider.OpenOptions, prompt, resumeID, logPath string) []string
	ParseLogFile(data []byte) ParseResult
}

// Provider is a one-shot provider backed by a Codec.
type Provider struct{ c Codec }

// NewProvider wraps a Codec as a provider.Provider.
func NewProvider(c Codec) provider.Provider { return &Provider{c: c} }

func (p *Provider) Name() provider.Name               { return p.c.AgentName() }
func (p *Provider) Runtime() string                   { return p.c.Runtime() }
func (p *Provider) DefaultConfigRoot() string         { return p.c.ConfigRoot() }
func (p *Provider) Transport() provider.TransportKind { return provider.OneShot }
func (p *Provider) AuthEnv(env []string) []string     { return p.c.AuthEnv(env) }

// ArgsForTest exposes codec.Args for registry golden locks (CAPA A2/A3).
// Not part of provider.Provider; callers type-assert the concrete oneshot.Provider.
func (p *Provider) ArgsForTest(opts provider.OpenOptions, prompt, resumeID string) []string {
	return p.c.Args(opts, prompt, resumeID)
}

func (p *Provider) ListModels(ctx context.Context) ([]provider.Model, error) {
	return p.c.Models(ctx)
}

// Open probes the CLI (fail-fast if missing) and returns a virtual session.
func (p *Provider) Open(procCtx, _ context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), _ provider.PermissionChooser) (provider.Session, error) {
	bin := p.c.Bin()
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("one-shot agent %q not found on PATH: %w", bin, err)
	}
	fsRoot := opts.FSRoot
	if fsRoot == "" {
		fsRoot = opts.Cwd
	}
	s := &engine{
		c:       p.c,
		binPath: path,
		procCtx: procCtx,
		opts:    opts,
		cwd:     opts.Cwd,
		fsRoot:  fsRoot,
		// Seed from the parent environment so the child keeps HOME/PATH/etc.
		// (needed to locate the CLI's login/config), then normalize auth vars.
		env:      p.c.AuthEnv(os.Environ()),
		onEvent:  onEvent,
		done:     make(chan struct{}),
		cumUsage: map[string]provider.TokenUsage{},
	}
	s.sessionID = opts.ResumeSessionID
	if s.sessionID == "" {
		if init, ok := p.c.(SessionInitializer); ok {
			if sid, ierr := init.InitSession(opts); ierr != nil {
				return nil, fmt.Errorf("one-shot agent %q session init: %w", bin, ierr)
			} else if sid != "" {
				s.sessionID = sid
			}
		}
	}
	log.Printf("oneshot: 已就绪 agent=%s bin=%s transport=one-shot", p.c.AgentName(), path)
	return s, nil
}

// engine is a one-shot provider.Session: it keeps session state across turns
// and spawns the CLI anew for each Prompt.
type engine struct {
	c       Codec
	binPath string
	procCtx context.Context
	opts    provider.OpenOptions
	cwd     string
	fsRoot  string
	env     []string
	onEvent func(json.RawMessage)

	mu        sync.Mutex
	sessionID string
	cumUsage  map[string]provider.TokenUsage
	curCancel context.CancelFunc

	done      chan struct{}
	closeOnce sync.Once
}

func (e *engine) SessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sessionID == "" {
		return "oneshot-pending"
	}
	return e.sessionID
}
func (e *engine) CWD() string    { return e.cwd }
func (e *engine) FSRoot() string { return e.fsRoot }

func (e *engine) Info() provider.AgentInfo {
	return provider.AgentInfo{
		Name:      string(e.c.AgentName()),
		Title:     string(e.c.AgentName()),
		ModelID:   e.opts.Model,
		ModelName: e.opts.Model,
	}
}

func (e *engine) ReportsUsage() bool { return e.c.ReportsUsage() }

func (e *engine) CumulativeUsage() map[string]provider.TokenUsage {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.cumUsage) == 0 {
		return nil
	}
	out := make(map[string]provider.TokenUsage, len(e.cumUsage))
	for k, v := range e.cumUsage {
		out[k] = v
	}
	return out
}

// Prompt runs one turn, spawning the CLI with a --resume pointer when available.
func (e *engine) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	// Unified attachment path: always land image/file bytes under /tmp and pass
	// absolute paths in the prompt. Agents then Read files like any other path
	// (works for cursor/gemini/codex and also for codecs that could embed images).
	if len(images) > 0 {
		dir, paths, merr := provider.MaterializeAttachments(images)
		if merr != nil {
			log.Printf("oneshot: 附件落盘失败: %v", merr)
			e.emitPromptDone("failed", nil)
			return provider.TurnResult{StopReason: "failed"}, merr
		}
		defer os.RemoveAll(dir)
		log.Printf("oneshot: agent %q 已将 %d 个附件落到 %s", e.c.AgentName(), len(paths), dir)
		text = provider.AppendAttachmentRefs(text, paths)
		images = nil
	}

	e.mu.Lock()
	resumeID := e.sessionID
	e.mu.Unlock()

	res, err := e.runOnce(ctx, text, images, resumeID)
	// Resume fallback: if a resume attempt failed before establishing a
	// session, retry once from scratch (rescues a stale resume pointer).
	if err != nil && resumeID != "" && res.newSessionID == "" && !errors.Is(err, context.Canceled) {
		log.Printf("oneshot: resume 失败，回退为全新会话重试一次: %v", err)
		e.mu.Lock()
		e.sessionID = ""
		e.mu.Unlock()
		res, err = e.runOnce(ctx, text, images, "")
	}
	if errors.Is(err, context.Canceled) && (res.stopReason == "" || res.stopReason == "failed") {
		res.stopReason = "cancelled"
	}
	e.emitPromptDone(res.stopReason, res.usage)
	return provider.TurnResult{StopReason: res.stopReason, Usage: res.usage}, err
}

type turnOutcome struct {
	stopReason   string
	usage        map[string]provider.TokenUsage
	newSessionID string
}

func (e *engine) runOnce(ctx context.Context, text string, images []provider.PromptImage, resumeID string) (turnOutcome, error) {
	turnCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.curCancel = cancel
	e.mu.Unlock()
	defer cancel()

	// LogFile mode: allocate a temp log the CLI writes its side channel to.
	var logPath string
	logCodec, logMode := e.c.(LogFileCodec)
	if logMode {
		f, ferr := os.CreateTemp("", "agent-log-*.log")
		if ferr != nil {
			return turnOutcome{stopReason: "failed"}, fmt.Errorf("oneshot: allocate log file: %w", ferr)
		}
		logPath = f.Name()
		if cerr := f.Close(); cerr != nil {
			_ = os.Remove(logPath)
			return turnOutcome{stopReason: "failed"}, fmt.Errorf("oneshot: close log file: %w", cerr)
		}
		defer os.Remove(logPath)
	}

	var args []string
	if logMode {
		args = logCodec.ArgsWithLog(e.opts, text, resumeID, logPath)
	} else {
		args = e.c.Args(e.opts, text, resumeID)
	}
	if len(args) == 0 {
		return turnOutcome{stopReason: "failed"}, errors.New("oneshot: codec produced empty argv")
	}
	// CAPA A6: greppable redacted argv (MCP flags+paths retained; no prompt/Authorization).
	log.Printf("oneshot: spawn agent=%s argv_redacted=%v", e.c.AgentName(), redactArgv(args))
	// Use plain Command (not CommandContext) so we can kill the whole process
	// group on cancel — many CLIs spawn children that would otherwise orphan.
	cmd := exec.Command(e.binPath, args[1:]...)
	cmd.Dir = e.cwd
	cmd.Env = e.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Own both output pipes instead of using StdoutPipe / an io.Writer Stderr:
	// handing exec *os.File ends keeps it from copying the streams in internal
	// goroutines that cmd.Wait blocks on, and keeps the read ends pollable so a
	// pipe a leaked descendant still holds can be abandoned (see joinOutputs).
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return turnOutcome{stopReason: "failed"}, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		closeFiles(stdoutR, stdoutW)
		return turnOutcome{stopReason: "failed"}, err
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	tail := newStderrTail(64 * 1024)

	var stdin io.WriteCloser
	if e.c.PromptViaStdin() {
		if stdin, err = cmd.StdinPipe(); err != nil {
			closeFiles(stdoutR, stdoutW, stderrR, stderrW)
			return turnOutcome{stopReason: "failed"}, err
		}
	}
	if err := cmd.Start(); err != nil {
		closeFiles(stdoutR, stdoutW, stderrR, stderrW)
		return turnOutcome{stopReason: "failed"}, err
	}
	// The child holds its own copies now; drop the parent's write ends or even a
	// well-behaved CLI's pipes would never reach EOF.
	closeFiles(stdoutW, stderrW)
	// Cancel → SIGKILL the process group (negative pgid). Disarmed as soon as the
	// CLI is known to be gone: signalling the group after that only reaches the
	// children it left behind, e.g. a service the agent started for a preview.
	killCh := make(chan struct{})
	var killerOff sync.Once
	disarmKiller := func() { killerOff.Do(func() { close(killCh) }) }
	defer disarmKiller()
	go func() {
		select {
		case <-turnCtx.Done():
			// Ties go to the disarm side.
			select {
			case <-killCh:
				return
			default:
			}
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-killCh:
		}
	}()

	// Feed stdin in a goroutine so large prompts cannot deadlock against a
	// child that is simultaneously filling the stdout pipe buffer.
	var stdinErr error
	var stdinDone chan struct{}
	if stdin != nil {
		if _, ok := e.c.(StdinEncoder); !ok && len(images) > 0 {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			closeFiles(stdoutR, stderrR)
			return turnOutcome{stopReason: "failed"}, fmt.Errorf("oneshot: agent %q 图片附件需要 StdinEncoder", e.c.AgentName())
		}
		stdinDone = make(chan struct{})
		go func() {
			defer close(stdinDone)
			var werr error
			if enc, ok := e.c.(StdinEncoder); ok {
				_, werr = stdin.Write(enc.StdinBytes(text, images))
			} else {
				_, werr = io.WriteString(stdin, text)
			}
			cerr := stdin.Close()
			if werr != nil {
				stdinErr = werr
				return
			}
			if cerr != nil {
				log.Printf("oneshot: close stdin: %v", cerr)
			}
		}()
	}

	turnUsage := map[string]provider.TokenUsage{}
	var newSID, stop string
	apply := func(pr ParseResult) {
		if pr.SessionID != "" && newSID == "" {
			newSID = pr.SessionID
			e.mu.Lock()
			e.sessionID = pr.SessionID
			e.mu.Unlock()
		}
		for model, u := range pr.Usage {
			turnUsage[model] = addUsage(turnUsage[model], u)
		}
		for _, m := range pr.Msgs {
			e.emitUpdate(m)
		}
		if pr.StopReason != "" {
			stop = pr.StopReason
		}
	}

	var streamErr error
	stdoutPipe := newOutPipe(stdoutR, outputDrainGrace)
	stderrPipe := newOutPipe(stderrR, outputDrainGrace)
	stderrPipe.start(func(r io.Reader) { _, _ = io.Copy(tail, r) })
	stdoutPipe.start(func(r io.Reader) {
		if whole, ok := e.c.(WholeOutputCodec); ok {
			// Whole-document mode: drain stdout, parse once (pretty-printed JSON).
			buf, rerr := io.ReadAll(r)
			if len(buf) > 0 {
				apply(whole.ParseAll(buf))
			}
			if rerr != nil {
				streamErr = fmt.Errorf("oneshot: read stdout: %w", rerr)
			}
			return
		}
		// Per-turn parser: stateful codecs get a fresh instance each turn.
		var parser LineParser = e.c
		if sf, ok := e.c.(StatefulCodec); ok {
			parser = sf.NewTurnParser(e.opts)
		}
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(strings.TrimSpace(string(line))) == 0 {
				continue
			}
			apply(parser.ParseLine(line))
		}
		if serr := sc.Err(); serr != nil {
			streamErr = fmt.Errorf("oneshot: scan stdout: %w", serr)
		}
	})

	// The turn ends when the CLI process exits, never when its pipes reach EOF:
	// a service the agent started during the turn inherits the write ends, so
	// EOF can be arbitrarily far away (or never come at all).
	waitErr := cmd.Wait()
	disarmKiller()
	stdoutPipe.startDraining()
	stderrPipe.startDraining()
	leakedOut, leakedErr := stdoutPipe.join(), stderrPipe.join()
	if leakedOut || leakedErr {
		log.Printf("oneshot: agent=%s 已退出但输出管道仍被其后代进程持有，单次读取超过 %s 后放弃（本回合可能在前台或未重定向地起了常驻服务，正确写法：setsid nohup cmd </dev/null >log 2>&1 &）",
			e.c.AgentName(), outputDrainGrace)
		if errors.Is(streamErr, os.ErrDeadlineExceeded) {
			streamErr = nil
		}
	}
	if stdinDone != nil {
		<-stdinDone
	}
	if stdinErr != nil && waitErr == nil {
		waitErr = fmt.Errorf("oneshot: write stdin: %w; stderr: %s", stdinErr, tail.String())
	} else if stdinErr != nil {
		waitErr = fmt.Errorf("oneshot: write stdin: %v; wait: %w; stderr: %s", stdinErr, waitErr, tail.String())
	}
	if turnCtx.Err() != nil {
		// Prefer the caller's cancel over a SIGKILL exit status.
		waitErr = turnCtx.Err()
	}

	// LogFile post-run: recover session id / structured errors from the log.
	if logMode {
		data, rerr := os.ReadFile(logPath)
		if rerr != nil {
			log.Printf("oneshot: read log file %s: %v", logPath, rerr)
		} else if len(data) > 0 {
			apply(logCodec.ParseLogFile(data))
		}
	}

	e.mergeCumulative(turnUsage)
	if stop == "" {
		if waitErr != nil || streamErr != nil {
			stop = "failed"
		} else {
			stop = "end_turn"
		}
	}
	out := turnOutcome{stopReason: stop, usage: nonEmptyUsage(turnUsage), newSessionID: newSID}
	if waitErr != nil {
		if streamErr != nil && !errors.Is(waitErr, context.Canceled) {
			return out, fmt.Errorf("%v; %w; stderr: %s", streamErr, waitErr, tail.String())
		}
		return out, fmt.Errorf("%w; stderr: %s", waitErr, tail.String())
	}
	if streamErr != nil {
		return out, fmt.Errorf("%w; stderr: %s", streamErr, tail.String())
	}
	return out, nil
}

func (e *engine) mergeCumulative(turn map[string]provider.TokenUsage) {
	if len(turn) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for model, u := range turn {
		e.cumUsage[model] = addUsage(e.cumUsage[model], u)
	}
}

func (e *engine) Cancel() error {
	e.mu.Lock()
	cancel := e.curCancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (e *engine) Close() error {
	e.closeOnce.Do(func() {
		e.Cancel()
		close(e.done)
	})
	return nil
}

func (e *engine) Done() <-chan struct{} { return e.done }

func (e *engine) ExitInfo() (string, error) {
	return "Agent 会话已结束（one-shot：每轮独立进程）。", nil
}

// --- event mapping: unified taxonomy -> ACP session/update shapes ----------

func (e *engine) emitUpdate(m Msg) {
	var update map[string]any
	switch m.Kind {
	case KindText:
		update = map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": m.Text}}
	case KindThinking:
		update = map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": m.Text}}
	case KindToolUse:
		update = map[string]any{"sessionUpdate": "tool_call", "toolCallId": m.ToolCallID, "title": m.ToolTitle, "status": "in_progress"}
		if len(m.RawInput) > 0 {
			update["rawInput"] = m.RawInput
		}
	case KindToolResult:
		status := m.ToolStatus
		if status == "" {
			status = "completed"
		}
		update = map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": m.ToolCallID, "status": status}
		if m.Text != "" {
			update["content"] = []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": m.Text}}}
		}
	case KindStatus, KindLog:
		// non-visual; surface as an assistant message chunk only if it carries text.
		if m.Text == "" {
			return
		}
		update = map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": m.Text}}
	case KindError:
		e.emit(map[string]any{"op": "raw", "type": "error_text", "text": m.Text})
		return
	default:
		return
	}
	e.emit(map[string]any{"type": "session_update", "sessionId": e.SessionID(), "update": update})
}

func (e *engine) emitPromptDone(stopReason string, usage map[string]provider.TokenUsage) {
	frame := map[string]any{"type": "prompt_done", "sessionId": e.SessionID(), "stopReason": stopReason}
	if e.c.ReportsUsage() && len(usage) > 0 {
		frame["usage"] = usage
	}
	e.emit(frame)
}

func (e *engine) emit(ev map[string]any) {
	if e.onEvent == nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		log.Printf("oneshot: 事件序列化失败: %v", err)
		return
	}
	e.onEvent(b)
}

// --- child output pipes ----------------------------------------------------

// outputDrainGrace bounds a single read on a CLI's stdout / stderr once the
// process itself exited. A var so tests can shrink it.
var outputDrainGrace = 2 * time.Second

// outPipe is the parent's read end of one child output stream, plus the
// goroutine consuming it.
type outPipe struct {
	f     *os.File
	grace time.Duration
	done  chan struct{}

	draining atomic.Bool
	timedOut atomic.Bool
}

func newOutPipe(f *os.File, grace time.Duration) *outPipe {
	return &outPipe{f: f, grace: grace, done: make(chan struct{})}
}

// start consumes the pipe in its own goroutine so the turn is bounded by the CLI
// process exiting rather than by the pipe reaching EOF.
func (p *outPipe) start(consume func(io.Reader)) {
	go func() {
		defer close(p.done)
		consume(&graceReader{p: p})
	}()
}

// startDraining arms a deadline per read now that the CLI process is gone. Only
// time spent blocked *in a read* counts against the grace, so a consumer that is
// merely slow (parsing, or emitting to a stalled websocket) is never mistaken
// for one waiting on a write end some leaked descendant kept open.
func (p *outPipe) startDraining() {
	p.draining.Store(true)
	if err := p.f.SetReadDeadline(time.Now().Add(p.grace)); err != nil {
		// No deadline support: closing is the only way to unblock the consumer.
		log.Printf("oneshot: 管道 %s 不支持读超时，直接关闭: %v", p.f.Name(), err)
		_ = p.f.Close()
	}
}

// join waits for the consumer and reports whether the pipe had to be abandoned,
// i.e. a descendant of the CLI leaked the write end and the trailing output is
// lost. An abandoned pipe keeps being drained to io.Discard in the background:
// the process still holding the write end is usually a service meant to outlive
// the turn, and closing the read end under it would kill it with SIGPIPE on its
// next line of output.
func (p *outPipe) join() bool {
	<-p.done
	if !p.timedOut.Load() {
		_ = p.f.Close()
		return false
	}
	if err := p.f.SetReadDeadline(time.Time{}); err != nil {
		_ = p.f.Close()
		return true
	}
	go func() {
		_, _ = io.Copy(io.Discard, p.f)
		_ = p.f.Close()
	}()
	return true
}

// graceReader re-arms the per-read deadline while draining and records whether a
// read ever hit it.
type graceReader struct{ p *outPipe }

func (g *graceReader) Read(b []byte) (int, error) {
	if g.p.draining.Load() {
		if err := g.p.f.SetReadDeadline(time.Now().Add(g.p.grace)); err != nil {
			return 0, err
		}
	}
	n, err := g.p.f.Read(b)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		g.p.timedOut.Store(true)
	}
	return n, err
}

func closeFiles(files ...*os.File) {
	for _, f := range files {
		if err := f.Close(); err != nil {
			log.Printf("oneshot: close %s: %v", f.Name(), err)
		}
	}
}

// --- helpers ---------------------------------------------------------------

func addUsage(a, b provider.TokenUsage) provider.TokenUsage {
	return provider.TokenUsage{
		InputTokens:      a.InputTokens + b.InputTokens,
		OutputTokens:     a.OutputTokens + b.OutputTokens,
		CacheReadTokens:  a.CacheReadTokens + b.CacheReadTokens,
		CacheWriteTokens: a.CacheWriteTokens + b.CacheWriteTokens,
	}
}

func nonEmptyUsage(u map[string]provider.TokenUsage) map[string]provider.TokenUsage {
	if len(u) == 0 {
		return nil
	}
	return u
}

// stderrTail is a bounded ring buffer capturing the last N bytes of stderr so
// native CLI crashes surface a diagnostic instead of a bare "exit status 3".
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newStderrTail(max int) *stderrTail { return &stderrTail{max: max} }

func (s *stderrTail) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
	return len(p), nil
}

func (s *stderrTail) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(string(s.buf))
}

// redactArgv returns a greppable argv snapshot for logs: keeps MCP-related
// flags and config paths, redacts prompt text and Authorization-like values.
func redactArgv(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		lower := strings.ToLower(a)
		if strings.Contains(lower, "authorization") || strings.HasPrefix(a, "Bearer ") || strings.HasPrefix(a, "bearer ") {
			out = append(out, "[redacted]")
			continue
		}
		// -p <prompt> (PromptArg CLIs): redact the following non-flag value.
		if (a == "-p" || a == "--message") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			out = append(out, a, "[prompt-redacted]")
			i++
			continue
		}
		out = append(out, a)
	}
	return out
}
