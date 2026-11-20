package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFakeAppServer is the app-server the tests talk to: it runs only as a
// helper process (GRASP_FAKE_CODEX=1) and scripts each turn by a keyword in
// the prompt text.
func TestFakeAppServer(t *testing.T) {
	if os.Getenv("GRASP_FAKE_CODEX") != "1" {
		return
	}
	if args := os.Getenv("GRASP_FAKE_CODEX_ARGS"); args != "" {
		_ = os.WriteFile(args, []byte(strings.Join(os.Args, "\n")), 0o600)
	}
	runFakeAppServer()
	os.Exit(0)
}

type fakeServer struct {
	mu     sync.Mutex
	out    *bufio.Writer
	thread string
	turn   string
	mode   string
	n      int
}

func (f *fakeServer) send(v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(v)
	_, _ = f.out.Write(append(b, '\n'))
	_ = f.out.Flush()
}

func (f *fakeServer) notify(method string, params map[string]any) {
	if _, ok := params["threadId"]; !ok {
		params["threadId"] = f.thread
	}
	f.send(map[string]any{"method": method, "params": params})
}

func (f *fakeServer) item(method string, item map[string]any) {
	f.notify(method, map[string]any{"turnId": f.turn, "item": item})
}

func (f *fakeServer) usage(totalIn, totalOut, lastIn, lastOut, cached int64) {
	f.notify("thread/tokenUsage/updated", map[string]any{"turnId": f.turn, "tokenUsage": map[string]any{
		"total": map[string]any{"inputTokens": totalIn, "outputTokens": totalOut, "cachedInputTokens": cached},
		"last":  map[string]any{"inputTokens": lastIn, "outputTokens": lastOut, "cachedInputTokens": cached},
	}})
}

func (f *fakeServer) complete(status string, errText string) {
	turn := map[string]any{"id": f.turn, "status": status}
	if errText != "" {
		turn["error"] = map[string]any{"message": errText}
	}
	f.notify("turn/completed", map[string]any{"turn": turn})
	f.turn, f.mode = "", ""
}

