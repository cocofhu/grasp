package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"
)

type recorder struct {
	mu     sync.Mutex
	frames []map[string]any
}

func (r *recorder) add(raw json.RawMessage) {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	r.mu.Lock()
	r.frames = append(r.frames, m)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.frames...)
}

func (r *recorder) updates(kind string) []map[string]any {
	var out []map[string]any
	for _, f := range r.snapshot() {
		if u, ok := f["update"].(map[string]any); ok && u["sessionUpdate"] == kind {
			out = append(out, u)
		}
	}
	return out
}

func (r *recorder) ofType(typ string) []map[string]any {
	var out []map[string]any
	for _, f := range r.snapshot() {
		if f["type"] == typ {
			out = append(out, f)
		}
	}
	return out
}

func (r *recorder) text(kind string) string {
	var sb strings.Builder
	for _, u := range r.updates(kind) {
		sb.WriteString(u["content"].(map[string]any)["text"].(string))
	}
	return sb.String()
}

func installFake(t *testing.T, extra ...string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexec \"$GRASP_FAKE_PI_BIN\" -test.run=^TestFakePiProcess$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "pi"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GRASP_FAKE_PI", "1")
	t.Setenv("GRASP_FAKE_PI_BIN", os.Args[0])
	t.Setenv("HOME", t.TempDir())
	for _, kv := range extra {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
}

func open(t *testing.T, opts provider.OpenOptions) (*session, *recorder) {
	t.Helper()
	if opts.Cwd == "" {
		opts.Cwd = t.TempDir()
	}
	rec := &recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := New(Config{Bin: "pi", Runtime: "pi-rpc", ConfigRoot: t.TempDir(), SessionDir: t.TempDir()})
	sess, err := p.Open(context.Background(), ctx, opts, rec.add, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess.(*session), rec
}

func turnCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Error("condition not reached")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPromptStreamsTheTurnAndItsUsage(t *testing.T) {
	installFake(t)
	s, rec := open(t, provider.OpenOptions{})
	if s.Info().ModelID != "fake/fake-model" || !strings.HasSuffix(s.SessionID(), ".jsonl") {
		t.Fatalf("handshake: %+v %s", s.Info(), s.SessionID())
	}
	res, err := s.Prompt(turnCtx(t), "hello", nil)
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("turn: %+v %v", res, err)
	}
	if got := rec.text("agent_message_chunk"); got != "hello from pi" {
		t.Fatalf("reply %q (control tokens must be stripped)", got)
	}
	if got := rec.text("agent_thought_chunk"); got != "thinking it over" {
		t.Fatalf("thought %q", got)
	}
	want := provider.TokenUsage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 4, CacheWriteTokens: 1}
	if res.Usage["fake-model"] != want {
		t.Fatalf("usage %+v", res.Usage)
	}
	frames := rec.snapshot()
	if last := frames[len(frames)-1]; last["type"] != "prompt_done" {
		t.Fatalf("output after prompt_done: %v", last)
	}
	if _, err := s.Prompt(turnCtx(t), "again", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.CumulativeUsage()["fake-model"]; got.InputTokens != 20 {
		t.Fatalf("cumulative %+v", got)
	}
}

func TestOpenPassesModelAndThinking(t *testing.T) {
	installFake(t)
	p := New(Config{Bin: "pi", SessionDir: t.TempDir()}).(piProvider)
	file, err := p.sessionFile("")
	if err != nil || filepath.Dir(file) != p.c.SessionDir {
		t.Fatalf("session file %q %v", file, err)
	}
	if got, _ := p.sessionFile("/missing/session.jsonl"); got == "/missing/session.jsonl" {
		t.Fatal("missing session file resumed")
	}
	existing := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.sessionFile(existing); got != existing {
		t.Fatalf("existing session file not resumed: %q", got)
	}
}

func TestSteerJoinsTheRunningTurn(t *testing.T) {
	installFake(t)
	s, rec := open(t, provider.OpenOptions{})
	if err := s.Steer("early"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("steer without a turn: %v", err)
	}
	out := make(chan error, 1)
	go func() {
		res, err := s.Prompt(turnCtx(t), "hang until steered", nil)
		if err == nil && res.StopReason != "end_turn" {
			err = fmt.Errorf("stop reason %q", res.StopReason)
		}
		out <- err
	}()
	waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
	if err := s.Steer("now finish"); err != nil {
		t.Fatal(err)
	}
	if err := <-out; err != nil {
		t.Fatal(err)
	}
	if len(rec.ofType("prompt_done")) != 1 || rec.text("agent_message_chunk") != "hello from pi" {
		t.Fatalf("steer not answered inside the turn: %v", rec.snapshot())
	}
}

