package cursorsdk

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestFakeBridge is a scripted bridge.mjs. The sent text selects the run:
// "hang" calls a tool and waits for a steer or a cancel (cancelled before
// Cursor saved the run), "fail" ends with an error, anything else echoes the
// text and finishes. A steer containing "decline" is handed back.
func TestFakeBridge(t *testing.T) {
	if os.Getenv("GRASP_FAKE_CURSOR_BRIDGE") == "" {
		return
	}
	f := &fakeBridge{}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var cmd map[string]any
		if json.Unmarshal(scanner.Bytes(), &cmd) == nil {
			f.handle(cmd)
		}
	}
	os.Exit(0)
}

type fakeBridge struct {
	out     sync.Mutex
	mu      sync.Mutex
	running bool
}

func (f *fakeBridge) emit(v map[string]any) {
	b, _ := json.Marshal(v)
	f.out.Lock()
	defer f.out.Unlock()
	_, _ = os.Stdout.Write(append(b, '\n'))
}

func (f *fakeBridge) reply(cmd map[string]any, data any) {
	f.emit(map[string]any{"type": "response", "id": cmd["id"], "ok": true, "data": data})
}

func (f *fakeBridge) delta(u map[string]any) { f.emit(map[string]any{"type": "delta", "update": u}) }

func (f *fakeBridge) end(v map[string]any) {
	f.mu.Lock()
	f.running = false
	f.mu.Unlock()
	v["type"], v["runId"] = "run_end", "run-1"
	f.emit(v)
}

var fakeUsage = map[string]any{"inputTokens": 100, "outputTokens": 10, "cacheReadTokens": 60, "cacheWriteTokens": 15}

func (f *fakeBridge) handle(cmd map[string]any) {
	switch cmd["type"] {
	case "models":
		raw, _ := os.ReadFile(os.Getenv("GRASP_FAKE_CURSOR_MODELS"))
		f.reply(cmd, json.RawMessage(raw))
	case "open":
		id, _ := cmd["agentId"].(string)
		model, _ := json.Marshal(cmd["model"])
		if id == "agent-gone" {
			f.reply(cmd, map[string]any{"agentId": "agent-new", "resumed": false, "resumeError": "Agent agent-gone was not found"})
		} else {
			f.reply(cmd, map[string]any{"agentId": map[bool]string{true: id, false: "agent-new"}[id != ""], "resumed": id != ""})
		}
		f.emit(map[string]any{"type": "connection", "state": "open " + string(model)})
	case "send":
		text, _ := cmd["text"].(string)
		f.mu.Lock()
		f.running = true
		f.mu.Unlock()
		f.reply(cmd, map[string]any{"runId": "run-1"})
		switch {
		case strings.HasSuffix(text, "hang"):
			f.delta(map[string]any{"type": "model-call-completed", "usage": map[string]any{"inputTokens": 50, "outputTokens": 5, "cacheReadTokens": 20}})
			f.delta(map[string]any{"type": "tool-call-started", "callId": "c1", "toolCall": map[string]any{"type": "shell", "args": map[string]any{"command": "sleep 60"}}})
		case strings.HasSuffix(text, "fail"):
			f.end(map[string]any{"status": "error", "error": "boom"})
		default:
			f.delta(map[string]any{"type": "thinking-delta", "text": "hmm"})
			f.delta(map[string]any{"type": "text-delta", "text": "echo:" + text})
			call := map[string]any{"type": "mcp", "args": map[string]any{"toolName": "ask_question", "args": map[string]any{"q": 1}}}
			f.delta(map[string]any{"type": "tool-call-started", "callId": "c2", "toolCall": call})
			call["result"] = map[string]any{"status": "success", "value": map[string]any{"content": []any{map[string]any{"text": map[string]any{"text": "answered"}}}, "isError": true}}
			f.delta(map[string]any{"type": "tool-call-completed", "callId": "c2", "toolCall": call})
			f.end(map[string]any{"status": "finished", "usage": fakeUsage})
		}
	case "steer":
		f.mu.Lock()
		running := f.running
		f.mu.Unlock()
		text, _ := cmd["text"].(string)
		switch {
		case !running:
			f.reply(cmd, map[string]any{"outcome": "no_turn"})
		case strings.Contains(text, "decline"):
			f.reply(cmd, map[string]any{"outcome": "revert_to_followup"})
		default:
			f.reply(cmd, map[string]any{"outcome": "complete_delivered"})
			f.delta(map[string]any{"type": "text-delta", "text": "steered:" + text})
			f.end(map[string]any{"status": "finished", "usage": fakeUsage})
		}
	case "cancel":
		f.reply(cmd, nil)
		f.mu.Lock()
		running := f.running
		f.mu.Unlock()
		if running {
			f.end(map[string]any{"status": "cancelled", "checkpointed": false})
		}
	case "close":
		f.reply(cmd, nil)
		os.Exit(0)
	}
}
