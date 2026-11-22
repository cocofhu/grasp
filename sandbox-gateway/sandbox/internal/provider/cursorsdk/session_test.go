package cursorsdk

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

func (r *recorder) updates(kind string) []map[string]any {
	var out []map[string]any
	for _, f := range r.snapshot() {
		if u, ok := f["update"].(map[string]any); ok && u["sessionUpdate"] == kind {
			out = append(out, u)
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

func (r *recorder) ofType(typ string) []map[string]any {
	var out []map[string]any
	for _, f := range r.snapshot() {
		if f["type"] == typ {
			out = append(out, f)
		}
	}
	return out
}

// installFake points the provider at the fake bridge and an SDK directory
// that passes the install check.
func installFake(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	node := filepath.Join(dir, "node")
	script := "#!/bin/sh\nexec \"$GRASP_FAKE_CURSOR_BIN\" -test.run=^TestFakeBridge$\n"
	if err := os.WriteFile(node, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	sdk := filepath.Join(dir, "sdk")
	if err := os.MkdirAll(filepath.Join(sdk, "node_modules", "@cursor", "sdk"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdk, "node_modules", "@cursor", "sdk", "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	models, _ := filepath.Abs("testdata/models.json")
	t.Setenv("GRASP_FAKE_CURSOR_BRIDGE", "1")
	t.Setenv("GRASP_FAKE_CURSOR_BIN", os.Args[0])
	t.Setenv("GRASP_FAKE_CURSOR_MODELS", models)
	t.Setenv("GRASP_CURSOR_SDK_DIR", "")
	t.Setenv("CURSOR_API_KEY", "key-test")
	return Config{Node: node, SDKDir: sdk, Runtime: "cursor-sdk"}
}

func open(t *testing.T, c Config, opts provider.OpenOptions) (*session, *recorder) {
	t.Helper()
	if opts.Cwd == "" {
		opts.Cwd = t.TempDir()
	}
	rec := &recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := New(c).Open(context.Background(), ctx, opts, rec.add, nil)
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
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPromptStreamsTheTurnAndItsUsage(t *testing.T) {
	s, rec := open(t, installFake(t), provider.OpenOptions{Model: "gpt-5.5", ReasoningEffort: "xhigh"})
	if s.SessionID() != "agent-new" || s.Info().ModelID != "gpt-5.5" || !strings.Contains(s.Info().ModelName, "reasoning=extra-high") {
		t.Fatalf("session %q info %+v", s.SessionID(), s.Info())
	}
	res, err := s.Prompt(turnCtx(t), "hi", nil)
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("res %+v err %v", res, err)
	}
	want := provider.TokenUsage{InputTokens: 25, OutputTokens: 10, CacheReadTokens: 60, CacheWriteTokens: 15}
	if res.Usage["gpt-5.5"] != want || res.UsagePartial {
		t.Fatalf("usage %+v", res)
	}
	if got := s.CumulativeUsage()["gpt-5.5"]; got != want {
		t.Fatalf("cumulative %+v", got)
	}
	if rec.text("agent_message_chunk") != "echo:hi" || rec.text("agent_thought_chunk") != "hmm" {
		t.Fatalf("frames %v", rec.snapshot())
	}
	call := rec.updates("tool_call")
	if len(call) != 1 || call[0]["title"] != "ask_question" || call[0]["rawInput"].(map[string]any)["q"] != float64(1) {
		t.Fatalf("tool_call %v", call)
	}
	done := rec.updates("tool_call_update")
	if len(done) != 1 || done[0]["status"] != "failed" || !strings.Contains(done[0]["content"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string), "answered") {
		t.Fatalf("tool_call_update %v", done)
	}
	if pd := rec.ofType("prompt_done"); len(pd) != 1 || pd[0]["stopReason"] != "end_turn" {
		t.Fatalf("prompt_done %v", pd)
	}
}

func TestResumeOpensTheSameAgent(t *testing.T) {
	s, _ := open(t, installFake(t), provider.OpenOptions{ResumeSessionID: "agent-old"})
	if s.SessionID() != "agent-old" || s.Info().ModelID != "default" {
		t.Fatalf("session %q model %q", s.SessionID(), s.Info().ModelID)
	}
}

func TestOpenRejectsAnUnofferedModel(t *testing.T) {
	c := installFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := New(c).Open(context.Background(), ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: "gpt-4"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "不提供模型") {
		t.Fatalf("err %v", err)
	}
}

func TestSteerReachesTheRunningTurn(t *testing.T) {
	s, rec := open(t, installFake(t), provider.OpenOptions{})
	if err := s.Steer("early"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("idle steer: %v", err)
	}
	type result struct {
		res provider.TurnResult
		err error
	}
	out := make(chan result, 1)
	go func() {
		res, err := s.Prompt(turnCtx(t), "hang", nil)
		out <- result{res, err}
	}()
	waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
	if err := s.Steer("please decline"); !errors.Is(err, provider.ErrSteerDeclined) {
		t.Fatalf("declined steer: %v", err)
	}
	if err := s.Steer("go on"); err != nil {
		t.Fatal(err)
	}
	r := <-out
	if r.err != nil || r.res.StopReason != "end_turn" || !strings.Contains(rec.text("agent_message_chunk"), "steered:go on") {
		t.Fatalf("res %+v err %v frames %v", r.res, r.err, rec.snapshot())
	}
	// The tool the run left open is closed when the turn ends.
	if u := rec.updates("tool_call_update"); len(u) != 1 || u[0]["toolCallId"] != "c1" || u[0]["status"] != "failed" {
		t.Fatalf("open tool %v", u)
	}
}

func TestCancelBeforeCursorSavedTheRunReplaysIt(t *testing.T) {
	s, rec := open(t, installFake(t), provider.OpenOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan error, 1)
	var res provider.TurnResult
	go func() {
		var err error
		res, err = s.Prompt(ctx, "hang", nil)
		out <- err
	}()
	waitFor(t, func() bool { return len(rec.updates("tool_call")) == 1 })
	cancel()
	if err := <-out; !errors.Is(err, context.Canceled) || res.StopReason != "cancelled" {
		t.Fatalf("res %+v err %v", res, err)
	}
	// Usage of the model call that completed before the cancel is kept.
	if u := res.Usage["default"]; u.InputTokens != 30 || u.CacheReadTokens != 20 || !res.UsagePartial {
		t.Fatalf("partial usage %+v", res)
	}
	if _, err := s.Prompt(turnCtx(t), "next", nil); err != nil {
		t.Fatal(err)
	}
	got := rec.text("agent_message_chunk")
	if !strings.Contains(got, "hang") || !strings.HasSuffix(got, "New message:\n\nnext") {
		t.Fatalf("replay %q", got)
	}
	// Only once.
	if _, err := s.Prompt(turnCtx(t), "third", nil); err != nil {
		t.Fatal(err)
	}
	if got := rec.text("agent_message_chunk"); !strings.HasSuffix(got, "echo:third") {
		t.Fatalf("third %q", got)
	}
}

func TestFailedRunEndsTheTurnFailed(t *testing.T) {
	s, rec := open(t, installFake(t), provider.OpenOptions{})
	res, err := s.Prompt(turnCtx(t), "fail", nil)
	if err == nil || res.StopReason != "failed" || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("res %+v err %v", res, err)
	}
	if e := rec.ofType("error_text"); len(e) != 1 {
		t.Fatalf("error_text %v", rec.snapshot())
	}
}

func TestCloseStopsTheBridge(t *testing.T) {
	s, _ := open(t, installFake(t), provider.OpenOptions{})
	_ = s.Close()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("bridge still running")
	}
}