func TestSteerQueuedAfterTheRunEndedIsTakenInOnce(t *testing.T) {
	installFake(t)
	users := filepath.Join(t.TempDir(), "users")
	t.Setenv("GRASP_FAKE_PI_USERS", users)
	s, rec := open(t, provider.OpenOptions{})
	out := make(chan error, 1)
	go func() {
		_, err := s.Prompt(turnCtx(t), "strand", nil)
		out <- err
	}()
	waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
	if err := s.Steer("too late"); err != nil {
		t.Fatal(err)
	}
	if err := <-out; err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(users)
	if string(got) != "\"strand\"\n\"too late\"\n" {
		t.Fatalf("user messages taken in:\n%s", got)
	}
	if n := len(rec.ofType("prompt_done")); n != 1 {
		t.Fatalf("prompt_done = %d", n)
	}
	if res, err := s.Prompt(turnCtx(t), "hello", nil); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("next turn: %+v %v", res, err)
	}
	got, _ = os.ReadFile(users)
	if strings.Count(string(got), "too late") != 1 {
		t.Fatalf("steer taken in again:\n%s", got)
	}
}

func TestQueueCheckWaitsOutStreamingAfterTheRunEnded(t *testing.T) {
	installFake(t)
	users := filepath.Join(t.TempDir(), "users")
	t.Setenv("GRASP_FAKE_PI_USERS", users)
	t.Setenv("GRASP_FAKE_PI_LATE_STREAMING", "3")
	s, rec := open(t, provider.OpenOptions{})
	out := make(chan error, 1)
	go func() {
		_, err := s.Prompt(turnCtx(t), "strand", nil)
		out <- err
	}()
	waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
	if err := s.Steer("too late"); err != nil {
		t.Fatal(err)
	}
	if err := <-out; err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(users); string(got) != "\"strand\"\n\"too late\"\n" {
		t.Fatalf("user messages taken in:\n%s", got)
	}
}

func TestUnknownQueueStateFailsTheTurn(t *testing.T) {
	installFake(t)
	t.Setenv("GRASP_FAKE_PI_STATE_FAILS", "1")
	s, rec := open(t, provider.OpenOptions{})
	out := make(chan error, 1)
	go func() {
		_, err := s.Prompt(turnCtx(t), "strand", nil)
		out <- err
	}()
	waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
	if err := s.Steer("too late"); err != nil {
		t.Fatal(err)
	}
	if err := <-out; err == nil {
		t.Fatal("a turn whose steer may be stranded ended as success")
	}
}

// Queue checks outlive their test by a moment, so they are shortened for the
// whole package rather than per test.
func init() { queueChecks, queueCheckDelay = 5, 10*time.Millisecond }

func TestCancelAbortsTheRunAndKeepsTheProcess(t *testing.T) {
	installFake(t)
	s, rec := open(t, provider.OpenOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
		cancel()
	}()
	res, err := s.Prompt(ctx, "hang", nil)
	if !errors.Is(err, context.Canceled) || res.StopReason != "cancelled" {
		t.Fatalf("cancelled turn: %+v %v", res, err)
	}
	if u := rec.updates("tool_call_update"); len(u) != 1 || u[0]["status"] != "failed" {
		t.Fatalf("aborted tool: %v", u)
	}
	if res, err := s.Prompt(turnCtx(t), "hello", nil); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("process did not survive the abort: %+v %v", res, err)
	}
}

func TestContextCauses(t *testing.T) {
	timeout := fmt.Errorf("%w: stuck", provider.ErrTurnTimeout)
	for _, tc := range []struct {
		cause      error
		stopReason string
		promptDone bool
	}{
		{provider.ErrTurnRecover, "recover", false},
		{timeout, provider.TimeoutStopReason(timeout), true},
	} {
		installFake(t)
		s, rec := open(t, provider.OpenOptions{})
		ctx, cancel := context.WithCancelCause(context.Background())
		go func() {
			waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
			cancel(tc.cause)
		}()
		res, err := s.Prompt(ctx, "hang", nil)
		if !errors.Is(err, tc.cause) || res.StopReason != tc.stopReason {
			t.Fatalf("cause %v: %+v %v", tc.cause, res, err)
		}
		if got := len(rec.ofType("prompt_done")) == 1; got != tc.promptDone {
			t.Fatalf("cause %v: prompt_done=%v", tc.cause, got)
		}
	}
}

func TestUnconfirmedAbortKillsTheProcess(t *testing.T) {
	old := provider.InterruptGrace
	provider.InterruptGrace = 200 * time.Millisecond
	t.Cleanup(func() { provider.InterruptGrace = old })
	installFake(t, "GRASP_FAKE_PI_IGNORE_ABORT=1")
	s, rec := open(t, provider.OpenOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
		cancel()
	}()
	if res, _ := s.Prompt(ctx, "hang", nil); res.StopReason != "cancelled" {
		t.Fatalf("stop reason %q", res.StopReason)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process kept running after ignoring the abort")
	}
	if msg, _ := s.ExitInfo(); !strings.Contains(msg, "Pi 已退出") {
		t.Fatalf("exit banner %q", msg)
	}
}

