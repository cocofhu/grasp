// Package oneshot drives one-shot agent CLIs: a fresh subprocess is spawned per
// turn and multi-turn continuity is achieved by resuming a session id. It maps
// each CLI's streaming stdout into WSP session/update frames and presents a
// long-lived-looking provider.Session to the bridge (Done closes only on Close).
package oneshot

import (
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
	"backend/internal/provider/modelconnection"
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
	// DurationMs is how long a finished tool ran, when the CLI reports it.
	DurationMs int64
	// BackgroundPID is set on a KindToolResult when the call only launched a
	// background shell, which keeps running after the result: the pid of that
	// shell, its own process group leader.
	BackgroundPID int
}

// ParseResult is what a codec extracts from a single stdout line.
type ParseResult struct {
	Msgs      []Msg
	SessionID string
	Usage     map[string]provider.TokenUsage
	// UsageIsTotal marks an authoritative per-turn snapshot. A nonempty total
	// replaces prior streamed samples, which may use different model aliases.
	UsageIsTotal bool
	// UsageIsSessionTotal marks a total as the native session's running
	// per-model counters, which a resumed CLI continues from its previous
	// process. The engine reports the difference from the session's last total.
	UsageIsSessionTotal bool
	// SessionUsage is a separate canonical native-session total. Some CLIs
	// report per-process model buckets alongside cumulative flat counters.
	SessionUsage *provider.TokenUsage
	// TracksSessionUsage also advances the baseline on partial turns whose
	// native cumulative snapshot is unavailable.
	TracksSessionUsage bool
	UsagePartial       bool
	StopReason         string
	// UserMessagesConsumed counts stdin user messages the CLI acknowledged
	// (TurnInputCodec only).
	UserMessagesConsumed int
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
type Provider struct {
	c      Codec
	ledger sessionLedger
}

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
	if models, selected, err := modelconnection.Catalog(p.Name(), os.Environ()); selected {
		return models, err
	}
	return p.c.Models(ctx)
}

