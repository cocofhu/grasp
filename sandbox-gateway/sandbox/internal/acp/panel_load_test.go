package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"
)

// TestPanelAgentHelperProcess is a minimal ACP agent. It knows one stored
// session, "stored", whose history it replays on session/load. Modes pick the
// advertised reopen capability: "load", "resume" or "none". A prompt "hang"
// runs until session/cancel, which the agent obeys unless
// GRASP_ACP_PANEL_CANCEL is "ignore".
func TestPanelAgentHelperProcess(t *testing.T) {
	mode := os.Getenv("GRASP_ACP_PANEL_HELPER")
	if mode == "" {
		return
	}
	out := bufio.NewWriter(os.Stdout)
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = out.Write(append(b, '\n'))
		_ = out.Flush()
	}
	update := func(sid, text string) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
			"sessionId": sid,
			"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}},
		}})
	}
	s := bufio.NewScanner(os.Stdin)
	sid := ""
	var hanging int64
	for s.Scan() {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		_ = json.Unmarshal(s.Bytes(), &req)
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
		switch req.Method {
		case "initialize":
			caps := map[string]any{"loadSession": mode == "load"}
			if mode == "resume" {
				caps["sessionCapabilities"] = map[string]any{"resume": map[string]any{}}
			}
			reply(map[string]any{"protocolVersion": 1, "agentCapabilities": caps})
		case "session/load", "session/resume":
			if req.Params.SessionID != "stored" || (req.Method == "session/load") != (mode == "load") {
				send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32002, "message": "session not found"}})
				continue
			}
			if req.Method == "session/load" {
				update("stored", "replayed history")
			}
			sid = "stored"
			reply(map[string]any{"configOptions": []any{map[string]any{
				"id": "model", "currentValue": "m2", "options": []any{map[string]any{"value": "m2", "name": "Model 2"}},
			}}})
		case "session/new":
			sid = "fresh"
			reply(map[string]any{"sessionId": sid})
		case "session/prompt":
			if len(req.Params.Prompt) > 0 && req.Params.Prompt[0].Text == "hang" {
				hanging = req.ID
				continue
			}
			update(sid, "live reply")
			reply(map[string]any{"stopReason": "end_turn"})
		case "session/cancel":
			if hanging != 0 && os.Getenv("GRASP_ACP_PANEL_CANCEL") != "ignore" {
				send(map[string]any{"jsonrpc": "2.0", "id": hanging, "result": map[string]any{"stopReason": "cancelled"}})
				hanging = 0
			}
		}
	}
	os.Exit(0)
}

type panelRun struct {
	panel  *Panel
	mu     sync.Mutex
	events []string
}

func (r *panelRun) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func openHelperPanel(t *testing.T, mode, resume string, env ...string) *panelRun {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	run := &panelRun{}
	p, err := NewPanel(context.Background(), ctx, PanelSpec{
		Argv: []string{exe, "-test.run=^TestPanelAgentHelperProcess$"},
		Env:  append(append(os.Environ(), "GRASP_ACP_PANEL_HELPER="+mode), env...),
		Cwd:  t.TempDir(),
		OnEvent: func(ev json.RawMessage) {
			var frame struct {
				Type   string `json:"type"`
				Update struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"update"`
			}
			if json.Unmarshal(ev, &frame) != nil || frame.Type != "session_update" {
				return
			}
			run.mu.Lock()
			run.events = append(run.events, frame.Update.Content.Text)
			run.mu.Unlock()
		},
		ResumeSessionID: resume,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(); _ = p.Conn().Wait() })
	run.panel = p
	if _, err := p.Prompt(ctx, "hi", nil); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestPanelReopensTheStoredSessionWithoutReplayingIt(t *testing.T) {
	for _, mode := range []string{"load", "resume"} {
		t.Run(mode, func(t *testing.T) {
			run := openHelperPanel(t, mode, "stored")
			if run.panel.SessionID != "stored" || !run.panel.CanResume() || run.panel.ModelID != "m2" {
				t.Fatalf("session=%q canResume=%v model=%q", run.panel.SessionID, run.panel.CanResume(), run.panel.ModelID)
			}
			if got := run.texts(); fmt.Sprint(got) != "[live reply]" {
				t.Fatalf("events = %q, want only the live reply", got)
			}
		})
	}
}

func TestPanelStartsFreshWhenTheSessionCannotBeLoaded(t *testing.T) {
	cases := map[string]struct{ mode, resume string }{
		"unknown session":   {"load", "gone"},
		"no loadSession":    {"noload", "stored"},
		"nothing to resume": {"load", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			run := openHelperPanel(t, tc.mode, tc.resume)
			if run.panel.SessionID != "fresh" {
				t.Fatalf("session = %q, want a new one", run.panel.SessionID)
			}
			if got := run.texts(); fmt.Sprint(got) != "[live reply]" {
				t.Fatalf("events = %q", got)
			}
		})
	}
}

func TestCancelledPromptWaitsForTheAgentToStop(t *testing.T) {
	run := openHelperPanel(t, "none", "")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := run.panel.Prompt(ctx, "hang", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, err := run.panel.Prompt(context.Background(), "next", nil); err != nil {
		t.Fatalf("the agent that stopped must keep serving: %v", err)
	}
}

func TestAgentThatIgnoresTheInterruptIsClosed(t *testing.T) {
	old := provider.InterruptGrace
	provider.InterruptGrace = 100 * time.Millisecond
	t.Cleanup(func() { provider.InterruptGrace = old })
	run := openHelperPanel(t, "none", "", "GRASP_ACP_PANEL_CANCEL=ignore")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := run.panel.Prompt(ctx, "hang", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	select {
	case <-run.panel.Conn().Done():
	case <-time.After(3 * time.Second):
		t.Fatal("an agent that ignored the interrupt was left running")
	}
}
