package acpx

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"
)

// TestACPXAgentHelperProcess is a minimal ACP agent that requires
// authenticate, advertises model and thought_level options and answers
// prompts with usage. "fail" replies with failure text on end_turn; "hang"
// waits for session/cancel and answers with the usage spent so far.
func TestACPXAgentHelperProcess(t *testing.T) {
	if os.Getenv("GRASP_ACPX_HELPER") == "" {
		return
	}
	out := bufio.NewWriter(os.Stdout)
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = out.Write(append(b, '\n'))
		_ = out.Flush()
	}
	model, effort := "m1", "low"
	options := func() []any {
		return []any{
			map[string]any{"id": "model", "currentValue": model, "options": []any{map[string]any{"value": "m1"}, map[string]any{"value": "m2"}}},
			map[string]any{"id": "effort", "category": "thought_level", "currentValue": effort, "groups": []any{map[string]any{"group": "g", "options": []any{map[string]any{"value": "low"}, map[string]any{"value": "high"}}}}},
		}
	}
	usage := map[string]any{"inputTokens": 30, "outputTokens": 4, "cachedReadTokens": 10, "totalTokens": 34}
	authed := ""
	var hanging int64
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				MethodID string `json:"methodId"`
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
				Prompt   []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		_ = json.Unmarshal(s.Bytes(), &req)
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
		switch req.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
		case "authenticate":
			authed = req.Params.MethodID
			reply(map[string]any{})
		case "session/new":
			if authed != "key" {
				send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "Authentication required"}})
				continue
			}
			reply(map[string]any{"sessionId": "s1", "configOptions": options()})
		case "session/set_config_option":
			if req.Params.ConfigID == "model" {
				model = req.Params.Value
			} else {
				effort = req.Params.Value
			}
			reply(map[string]any{"configOptions": options()})
		case "session/prompt":
			text := req.Params.Prompt[0].Text
			if text == "hang" {
				hanging = req.ID
				continue
			}
			if text == "fail" {
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s1",
					"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "\n⚠ boom: quota"}}}})
			}
			reply(map[string]any{"stopReason": "end_turn", "usage": usage, "_meta": map[string]any{"model": model, "effort": effort}})
		case "session/cancel":
			if hanging != 0 {
				send(map[string]any{"jsonrpc": "2.0", "id": hanging, "result": map[string]any{"stopReason": "cancelled", "usage": usage}})
				hanging = 0
			}
		}
	}
	os.Exit(0)
}

type helperRun struct {
	mu   sync.Mutex
	done []string
}

func openHelper(t *testing.T, spec Spec, opts provider.OpenOptions) (provider.Session, *helperRun, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRASP_ACPX_HELPER", "1")
	spec.AgentName = "helper"
	spec.Runtime = "helper-acp"
	spec.ConfigRoot = t.TempDir()
	spec.ArgvFn = func(string) []string { return []string{exe, "-test.run=^TestACPXAgentHelperProcess$"} }
	opts.Cwd = t.TempDir()
	run := &helperRun{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	s, err := New(spec).Open(context.Background(), ctx, opts, func(ev json.RawMessage) {
		if strings.Contains(string(ev), `"prompt_done"`) {
			run.mu.Lock()
			run.done = append(run.done, string(ev))
			run.mu.Unlock()
		}
	}, nil)
	if err == nil {
		t.Cleanup(func() { _ = s.Close() })
	}
	return s, run, err
}

var keyAuth = func([]string) string { return "key" }

func TestSessionReportsResultUsageAndSelectsConfig(t *testing.T) {
	spec := Spec{AuthMethodFn: keyAuth, ResultUsage: true, ConfigModel: true, FailureText: func(text string) bool { return strings.HasPrefix(strings.TrimSpace(text), "⚠") }}
	s, run, err := openHelper(t, spec, provider.OpenOptions{Model: "m2", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if !s.ReportsUsage() || s.Info().ModelID != "m2" {
		t.Fatalf("reports=%v model=%q", s.ReportsUsage(), s.Info().ModelID)
	}
	want := map[string]provider.TokenUsage{"m2": {InputTokens: 20, OutputTokens: 4, CacheReadTokens: 10}}
	res, err := s.Prompt(context.Background(), "hello", nil)
	if err != nil || res.StopReason != "end_turn" || res.UsagePartial || !reflect.DeepEqual(res.Usage, want) {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	res, err = s.Prompt(context.Background(), "fail", nil)
	if err != nil || res.StopReason != "failed" || !res.UsagePartial {
		t.Fatalf("failure text must fail the turn with partial usage: %+v err=%v", res, err)
	}
	res, err = s.Prompt(context.Background(), "hello", nil)
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("the next turn must not inherit the failure: %+v err=%v", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	res, err = s.Prompt(ctx, "hang", nil)
	if !errors.Is(err, context.Canceled) || !res.UsagePartial || !reflect.DeepEqual(res.Usage, want) {
		t.Fatalf("cancelled result=%+v err=%v", res, err)
	}
	// A stop the agent answers before ctx ends is still an unfinished turn.
	time.AfterFunc(50*time.Millisecond, func() { _ = s.Cancel() })
	res, err = s.Prompt(context.Background(), "hang", nil)
	if err != nil || res.StopReason != "cancelled" || !res.UsagePartial || !reflect.DeepEqual(res.Usage, want) {
		t.Fatalf("agent-cancelled result=%+v err=%v", res, err)
	}
	total := map[string]provider.TokenUsage{"m2": {InputTokens: 100, OutputTokens: 20, CacheReadTokens: 50}}
	if got := s.CumulativeUsage(); !reflect.DeepEqual(got, total) {
		t.Fatalf("cumulative=%+v want %+v", got, total)
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if len(run.done) != 4 || !strings.Contains(run.done[0], `"usage":{"m2"`) || strings.Contains(run.done[0], "usagePartial") ||
		!strings.Contains(run.done[1], `"stopReason":"failed"`) || !strings.Contains(run.done[1], `"usagePartial":true`) ||
		!strings.Contains(run.done[3], `"stopReason":"cancelled"`) || !strings.Contains(run.done[3], `"usagePartial":true`) {
		t.Fatalf("prompt_done frames=%q", run.done)
	}
}

func TestSessionWithoutResultUsageReportsNone(t *testing.T) {
	s, _, err := openHelper(t, Spec{AuthMethodFn: keyAuth}, provider.OpenOptions{Model: "m2"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ReportsUsage() || s.Info().ModelID != "m1" {
		t.Fatalf("reports=%v model=%q (model is only selected with ConfigModel)", s.ReportsUsage(), s.Info().ModelID)
	}
	res, err := s.Prompt(context.Background(), "hello", nil)
	if err != nil || res.Usage != nil || s.CumulativeUsage() != nil {
		t.Fatalf("result=%+v cumulative=%+v err=%v", res, s.CumulativeUsage(), err)
	}
}

func TestOpenRejectsConfigTheAgentDoesNotOffer(t *testing.T) {
	for name, opts := range map[string]provider.OpenOptions{
		"model":  {Model: "m9"},
		"effort": {ReasoningEffort: "max"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := openHelper(t, Spec{AuthMethodFn: keyAuth, ConfigModel: true}, opts); err == nil || !strings.Contains(err.Error(), "helper 不") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if _, _, err := openHelper(t, Spec{AuthMethodFn: func([]string) string { return "" }}, provider.OpenOptions{}); err == nil {
		t.Fatal("an agent that requires authenticate must refuse the session")
	}
}