// Open probes the CLI (fail-fast if missing) and returns a virtual session.
func (p *Provider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), _ provider.PermissionChooser) (provider.Session, error) {
	prepared, err := modelconnection.Prepare(p.Name(), p.c.AuthEnv(os.Environ()), opts.Model, p.DefaultConfigRoot())
	if err != nil {
		return nil, err
	}
	if err := prepared.ValidateOptions(p.Name(), opts); err != nil {
		prepared.Cleanup()
		return nil, err
	}
	bin := p.c.Bin()
	path, err := exec.LookPath(bin)
	if err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("one-shot agent %q not found on PATH (sandbox image lacks this CLI; rebuild it with AGENT_PROVIDERS including %q): %w",
			bin, p.c.AgentName(), err)
	}
	if prepared.Platform || prepared.Connection != nil {
		if err := modelconnection.CheckVersion(procCtx, p.Name(), path, prepared.Env); err != nil {
			prepared.Cleanup()
			return nil, err
		}
	}
	opts.Model = prepared.Model
	opts.CustomArgs = append(append([]string(nil), opts.CustomArgs...), prepared.Args...)
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
		env:      prepared.Env,
		cleanup:  prepared.Cleanup,
		onEvent:  onEvent,
		ledger:   &p.ledger,
		done:     make(chan struct{}),
		cumUsage: map[string]provider.TokenUsage{},
		bgGrace:  BackgroundGrace(),
	}
	if prepared.Connection != nil {
		s.displayModel = prepared.ChatModelID()
	}
	s.sessionID = opts.ResumeSessionID
	if s.sessionID == "" {
		if init, ok := p.c.(SessionInitializer); ok {
			if sid, ierr := init.InitSession(opts); ierr != nil {
				prepared.Cleanup()
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
	c            Codec
	binPath      string
	procCtx      context.Context
	opts         provider.OpenOptions
	cwd          string
	fsRoot       string
	env          []string
	onEvent      func(json.RawMessage)
	cleanup      func()
	displayModel string

	ledger *sessionLedger

	mu        sync.Mutex
	sessionID string
	cumUsage  map[string]provider.TokenUsage
	curCancel context.CancelFunc
	// agentPID is the running CLI's pid (0 between turns), for the bridge's
	// CPU / IO liveness sampling.
	agentPID atomic.Int64
	// bgGrace is how long a quiet CLI may wait on its background shells before
	// they are ended (0 = never). bg is the running turn's tracker.
	bgGrace time.Duration
	bg      atomic.Pointer[backgroundShells]
	// input is the running turn's open stdin (TurnInputCodec only).
	input atomic.Pointer[turnInput]

	done      chan struct{}
	closeOnce sync.Once
}

func (e *engine) SessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sessionID == "" {
		return provider.SessionPending
	}
	return e.sessionID
}
func (e *engine) CWD() string    { return e.cwd }
func (e *engine) FSRoot() string { return e.fsRoot }

func (e *engine) Info() provider.AgentInfo {
	model := e.opts.Model
	if e.displayModel != "" {
		model = e.displayModel
	}
	return provider.AgentInfo{
		Name:      string(e.c.AgentName()),
		Title:     string(e.c.AgentName()),
		ModelID:   model,
		ModelName: model,
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
	// Files outlive the turn so follow-ups and a switched model can reopen them.
	if len(images) > 0 {
		files, merr := provider.MaterializeAttachments(images)
		if merr != nil {
			merr = e.safeError(merr)
			log.Printf("oneshot: 附件落盘失败: %v", merr)
			e.emitError(merr)
			e.emitPromptDone("failed", nil, false, nil)
			return provider.TurnResult{StopReason: "failed"}, merr
		}
		log.Printf("oneshot: agent %q 已将 %d 个附件落盘", e.c.AgentName(), len(files))
		text = provider.AppendAttachmentRefs(text, files)
		images = nil
	}

	e.mu.Lock()
	resumeID := e.sessionID
	e.mu.Unlock()

	res, err := e.runOnce(ctx, text, images, resumeID)
	resumeFailed := func() bool {
		return err != nil && resumeID != "" && res.newSessionID == "" && !errors.Is(err, context.Canceled) && context.Cause(ctx) == nil
	}
	// A CLI that cannot reach its API exits before reporting a session, which
	// looks like a stale pointer. Dropping the pointer then would silently start
	// an empty conversation, so connection failures retry the same session.
	for attempt := 0; resumeFailed() && transientFailure(err, res) && attempt < len(resumeRetryDelays); attempt++ {
		delay := resumeRetryDelays[attempt]
		log.Printf("oneshot: resume 遇到网络类错误，%s 后用同一会话重试（%d/%d）: %v", delay, attempt+1, len(resumeRetryDelays), e.safeError(err))
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		spent, spentPartial := res.usage, res.usagePartial
		res, err = e.runOnce(ctx, text, images, resumeID)
		res.usage = mergeUsage(spent, res.usage)
		res.usagePartial = res.usagePartial || spentPartial
	}
	// Resume fallback: a non-connection failure before a session was reported
	// means the CLI rejected the pointer. Retry once from scratch; the pointer
	// is replaced only when the fresh attempt reports its own session.
	sessionReset := false
	if resumeFailed() && !transientFailure(err, res) {
		spent := res.usage
		spentPartial := res.usagePartial
		log.Printf("oneshot: resume 失败，回退为全新会话重试一次: %v", e.safeError(err))
		res, err = e.runOnce(ctx, text, images, "")
		res.usage = mergeUsage(spent, res.usage)
		res.usagePartial = res.usagePartial || spentPartial
		sessionReset = res.newSessionID != "" && res.newSessionID != resumeID
		if sessionReset {
			log.Printf("oneshot: 已改用全新会话 %s（原会话 %s 的上下文未带入）", res.newSessionID, resumeID)
		}
	} else if resumeFailed() {
		log.Printf("oneshot: resume 重试后仍为网络类错误，保留会话 %s 供下次重试: %v", resumeID, e.safeError(err))
	}
	if cause := context.Cause(ctx); err != nil && errors.Is(cause, provider.ErrTurnRecover) {
		// First idle: the bridge will prompt again. Do not close the user turn.
		if res.stopReason == "end_turn" {
			err = nil
		} else {
			return provider.TurnResult{StopReason: "recover", Usage: res.usage, UsagePartial: res.usagePartial}, cause
		}
	}
	if cause := context.Cause(ctx); err != nil && errors.Is(cause, provider.ErrTurnTimeout) {
		if res.stopReason == "end_turn" {
			// The CLI already reported its final result and only failed to exit.
			err = nil
		} else {
			res.stopReason = provider.TimeoutStopReason(cause)
			err = cause
		}
	}
	if errors.Is(err, context.Canceled) && (res.stopReason == "" || res.stopReason == "failed") {
		res.stopReason = "cancelled"
	}
	// A failed resume attempt may have emitted structured errors before the
	// fresh-session fallback succeeded. Publish only the final attempt's
	// diagnostics, so an error from the abandoned attempt cannot fail that turn.
	errorReported := false
	for _, diagnostic := range res.diagnostics {
		if strings.TrimSpace(diagnostic.Text) != "" {
			e.emitUpdate(diagnostic)
			errorReported = true
		}
	}
	if err != nil {
		err = e.safeError(err)
		timedOut := res.stopReason == provider.StopReasonTimeout || res.stopReason == provider.StopReasonStuck
		if !errors.Is(err, context.Canceled) && (!errorReported || timedOut) {
			// Clients stop reading at prompt_done. Surface the final attempt's error
			// (including stderr) first, after resume fallback and watchdog handling.
			e.emitError(err)
		}
	}
	var reset map[string]any
	if sessionReset {
		reset = map[string]any{"sessionReset": true, "previousSessionId": resumeID}
	}
	e.emitPromptDone(res.stopReason, res.usage, res.usagePartial, reset)
	return provider.TurnResult{StopReason: res.stopReason, Usage: res.usage, UsagePartial: res.usagePartial}, err
}

type turnOutcome struct {
	usagePartial bool
	stopReason   string
	usage        map[string]provider.TokenUsage
	newSessionID string
	diagnostics  []Msg
	hasText      bool
}

func (e *engine) runOnce(ctx context.Context, text string, images []provider.PromptImage, resumeID string) (turnOutcome, error) {
	turnCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.curCancel = cancel
	e.mu.Unlock()
	defer cancel()

	args, logPath, err := e.turnArgs(text, resumeID)
	if logPath != "" {
		defer func() { _ = os.Remove(logPath) }()
	}
	if err != nil {
		return turnOutcome{stopReason: "failed"}, err
	}
	// CAPA A6: greppable redacted argv (MCP flags+paths retained; no prompt/Authorization).
	log.Printf("oneshot: spawn agent=%s argv_redacted=%v", e.c.AgentName(), redactArgv(args))
	proc, err := e.startTurnProcess(args)
	if err != nil {
		return turnOutcome{stopReason: "failed"}, err
	}
	pid := proc.cmd.Process.Pid
	e.agentPID.Store(int64(pid))
	defer e.agentPID.Store(0)
	bg := newBackgroundShells(pid, e.bgGrace)
	e.bg.Store(bg)
	defer e.bg.Store(nil)
	disarmKiller := killGroupOnCancel(turnCtx, proc.cmd)
	defer disarmKiller()

	waitStdin, input, err := e.feedStdin(proc, text, images)
	if err != nil {
		return turnOutcome{stopReason: "failed"}, err
	}
	if input != nil {
		e.input.Store(input)
		defer func() {
			e.input.CompareAndSwap(input, nil)
			input.close()
		}()
	}

	acc := newTurnAccumulator(e, bg, input)
	tail := newStderrTail(64 * 1024)
	var streamErr error
	stdoutPipe := newOutPipe(proc.stdout, outputDrainGrace)
	stderrPipe := newOutPipe(proc.stderr, outputDrainGrace)
	stderrPipe.start(func(r io.Reader) { _, _ = io.Copy(tail, r) })
	stdoutPipe.start(func(r io.Reader) { streamErr = e.consumeStdout(r, acc.apply) })

	waitErr, killedAfterResult := e.waitTurnProcess(proc.cmd, bg, acc.resultSeen)
	disarmKiller()
	// resultStop is safe to read here: it was written before resultSeen closed,
	// which the supervisor observed before setting killedAfterResult.
	if killedAfterResult && acc.resultStop == "end_turn" {
		waitErr = nil
	}
	// streamErr is safe to read once joinOutputs has waited for the consumer.
	if e.joinOutputs(stdoutPipe, stderrPipe) && errors.Is(streamErr, os.ErrDeadlineExceeded) {
		streamErr = nil
	}
	if stdinErr := waitStdin(); stdinErr != nil && waitErr == nil {
		waitErr = fmt.Errorf("oneshot: write stdin: %w; stderr: %s", stdinErr, tail.String())
	} else if stdinErr != nil {
		waitErr = fmt.Errorf("oneshot: write stdin: %v; wait: %w; stderr: %s", stdinErr, waitErr, tail.String())
	}
	if turnCtx.Err() != nil {
		// Prefer the caller's cancel over a SIGKILL exit status.
		waitErr = turnCtx.Err()
	}

	e.applyLogFile(logPath, acc)
	acc.settleUsage(resumeID)
	return acc.outcome(waitErr, streamErr, tail)
}

// turnArgs builds the turn's argv. LogFile codecs also get a temp log path the
// CLI writes its side channel to; the caller removes it whenever it is non-empty.
func (e *engine) turnArgs(text, resumeID string) ([]string, string, error) {
	var args []string
	var logPath string
	if logCodec, ok := e.c.(LogFileCodec); ok {
		f, ferr := os.CreateTemp("", "agent-log-*.log")
		if ferr != nil {
			return nil, "", fmt.Errorf("oneshot: allocate log file: %w", ferr)
		}
		logPath = f.Name()
		if cerr := f.Close(); cerr != nil {
			return nil, logPath, fmt.Errorf("oneshot: close log file: %w", cerr)
		}
		args = logCodec.ArgsWithLog(e.opts, text, resumeID, logPath)
	} else {
		args = e.c.Args(e.opts, text, resumeID)
	}
	if len(args) == 0 {
		return nil, logPath, errors.New("oneshot: codec produced empty argv")
	}
	return args, logPath, nil
}

// applyLogFile recovers session id / structured errors a LogFile codec wrote
// to its side channel. No-op for other codecs.
func (e *engine) applyLogFile(logPath string, acc *turnAccumulator) {
	logCodec, ok := e.c.(LogFileCodec)
	if !ok || logPath == "" {
		return
	}
	data, rerr := os.ReadFile(logPath)
	if rerr != nil {
		log.Printf("oneshot: read log file %s: %v", logPath, rerr)
	} else if len(data) > 0 {
		acc.apply(logCodec.ParseLogFile(data))
	}
}

// turnProcess is a started turn CLI and the parent's ends of its streams.
type turnProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File
	stderr *os.File
}

func (e *engine) startTurnProcess(args []string) (*turnProcess, error) {
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
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		closeFiles(stdoutR, stdoutW)
		return nil, err
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	var stdin io.WriteCloser
	if e.c.PromptViaStdin() {
		if stdin, err = cmd.StdinPipe(); err != nil {
			closeFiles(stdoutR, stdoutW, stderrR, stderrW)
			return nil, err
		}
	}
	if err := cmd.Start(); err != nil {
		closeFiles(stdoutR, stdoutW, stderrR, stderrW)
		return nil, err
	}
	// The child holds its own copies now; drop the parent's write ends or even a
	// well-behaved CLI's pipes would never reach EOF.
	closeFiles(stdoutW, stderrW)
	return &turnProcess{cmd: cmd, stdin: stdin, stdout: stdoutR, stderr: stderrR}, nil
}

// killGroupOnCancel SIGKILLs the process group (negative pgid) when ctx ends.
// The returned disarm must be called as soon as the CLI is known to be gone:
// signalling the group after that only reaches the children it left behind,
// e.g. a service the agent started for a preview.
func killGroupOnCancel(ctx context.Context, cmd *exec.Cmd) (disarm func()) {
	killCh := make(chan struct{})
	var killerOff sync.Once
	disarm = func() { killerOff.Do(func() { close(killCh) }) }
	go func() {
		select {
		case <-ctx.Done():
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
	return disarm
}

// feedStdin writes the prompt in a goroutine so large prompts cannot deadlock
// against a child that is simultaneously filling the stdout pipe buffer. The
// returned wait blocks until the write finished and reports its error. A
// TurnInputCodec keeps stdin open behind the returned turnInput; otherwise
// stdin closes after the prompt. When the prompt cannot be encoded the process
// is reaped and its pipes closed.
func (e *engine) feedStdin(proc *turnProcess, text string, images []provider.PromptImage) (wait func() error, input *turnInput, err error) {
	stdin := proc.stdin
	if stdin == nil {
		return func() error { return nil }, nil, nil
	}
	enc, encodes := e.c.(StdinEncoder)
	if !encodes && len(images) > 0 {
		_ = stdin.Close()
		_ = proc.cmd.Process.Kill()
		_ = proc.cmd.Wait()
		closeFiles(proc.stdout, proc.stderr)
		return nil, nil, fmt.Errorf("oneshot: agent %q 图片附件需要 StdinEncoder", e.c.AgentName())
	}
	if _, ok := turnInputCodec(e.c); ok {
		input, wait = openTurnInput(stdin, enc.StdinBytes(text, images))
		return wait, input, nil
	}
	var stdinErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		var werr error
		if encodes {
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
	return func() error {
		<-done
		return stdinErr
	}, input, nil
}

// waitTurnProcess waits for the CLI to exit under superviseTurn. The turn ends
// when the CLI process exits, never when its pipes reach EOF: a service the
// agent started during the turn inherits the write ends, so EOF can be
// arbitrarily far away (or never come at all).
func (e *engine) waitTurnProcess(cmd *exec.Cmd, bg *backgroundShells, resultSeen <-chan struct{}) (waitErr error, killedAfterResult bool) {
	exited := make(chan struct{})
	var killed atomic.Bool
	supervised := make(chan struct{})
	go func() {
		defer close(supervised)
		e.superviseTurn(cmd.Process.Pid, bg, resultExitGrace, resultSeen, exited, &killed)
	}()
	waitErr = cmd.Wait()
	close(exited)
	<-supervised
	return waitErr, killed.Load()
}

// joinOutputs bounds the remaining reads now that the CLI exited and waits for
// both consumers. It reports whether either pipe had to be abandoned because a
// descendant of the CLI still holds its write end.
func (e *engine) joinOutputs(stdout, stderr *outPipe) bool {
	stdout.startDraining()
	stderr.startDraining()
	leakedOut, leakedErr := stdout.join(), stderr.join()
	if !leakedOut && !leakedErr {
		return false
	}
	log.Printf("oneshot: agent=%s 已退出但输出管道仍被其后代进程持有，单次读取超过 %s 后放弃（本回合可能在前台或未重定向地起了常驻服务，正确写法：setsid nohup cmd </dev/null >log 2>&1 &）",
		e.c.AgentName(), outputDrainGrace)
	return true
}

// superviseTurn runs beside a turn's CLI until it exits. It ends background
// shells the CLI is only waiting on, and kills a CLI that reported its result
// but has not exited within exitGrace (some versions keep a worker alive);
// that turn still counts as finished.
func (e *engine) superviseTurn(pid int, bg *backgroundShells, exitGrace time.Duration, resultSeen, exited <-chan struct{}, killedAfterResult *atomic.Bool) {
	tick := time.NewTicker(backgroundCheckEvery(bg.grace))
	defer tick.Stop()
	var lingering <-chan time.Time
	for {
		select {
		case <-exited:
			return
		case <-resultSeen:
			resultSeen = nil
			t := time.NewTimer(exitGrace)
			defer t.Stop()
			lingering = t.C
		case <-lingering:
			log.Printf("oneshot: agent=%s 已给出结果但 %s 后仍未退出，结束其进程组", e.c.AgentName(), exitGrace)
			killedAfterResult.Store(true)
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return
		case now := <-tick.C:
			if shells := bg.due(now); len(shells) > 0 {
				log.Printf("oneshot: agent=%s 已安静 %s，只在等 %d 个后台任务，结束这些后台任务让它收尾（常驻服务请用 setsid nohup cmd </dev/null >log 2>&1 & 启动）",
					e.c.AgentName(), bg.grace, len(shells))
				terminateShells(shells)
			}
		}
	}
}

// WaitingOnBackground reports whether the running turn's CLI is only waiting
// on background shells, so its own bookkeeping IO is not mistaken for work.
func (e *engine) WaitingOnBackground() bool {
	if bg := e.bg.Load(); bg != nil {
		return bg.waiting()
	}
	return false
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
		_ = e.Cancel()
		if e.cleanup != nil {
			e.cleanup()
		}
		close(e.done)
	})
	return nil
}

func (e *engine) Done() <-chan struct{} { return e.done }

// AgentPIDs is the CLI running the current turn (empty between turns).
func (e *engine) AgentPIDs() []int {
	if pid := int(e.agentPID.Load()); pid > 0 {
		return []int{pid}
	}
	return nil
}

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
		if m.DurationMs > 0 {
			update["durationMs"] = m.DurationMs
		}
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
		e.emit(map[string]any{"op": "raw", "type": "error_text", "text": provider.RedactDiagnostic(m.Text, e.env)})
		return
	default:
		return
	}
	e.emit(map[string]any{"type": "session_update", "sessionId": e.SessionID(), "update": update})
}