func TestModelErrorFailsTheTurn(t *testing.T) {
	installFake(t)
	s, rec := open(t, provider.OpenOptions{})
	res, err := s.Prompt(turnCtx(t), "fail", nil)
	if err == nil || res.StopReason != "failed" || !strings.Contains(err.Error(), "upstream model failed") {
		t.Fatalf("failed turn: %+v %v", res, err)
	}
	if e := rec.ofType("error_text"); len(e) != 1 {
		t.Fatalf("error_text: %v", e)
	}
}

func TestAnAutomaticRetryDecidesTheTurn(t *testing.T) {
	t.Run("recovers", func(t *testing.T) {
		installFake(t)
		s, rec := open(t, provider.OpenOptions{})
		res, err := s.Prompt(turnCtx(t), "retry", nil)
		if err != nil || res.StopReason != "end_turn" || rec.text("agent_message_chunk") != "recovered" {
			t.Fatalf("turn: %+v %v %q", res, err, rec.text("agent_message_chunk"))
		}
		if e := rec.ofType("error_text"); len(e) != 0 {
			t.Fatalf("the retried attempt's error was reported: %v", e)
		}
		if frames := rec.snapshot(); frames[len(frames)-1]["type"] != "prompt_done" {
			t.Fatalf("output after prompt_done: %v", frames[len(frames)-1])
		}
	})
	t.Run("gives up", func(t *testing.T) {
		installFake(t)
		s, _ := open(t, provider.OpenOptions{})
		res, err := s.Prompt(turnCtx(t), "retry giveup", nil)
		if err == nil || res.StopReason != "failed" || !strings.Contains(err.Error(), "Retry cancelled") {
			t.Fatalf("turn: %+v %v", res, err)
		}
	})
}

func TestTheNextTurnWaitsForTheLastRunToClose(t *testing.T) {
	installFake(t, "GRASP_FAKE_PI_LINGER_MS=10")
	s, _ := open(t, provider.OpenOptions{})
	for _, text := range []string{"hello", "again"} {
		if res, err := s.Prompt(turnCtx(t), text, nil); err != nil || res.StopReason != "end_turn" {
			t.Fatalf("%s: %+v %v", text, res, err)
		}
	}
}

func TestExtensionDialogsAreDismissed(t *testing.T) {
	installFake(t)
	s, _ := open(t, provider.OpenOptions{})
	if res, err := s.Prompt(turnCtx(t), "dialog", nil); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("dialog turn: %+v %v", res, err)
	}
}

func TestResumeReopensTheSessionFile(t *testing.T) {
	installFake(t)
	first, _ := open(t, provider.OpenOptions{})
	file := first.SessionID()
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	again, _ := open(t, provider.OpenOptions{ResumeSessionID: file})
	if again.SessionID() != file {
		t.Fatalf("resume opened %q, want %q", again.SessionID(), file)
	}
}

func TestProcessExitEndsTheSession(t *testing.T) {
	installFake(t)
	s, _ := open(t, provider.OpenOptions{})
	s.proc.stop()
	if _, err := s.Prompt(turnCtx(t), "hello", nil); err == nil {
		t.Fatal("prompt on an exited process succeeded")
	}
	if pids := s.AgentPIDs(); len(pids) != 1 || pids[0] <= 0 {
		t.Fatalf("pids %v", pids)
	}
}

func TestStartupFailureReportsTheOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pi"), []byte("#!/bin/sh\necho 'no api key' >&2\nexit 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := New(Config{Bin: "pi", SessionDir: t.TempDir()}).Open(ctx, ctx, provider.OpenOptions{Cwd: dir}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no api key") || !strings.Contains(err.Error(), "退出码=3") {
		t.Fatalf("startup failure: %v", err)
	}
}

func TestConfigFiles(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	s := &session{}
	want := "/home/u/.pi/agent/settings.json,/home/u/.pi/agent/models.json,/home/u/.pi/agent/auth.json"
	if got := strings.Join(s.ConfigFiles(), ","); got != want {
		t.Fatalf("config files %s", got)
	}
}

func TestAnInterruptedPromptRequestKeepsTheCancelCause(t *testing.T) {
	for _, c := range []struct {
		cause error
		stop  string
		done  bool
	}{{provider.ErrTurnRecover, "recover", false}, {provider.ErrTurnTimeout, "timeout", true}, {provider.ErrTurnStuck, "stuck", true}} {
		installFake(t)
		s, rec := open(t, provider.OpenOptions{})
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(c.cause)
		res, err := s.Prompt(ctx, "hello", nil)
		if !errors.Is(err, c.cause) || res.StopReason != c.stop || (len(rec.ofType("prompt_done")) == 1) != c.done {
			t.Fatalf("cause %v: %+v %v", c.cause, res, err)
		}
	}
}
