package codex

import (
	"context"
	"encoding/json"
	"errors"
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

func (r *recorder) reset() {
	r.mu.Lock()
	r.frames = nil
	r.mu.Unlock()
}

// updates returns the session_update payloads of one kind.
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

func open(t *testing.T, opts provider.OpenOptions) (*session, *recorder) {
	t.Helper()
	installFake(t)
	if opts.Cwd == "" {
		opts.Cwd = t.TempDir()
	}
	rec := &recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := New().Open(context.Background(), ctx, opts, rec.add, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess.(*session), rec
}

func prompt(t *testing.T, s *session, ctx context.Context, text string) (provider.TurnResult, error) {
	t.Helper()
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	return s.Prompt(ctx, text, nil)
}

// startTurn runs Prompt in the background and returns once the turn is live.
func startTurn(t *testing.T, s *session, ctx context.Context, text string) <-chan provider.TurnResult {
	t.Helper()
	type outcome = provider.TurnResult
	done := make(chan outcome, 1)
	errs := make(chan error, 1)
	go func() {
		res, err := s.Prompt(ctx, text, nil)
		errs <- err
		done <- res
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		live := s.turn != nil && s.turn.id != ""
		s.mu.Unlock()
		if live {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	out := make(chan provider.TurnResult, 1)
	go func() {
		err := <-errs
		res := <-done
		res.StopReason += "|" + errString(err)
		out <- res
	}()
	return out
}

func errString(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return err.Error()
}

func await(t *testing.T, ch <-chan provider.TurnResult) provider.TurnResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}
	return provider.TurnResult{}
}

func TestPromptMapsItemsAndUsage(t *testing.T) {
	s, rec := open(t, provider.OpenOptions{})
	if s.SessionID() != "thread-new" || s.Info().ModelID != "fake-model" || !s.ReportsUsage() || !s.Capabilities().Steer || !s.Capabilities().Resume {
		t.Fatalf("session identity: id=%q info=%+v", s.SessionID(), s.Info())
	}
	if len(s.AgentPIDs()) != 1 || s.CWD() == "" || s.FSRoot() != s.CWD() {
		t.Fatalf("pids=%v cwd=%q fsroot=%q", s.AgentPIDs(), s.CWD(), s.FSRoot())
	}
	if s.CumulativeUsage() != nil {
		t.Fatal("usage before any turn")
	}
	res, err := prompt(t, s, nil, "hello")
	if err != nil || res.StopReason != "end_turn" || res.UsagePartial {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	want := provider.TokenUsage{InputTokens: 170, OutputTokens: 60, CacheReadTokens: 30}
	if res.Usage["default"] != want {
		t.Fatalf("turn usage = %+v, want %+v", res.Usage, want)
	}
	if got := rec.text("agent_message_chunk"); got != "Hello" {
		t.Fatalf("streamed message must not repeat on completion: %q", got)
	}
	if got := rec.text("agent_thought_chunk"); got != "thinking" {
		t.Fatalf("reasoning = %q", got)
	}
	calls := rec.updates("tool_call")
	if len(calls) != 2 || calls[0]["title"] != "ls" || calls[1]["title"] != "store/get" {
		t.Fatalf("tool calls = %v", calls)
	}
	if calls[0]["rawInput"].(map[string]any)["command"] != "ls" || calls[1]["rawInput"].(map[string]any)["k"] != float64(1) {
		t.Fatalf("tool inputs = %v", calls)
	}
	updates := rec.updates("tool_call_update")
	got := map[string]string{}
	for _, u := range updates {
		text := ""
		if c, ok := u["content"].([]any); ok {
			text = c[0].(map[string]any)["content"].(map[string]any)["text"].(string)
		}
		got[u["toolCallId"].(string)] = u["status"].(string) + ":" + text
	}
	for id, want := range map[string]string{"c1": "completed:a.txt\n", "t1": "failed:tool says no", "f1": "completed:a.txt", "w1": "completed:"} {
		if got[id] != want {
			t.Fatalf("tool %s = %q, want %q (all %v)", id, got[id], want, got)
		}
	}
	plans := rec.updates("plan")
	if len(plans) != 1 {
		t.Fatalf("plans = %v", plans)
	}
	entries := plans[0]["entries"].([]any)
	if entries[1].(map[string]any)["status"] != "in_progress" || entries[0].(map[string]any)["content"] != "read" {
		t.Fatalf("plan entries = %v", entries)
	}
	if len(rec.ofType("error_text")) != 0 {
		t.Fatal("warnings and retried errors must not surface as errors")
	}
	done := rec.ofType("prompt_done")
	if len(done) != 1 || done[0]["stopReason"] != "end_turn" || done[0]["usage"] == nil || done[0]["usagePartial"] != nil {
		t.Fatalf("prompt_done = %v", done)
	}
	if _, err := prompt(t, s, nil, "hello"); err != nil {
		t.Fatal(err)
	}
	if cum := s.CumulativeUsage()["default"]; cum.InputTokens != 340 || cum.OutputTokens != 120 || cum.CacheReadTokens != 60 {
		t.Fatalf("cumulative usage = %+v", cum)
	}
}

func TestSteerJoinsRunningTurn(t *testing.T) {
	s, rec := open(t, provider.OpenOptions{})
	if err := s.Steer("early"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("steer without a turn: %v", err)
	}
	ch := startTurn(t, s, context.Background(), "wait")
	if err := s.Steer("more detail"); err != nil {
		t.Fatal(err)
	}
	if res := await(t, ch); res.StopReason != "end_turn|" {
		t.Fatalf("steered turn = %+v", res)
	}
	if got := rec.text("agent_message_chunk"); got != "steered: more detail" {
		t.Fatalf("steered output = %q", got)
	}
	if err := s.Steer("late"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("steer after the turn: %v", err)
	}
}

func TestSteerRaceWithTurnEndIsNoTurn(t *testing.T) {
	s, _ := open(t, provider.OpenOptions{})
	ch := startTurn(t, s, context.Background(), "wait")
	s.mu.Lock()
	turn := s.turn
	s.mu.Unlock()
	if err := s.Steer("x"); err != nil {
		t.Fatal(err)
	}
	await(t, ch)
	// The turn ended between the check and turn/steer: the fake rejects it.
	s.mu.Lock()
	s.turn = turn
	s.mu.Unlock()
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.mu.Lock()
		s.turn = nil
		s.mu.Unlock()
	}()
	if err := s.Steer("y"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("rejected steer on an ended turn: %v", err)
	}
}

func TestCancelInterruptsWithoutKillingProcess(t *testing.T) {
	s, rec := open(t, provider.OpenOptions{})
	if err := s.Cancel(); err != nil {
		t.Fatal(err)
	}
	ch := startTurn(t, s, context.Background(), "wait")
	if err := s.Cancel(); err != nil {
		t.Fatal(err)
	}
	if res := await(t, ch); res.StopReason != "cancelled|canceled" {
		t.Fatalf("cancelled turn = %+v", res)
	}
	if done := rec.ofType("prompt_done"); len(done) != 1 || done[0]["stopReason"] != "cancelled" {
		t.Fatalf("prompt_done = %v", done)
	}
	if res, err := prompt(t, s, nil, "hello"); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("process must survive an interrupt: %+v %v", res, err)
	}
}

func TestContextCancelCauses(t *testing.T) {
	s, rec := open(t, provider.OpenOptions{})
	for _, tc := range []struct {
		cause      error
		stop       string
		promptDone bool
		errorText  bool
	}{
		{cause: provider.ErrTurnRecover, stop: "recover|turn recover"},
		{cause: provider.ErrTurnTimeout, stop: provider.TimeoutStopReason(provider.ErrTurnTimeout) + "|turn timeout", promptDone: true, errorText: true},
		{cause: provider.ErrTurnStuck, stop: provider.TimeoutStopReason(provider.ErrTurnStuck) + "|" + provider.ErrTurnStuck.Error(), promptDone: true, errorText: true},
		{cause: context.Canceled, stop: "cancelled|canceled", promptDone: true},
	} {
		rec.reset()
		ctx, cancel := context.WithCancelCause(context.Background())
		ch := startTurn(t, s, ctx, "wait")
		cancel(tc.cause)
		if res := await(t, ch); res.StopReason != tc.stop {
			t.Fatalf("cause %v: stop = %q, want %q", tc.cause, res.StopReason, tc.stop)
		}
		if got := len(rec.ofType("prompt_done")) == 1; got != tc.promptDone {
			t.Fatalf("cause %v: prompt_done emitted=%v", tc.cause, got)
		}
		if got := len(rec.ofType("error_text")) == 1; got != tc.errorText {
			t.Fatalf("cause %v: error_text emitted=%v", tc.cause, got)
		}
	}
}

func TestUnresponsiveInterruptStopsProcess(t *testing.T) {
	old := interruptGrace
	interruptGrace = 100 * time.Millisecond
	t.Cleanup(func() { interruptGrace = old })
	s, _ := open(t, provider.OpenOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	ch := startTurn(t, s, ctx, "hang")
	cancel()
	if res := await(t, ch); res.StopReason != "cancelled|canceled" {
		t.Fatalf("hung turn = %+v", res)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("hung app-server was not stopped")
	}
	if banner, _ := s.ExitInfo(); !strings.Contains(banner, "Codex app-server") {
		t.Fatalf("banner = %q", banner)
	}
}

func TestFailedTurnsReportDiagnostics(t *testing.T) {
	s, rec := open(t, provider.OpenOptions{})
	res, err := prompt(t, s, nil, "fail")
	if res.StopReason != "failed" || err == nil || err.Error() != "provider exploded" {
		t.Fatalf("failed turn = %+v %v", res, err)
	}
	if errs := rec.ofType("error_text"); len(errs) != 1 || errs[0]["text"] != "provider exploded" {
		t.Fatalf("error_text = %v", errs)
	}
	if _, err := prompt(t, s, nil, "auth"); err == nil || err.Error() != codexLoginRepaste {
		t.Fatalf("auth failure = %v", err)
	}
	t.Setenv("GRASP_MODEL_CONNECTION", `{"x":1}`)
	if _, err := prompt(t, s, nil, "auth"); err == nil || !strings.Contains(err.Error(), "模型连接认证失败") {
		t.Fatalf("connection auth failure = %v", err)
	}
}

func TestServerRequestsAreDeclined(t *testing.T) {
	s, rec := open(t, provider.OpenOptions{})
	for prompt_, want := range map[string]string{
		"ask":    `"decision":"decline"`,
		"legacy": `"decision":"denied"`,
		"elicit": `"action":"decline"`,
	} {
		rec.reset()
		if res, err := prompt(t, s, nil, prompt_); err != nil || res.StopReason != "end_turn" {
			t.Fatalf("%s: %+v %v", prompt_, res, err)
		}
		if got := rec.text("agent_message_chunk"); !strings.Contains(got, want) {
			t.Fatalf("%s: answer %q, want %s", prompt_, got, want)
		}
	}
	if res, err := prompt(t, s, nil, "unknown-request"); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("unknown request: %+v %v", res, err)
	}
}

func TestProcessExitDuringTurn(t *testing.T) {
	s, rec := open(t, provider.OpenOptions{})
	res, err := prompt(t, s, nil, "die")
	if res.StopReason != "failed" || err == nil {
		t.Fatalf("exit during turn = %+v %v", res, err)
	}
	if len(rec.ofType("error_text")) != 1 {
		t.Fatal("exit must be reported")
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed")
	}
	if banner, waitErr := s.ExitInfo(); waitErr == nil || !strings.Contains(banner, "退出码=3") {
		t.Fatalf("banner = %q err=%v", banner, waitErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res, err := s.Prompt(ctx, "hello", nil); res.StopReason != "cancelled" || !errors.Is(err, context.Canceled) {
		t.Fatalf("prompt on a cancelled ctx = %+v %v", res, err)
	}
}

func TestAttachmentsBecomeFileReferences(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	s, _ := open(t, provider.OpenOptions{})
	res, err := s.Prompt(context.Background(), "hello", []provider.PromptImage{{MimeType: "image/png", Data: "aGk="}})
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("prompt with image = %+v %v", res, err)
	}
	if res, err := s.Prompt(context.Background(), "hello", []provider.PromptImage{{MimeType: "image/png", Data: "%%%"}}); err == nil || res.StopReason != "failed" {
		t.Fatalf("undecodable attachment = %+v %v", res, err)
	}
}

func TestResumeUsesExistingThread(t *testing.T) {
	s, _ := open(t, provider.OpenOptions{ResumeSessionID: "thread-old", Model: "gpt-x", FSRoot: "/fs"})
	if s.SessionID() != "thread-old" || s.FSRoot() != "/fs" {
		t.Fatalf("resumed id = %q fsroot=%q", s.SessionID(), s.FSRoot())
	}
	if res, err := prompt(t, s, nil, "hello"); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("resumed turn = %+v %v", res, err)
	}
}

// Naming the model is what makes a resumed thread take this process's
// reasoning effort and speed instead of the ones it was recorded with.
func TestResumeNamesTheModelANewThreadWouldUse(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts provider.OpenOptions
		want string
	}{
		{"selected model", provider.OpenOptions{Model: "gpt-x"}, "gpt-x"},
		{"configured model", provider.OpenOptions{CustomArgs: []string{"-c", `model="cfg-model"`}}, "cfg-model"},
		{"catalog default", provider.OpenOptions{}, "fake-default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "model")
			t.Setenv("GRASP_FAKE_CODEX_RESUME_MODEL", record)
			tc.opts.ResumeSessionID = "thread-old"
			open(t, tc.opts)
			if got, _ := os.ReadFile(record); string(got) != tc.want {
				t.Fatalf("thread/resume model = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResumeOfAMissingThreadStartsFresh(t *testing.T) {
	s, _ := open(t, provider.OpenOptions{ResumeSessionID: "thread-gone"})
	if s.SessionID() != "thread-new" {
		t.Fatalf("a rejected resume must start a new thread, got %q", s.SessionID())
	}
	if res, err := prompt(t, s, nil, "hello"); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("fresh turn = %+v %v", res, err)
	}
}

func TestOpenPassesProcessConfig(t *testing.T) {
	argsFile := installFake(t)
	sess, err := New().Open(context.Background(), context.Background(), provider.OpenOptions{
		Cwd: t.TempDir(), ReasoningEffort: "low", ServiceTier: "fast", CustomArgs: []string{"-c", "x=1"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(strings.Split(string(raw), "\n")[1:], " ")
	for _, want := range []string{"-- app-server", "-c x=1", `model_reasoning_effort="low"`, "features.fast_mode=true", `service_tier="fast"`} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv %q lacks %q", argv, want)
		}
	}
}

func TestOpenFailures(t *testing.T) {
	installFake(t)
	t.Setenv("PATH", t.TempDir())
	if _, err := New().Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir()}, nil, nil); err == nil || !strings.Contains(err.Error(), "not found on PATH") {
		t.Fatalf("missing binary: %v", err)
	}
	installFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Open(context.Background(), ctx, provider.OpenOptions{Cwd: t.TempDir()}, nil, nil); err == nil || !strings.Contains(err.Error(), "codex initialize") {
		t.Fatalf("cancelled handshake: %v", err)
	}
}
