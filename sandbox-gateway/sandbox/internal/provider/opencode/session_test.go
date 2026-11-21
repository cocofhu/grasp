package opencode

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
	"backend/internal/provider/opencode/opencodetest"
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

// TestFakeOpenCodeProcess is the fake `opencode` executable.
func TestFakeOpenCodeProcess(t *testing.T) {
	if os.Getenv("GRASP_FAKE_OPENCODE") == "" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	err := opencodetest.Serve(args, opencodetest.Options{
		Version: "1.19.4", Reply: "hello from opencode", Reasoning: "thinking it over",
		RequestLog: os.Getenv("GRASP_FAKE_OPENCODE_LOG"), SessionStore: os.Getenv("GRASP_FAKE_OPENCODE_STORE"),
		IgnoreAbort: os.Getenv("GRASP_FAKE_OPENCODE_IGNORE_ABORT") != "",
	})
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

type fakeEnv struct {
	bin, log, store string
	env             []string
}

func installFake(t *testing.T, extra ...string) fakeEnv {
	t.Helper()
	dir := t.TempDir()
	f := fakeEnv{bin: filepath.Join(dir, "opencode"), log: filepath.Join(dir, "requests"), store: filepath.Join(dir, "sessions")}
	script := "#!/bin/sh\nexec \"$GRASP_FAKE_OPENCODE_BIN\" -test.run=^TestFakeOpenCodeProcess$ -- \"$@\"\n"
	if err := os.WriteFile(f.bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	f.env = append(os.Environ(), "GRASP_FAKE_OPENCODE=1", "GRASP_FAKE_OPENCODE_BIN="+os.Args[0],
		"GRASP_FAKE_OPENCODE_LOG="+f.log, "GRASP_FAKE_OPENCODE_STORE="+f.store)
	f.env = append(f.env, extra...)
	return f
}

func (f fakeEnv) open(t *testing.T, resumeID, model, variant string) (*session, *recorder) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv, err := startServer(context.Background(), f.bin, t.TempDir(), f.env, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s := newSession(srv, provider.OpenOptions{Cwd: t.TempDir()}, f.env, rec.add, nil)
	s.model, s.variant = parseModel(model), variant
	if err := s.handshake(ctx, resumeID); err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, rec
}

func (f fakeEnv) prompts(t *testing.T) []opencodetest.Prompt {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil {
		t.Fatal(err)
	}
	var out []opencodetest.Prompt
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var req opencodetest.Request
		if json.Unmarshal([]byte(line), &req) != nil || !strings.HasSuffix(req.Path, "/prompt_async") {
			continue
		}
		var p opencodetest.Prompt
		if err := json.Unmarshal(req.Body, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func promptCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPromptStreamsTheTurnAndItsUsage(t *testing.T) {
	f := installFake(t)
	s, rec := f.open(t, "", "deepseek/deepseek-v4-pro", "high")
	res, err := s.Prompt(promptCtx(t), "hello", nil)
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("turn: %+v %v", res, err)
	}
	if got := rec.text("agent_message_chunk"); got != "hello from opencode" {
		t.Fatalf("reply streamed as %q", got)
	}
	if got := rec.text("agent_thought_chunk"); got != "thinking it over" {
		t.Fatalf("reasoning streamed as %q", got)
	}
	want := provider.TokenUsage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 4, CacheWriteTokens: 1}
	if res.Usage["default"] != want || res.UsagePartial {
		t.Fatalf("usage: %+v partial=%v", res.Usage, res.UsagePartial)
	}
	done := rec.ofType("prompt_done")
	if len(done) != 1 || done[0]["stopReason"] != "end_turn" {
		t.Fatalf("prompt_done: %v", done)
	}
	if _, err := s.Prompt(promptCtx(t), "again", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.CumulativeUsage()["default"]; got.InputTokens != 20 || got.OutputTokens != 10 {
		t.Fatalf("cumulative usage: %+v", got)
	}
	prompts := f.prompts(t)
	if len(prompts) != 2 || prompts[0].Model == nil || prompts[0].Model.ProviderID != "deepseek" ||
		prompts[0].Model.ModelID != "deepseek-v4-pro" || prompts[0].Variant != "high" {
		t.Fatalf("prompt routing: %+v", prompts)
	}
}

func TestDefaultModelSendsNoRouting(t *testing.T) {
	f := installFake(t)
	s, _ := f.open(t, "", "", "")
	if _, err := s.Prompt(promptCtx(t), "hello", nil); err != nil {
		t.Fatal(err)
	}
	if p := f.prompts(t); len(p) != 1 || p[0].Model != nil || p[0].Variant != "" {
		t.Fatalf("default routing: %+v", p)
	}
}

func TestSteerJoinsTheRunningTurn(t *testing.T) {
	f := installFake(t)
	s, rec := f.open(t, "", "", "")
	if err := s.Steer("early"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("steer without a turn: %v", err)
	}
	type result struct {
		res provider.TurnResult
		err error
	}
	out := make(chan result, 1)
	go func() {
		res, err := s.Prompt(promptCtx(t), "hang until steered", nil)
		out <- result{res, err}
	}()
	waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
	if err := s.Steer("now finish"); err != nil {
		t.Fatal(err)
	}
	r := <-out
	if r.err != nil || r.res.StopReason != "end_turn" {
		t.Fatalf("steered turn: %+v %v", r.res, r.err)
	}
	if len(rec.ofType("prompt_done")) != 1 || rec.text("agent_message_chunk") != "hello from opencode" {
		t.Fatalf("steer was not answered inside the turn: %v", rec.snapshot())
	}
	if p := f.prompts(t); len(p) != 2 || p[1].Text() != "now finish" {
		t.Fatalf("prompts: %+v", p)
	}
}

func TestCancelAbortsTheTurnAndKeepsTheServer(t *testing.T) {
	f := installFake(t)
	s, rec := f.open(t, "", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
		cancel()
	}()
	res, err := s.Prompt(ctx, "hang", nil)
	if !errors.Is(err, context.Canceled) || res.StopReason != "cancelled" {
		t.Fatalf("cancelled turn: %+v %v", res, err)
	}
	// The killed tool reports itself or the ended turn closes it, once.
	if updates := rec.updates("tool_call_update"); len(updates) != 1 {
		t.Fatalf("running tool not closed once: %v", updates)
	}
	frames := rec.snapshot()
	if last := frames[len(frames)-1]; last["type"] != "prompt_done" || last["stopReason"] != "cancelled" {
		t.Fatalf("frames after prompt_done: %v", frames)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(rec.snapshot()); n != len(frames) {
		t.Fatalf("late events of the aborted run reached the transcript: %v", rec.snapshot()[len(frames):])
	}
	if res, err := s.Prompt(promptCtx(t), "hello", nil); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("server did not survive the abort: %+v %v", res, err)
	}
}

func TestContextCauses(t *testing.T) {
	for _, tc := range []struct {
		cause      error
		stopReason string
		promptDone bool
	}{
		{provider.ErrTurnRecover, "recover", false},
		{fmt.Errorf("%w: stuck", provider.ErrTurnTimeout), provider.TimeoutStopReason(fmt.Errorf("%w: stuck", provider.ErrTurnTimeout)), true},
	} {
		f := installFake(t)
		s, rec := f.open(t, "", "", "")
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

func TestUnconfirmedAbortStopsTheServer(t *testing.T) {
	old := provider.InterruptGrace
	provider.InterruptGrace = 200 * time.Millisecond
	t.Cleanup(func() { provider.InterruptGrace = old })
	f := installFake(t, "GRASP_FAKE_OPENCODE_IGNORE_ABORT=1")
	s, rec := f.open(t, "", "", "")
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
		t.Fatal("server kept running after ignoring the abort")
	}
	if msg, _ := s.ExitInfo(); !strings.Contains(msg, "OpenCode serve 已退出") {
		t.Fatalf("exit banner %q", msg)
	}
}

func TestModelErrorFailsTheTurn(t *testing.T) {
	f := installFake(t)
	s, rec := f.open(t, "", "", "")
	res, err := s.Prompt(promptCtx(t), "fail", nil)
	if err == nil || res.StopReason != "failed" || !strings.Contains(err.Error(), "upstream model failed") {
		t.Fatalf("failed turn: %+v %v", res, err)
	}
	if e := rec.ofType("error_text"); len(e) != 1 || !strings.Contains(e[0]["text"].(string), "upstream model failed") {
		t.Fatalf("error_text: %v", e)
	}
}

func TestTurnWaitsForBusySubagents(t *testing.T) {
	old := subagentPoll
	subagentPoll = 10 * time.Millisecond
	t.Cleanup(func() { subagentPoll = old })
	f := installFake(t)
	s, rec := f.open(t, "", "", "")
	res, err := s.Prompt(promptCtx(t), "start a subagent", nil)
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("turn: %+v %v", res, err)
	}
	calls, updates := rec.updates("tool_call"), rec.updates("tool_call_update")
	if len(calls) != 1 || calls[0]["title"] != "等待 1 个子 Agent 完成" || len(updates) != 1 || updates[0]["status"] != "completed" {
		t.Fatalf("subagent wait: %v %v", calls, updates)
	}
}

func TestResumeContinuesTheSessionInANewServer(t *testing.T) {
	f := installFake(t)
	first, _ := f.open(t, "", "", "")
	id := first.SessionID()
	_ = first.Close()
	again, _ := f.open(t, id, "", "")
	if again.SessionID() != id {
		t.Fatalf("resume opened %q, want %q", again.SessionID(), id)
	}
	fresh, _ := f.open(t, "ses_gone", "", "")
	if fresh.SessionID() == "ses_gone" || fresh.SessionID() == "" {
		t.Fatalf("missing session resumed as %q", fresh.SessionID())
	}
}

func TestServerExitEndsTheSession(t *testing.T) {
	f := installFake(t)
	s, _ := f.open(t, "", "", "")
	s.srv.stop()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close")
	}
	if pids := s.AgentPIDs(); len(pids) != 1 || pids[0] <= 0 {
		t.Fatalf("pids %v", pids)
	}
}

func TestStartupFailureReportsTheServerOutput(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'bad config' >&2\nexit 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := startServer(context.Background(), bin, dir, os.Environ(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(srv, provider.OpenOptions{Cwd: dir}, nil, nil, nil)
	err = s.handshake(promptCtx(t), "")
	if err == nil || !strings.Contains(err.Error(), "bad config") || !strings.Contains(err.Error(), "退出码=3") {
		t.Fatalf("startup failure: %v", err)
	}
}

func TestConfigFilesFollowXDG(t *testing.T) {
	s := &session{env: []string{"XDG_CONFIG_HOME=/cfg", "XDG_DATA_HOME=/data"}}
	want := []string{"/cfg/opencode/opencode.json", "/cfg/opencode/opencode.jsonc", "/cfg/opencode/config.json", "/data/opencode/auth.json"}
	if got := s.ConfigFiles(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("config files %v", got)
	}
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

func TestServerExitWhileWaitingForSubagentsFailsTheTurn(t *testing.T) {
	old := subagentPoll
	subagentPoll = time.Hour
	t.Cleanup(func() { subagentPoll = old })
	f := installFake(t)
	s, rec := f.open(t, "", "", "")
	time.AfterFunc(500*time.Millisecond, s.srv.stop)
	res, err := s.Prompt(promptCtx(t), "start a subagent", nil)
	if err == nil || res.StopReason != "failed" || !strings.Contains(err.Error(), "exited while subagents were running") {
		t.Fatalf("turn: %+v %v", res, err)
	}
	if updates := rec.updates("tool_call_update"); len(updates) == 0 || updates[len(updates)-1]["status"] != "failed" {
		t.Fatalf("subagent wait not closed as failed: %v", updates)
	}
}