func runFakeAppServer() {
	f := &fakeServer{out: bufio.NewWriter(os.Stdout), thread: "thread-new"}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ThreadID string `json:"threadId"`
				Input    []struct {
					Text string `json:"text"`
				} `json:"input"`
			} `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(in.Bytes(), &msg) != nil {
			continue
		}
		reply := func(result any) { f.send(map[string]any{"id": msg.ID, "result": result}) }
		text := ""
		if len(msg.Params.Input) > 0 {
			text = msg.Params.Input[0].Text
		}
		switch msg.Method {
		case "initialize":
			reply(map[string]any{})
		case "initialized":
		case "thread/start":
			reply(map[string]any{"thread": map[string]any{"id": f.thread}, "model": "fake-model"})
		case "thread/resume":
			f.thread = msg.Params.ThreadID
			reply(map[string]any{"thread": map[string]any{"id": f.thread}, "model": "fake-model"})
		case "turn/start":
			f.n++
			f.turn = fmt.Sprintf("turn-%d", f.n)
			reply(map[string]any{"turn": map[string]any{"id": f.turn, "status": "inProgress"}})
			f.notify("turn/started", map[string]any{"turn": map[string]any{"id": f.turn}})
			f.play(text)
		case "turn/steer":
			if f.turn == "" {
				// Late, like a rejection racing the client's own turn/completed.
				time.Sleep(100 * time.Millisecond)
				f.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "message": "no active turn"}})
				continue
			}
			reply(map[string]any{"turnId": f.turn})
			f.item("item/completed", map[string]any{"type": "agentMessage", "id": "steered", "text": "steered: " + text})
			f.complete("completed", "")
		case "turn/interrupt":
			reply(map[string]any{})
			if f.mode != "hang" {
				f.complete("interrupted", "")
			}
		default:
			if len(msg.Result) > 0 {
				// A response to our server request ("ask" mode).
				f.item("item/completed", map[string]any{"type": "agentMessage", "id": "answer", "text": "approval: " + string(msg.Result)})
				f.complete("completed", "")
				continue
			}
			reply(map[string]any{})
		}
	}
}

func (f *fakeServer) play(text string) {
	switch {
	case strings.Contains(text, "hello"):
		f.notify("warning", map[string]any{"message": "Model metadata for `fake-model` not found."})
		f.notify("error", map[string]any{"turnId": f.turn, "willRetry": true, "error": map[string]any{"message": "Reconnecting... 1/5"}})
		f.item("item/started", map[string]any{"type": "agentMessage", "id": "m1", "text": ""})
		f.notify("item/agentMessage/delta", map[string]any{"turnId": f.turn, "itemId": "m1", "delta": "Hel"})
		f.notify("item/agentMessage/delta", map[string]any{"turnId": f.turn, "itemId": "m1", "delta": "lo"})
		f.item("item/completed", map[string]any{"type": "agentMessage", "id": "m1", "text": "Hello"})
		f.item("item/completed", map[string]any{"type": "reasoning", "id": "r1", "summary": []string{"thinking"}, "content": []string{}})
		f.item("item/started", map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "status": "inProgress"})
		f.item("item/completed", map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "status": "completed", "aggregatedOutput": "a.txt\n"})
		f.item("item/started", map[string]any{"type": "mcpToolCall", "id": "t1", "server": "store", "tool": "get", "arguments": map[string]any{"k": 1}, "status": "inProgress"})
		f.item("item/completed", map[string]any{"type": "mcpToolCall", "id": "t1", "server": "store", "tool": "get", "status": "failed",
			"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "tool says no"}}}})
		f.item("item/completed", map[string]any{"type": "fileChange", "id": "f1", "status": "completed", "changes": []any{map[string]any{"path": "a.txt", "kind": "add"}}})
		f.item("item/completed", map[string]any{"type": "webSearch", "id": "w1", "query": "grasp"})
		f.notify("turn/plan/updated", map[string]any{"turnId": f.turn, "plan": []any{
			map[string]any{"step": "read", "status": "completed"}, map[string]any{"step": "write", "status": "inProgress"}}})
		f.notify("item/agentMessage/delta", map[string]any{"turnId": "stale-turn", "itemId": "x", "delta": "stale"})
		f.usage(1100, 40, 100, 40, 0)
		f.usage(1200, 60, 100, 20, 30)
		f.complete("completed", "")
	case strings.Contains(text, "fail"):
		f.notify("error", map[string]any{"turnId": f.turn, "willRetry": false, "error": map[string]any{"message": "provider exploded"}})
		f.complete("failed", "")
	case strings.Contains(text, "auth"):
		f.complete("failed", "401 Unauthorized: https://api.openai.com/v1/responses")
	case strings.Contains(text, "ask"):
		f.send(map[string]any{"id": 99, "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": f.thread, "turnId": f.turn}})
	case strings.Contains(text, "elicit"):
		f.send(map[string]any{"id": 98, "method": "mcpServer/elicitation/request", "params": map[string]any{}})
	case strings.Contains(text, "legacy"):
		f.send(map[string]any{"id": 97, "method": "execCommandApproval", "params": map[string]any{}})
	case strings.Contains(text, "unknown-request"):
		f.send(map[string]any{"id": 96, "method": "item/tool/requestUserInput", "params": map[string]any{}})
		f.complete("completed", "")
	case strings.Contains(text, "die"):
		os.Exit(3)
	case strings.Contains(text, "hang"):
		f.mode = "hang"
	default: // "wait": stays open until steered or interrupted
		f.mode = "wait"
	}
}

// installFake puts a `codex` wrapper for the fake on PATH. It returns the file
// the fake writes its argv to.
func installFake(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexec \"" + os.Args[0] + "\" -test.run=^TestFakeAppServer$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "argv")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GRASP_FAKE_CODEX", "1")
	t.Setenv("GRASP_FAKE_CODEX_ARGS", argsFile)
	t.Setenv("GRASP_MODEL_CONNECTION", "")
	t.Setenv(envReasoningEffort, "")
	t.Setenv(envServiceTier, "")
	return argsFile
}