func (e *engine) emitPromptDone(stopReason string, usage map[string]provider.TokenUsage, partial bool, extra map[string]any) {
	frame := map[string]any{"type": "prompt_done", "sessionId": e.SessionID(), "stopReason": stopReason}
	if e.c.ReportsUsage() && len(usage) > 0 {
		frame["usage"] = usage
		if partial {
			frame["usagePartial"] = true
		}
	}
	for k, v := range extra {
		frame[k] = v
	}
	e.emit(frame)
}

// resumeRetryDelays spaces same-session retries after a connection failure.
// A var (not const) so tests can shrink it.
var resumeRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second}

var transientMarkers = []string{
	"failed to reach", "econnreset", "econnrefused", "econnaborted", "etimedout", "enotfound",
	"eai_again", "enetunreach", "ehostunreach", "socket hang up", "network", "tls", "handshake",
	"connection refused", "connection reset", "connection closed", "timed out",
	"temporary failure in name resolution", "no such host",
}

// transientFailure reports whether a failed attempt looks like the CLI could
// not reach its API, as opposed to rejecting the request or its session.
func transientFailure(err error, res turnOutcome) bool {
	texts := make([]string, 0, len(res.diagnostics)+1)
	if err != nil {
		texts = append(texts, err.Error())
	}
	for _, d := range res.diagnostics {
		texts = append(texts, d.Text)
	}
	for _, text := range texts {
		lower := strings.ToLower(text)
		for _, marker := range transientMarkers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
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

func sumUsage(usage map[string]provider.TokenUsage) provider.TokenUsage {
	var total provider.TokenUsage
	for _, bucket := range usage {
		total = addUsage(total, bucket)
	}
	return total
}

func mergeUsage(a, b map[string]provider.TokenUsage) map[string]provider.TokenUsage {
	if len(a) == 0 {
		return b
	}
	out := make(map[string]provider.TokenUsage, len(a)+len(b))
	for model, usage := range a {
		out[model] = usage
	}
	for model, usage := range b {
		out[model] = addUsage(out[model], usage)
	}
	return out
}

func nonEmptyUsage(u map[string]provider.TokenUsage) map[string]provider.TokenUsage {
	if len(u) == 0 {
		return nil
	}
	return u
}

func zeroUsage(usage map[string]provider.TokenUsage) bool {
	for _, u := range usage {
		if u.InputTokens != 0 || u.OutputTokens != 0 || u.CacheReadTokens != 0 || u.CacheWriteTokens != 0 {
			return false
		}
	}
	return true
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
