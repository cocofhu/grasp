package pi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFakePiProcess is a scripted `pi --mode rpc`. The prompt text selects
// the run: "hang" waits in a tool call for a steer or an abort, "fail" ends
// with a model error, "dialog" asks an extension dialog first, "strand"
// leaves a steer queued after the run ends; reloading the session drops the
// queue. "retry" fails its first request and recovers on Pi's automatic
// retry, "retry giveup" gives up during the backoff. Anything else gets a
// reply. A steer containing "slow" is answered late, and refused when it
// also contains "refuse". After a run ends, get_state may keep reporting streaming
// (GRASP_FAKE_PI_LATE_STREAMING reads) or fail (GRASP_FAKE_PI_STATE_FAILS),
// and the run may stay busy a while past its agent_end
// (GRASP_FAKE_PI_LINGER_MS).
func TestFakePiProcess(t *testing.T) {
	if os.Getenv("GRASP_FAKE_PI") == "" {
		return
	}
	session := ""
	for i, arg := range os.Args {
		if arg == "--session" && i+1 < len(os.Args) {
			session = os.Args[i+1]
		}
	}
	f := &fakePi{session: session, steer: make(chan string, 8), dialog: make(chan bool, 1)}
	f.late, _ = strconv.Atoi(os.Getenv("GRASP_FAKE_PI_LATE_STREAMING"))
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var cmd map[string]any
		if json.Unmarshal(scanner.Bytes(), &cmd) != nil {
			continue
		}
		f.handle(cmd)
	}
	os.Exit(0)
}

type fakePi struct {
	session string
	out     sync.Mutex

	mu        sync.Mutex
	streaming bool
	queued    int
	ended     bool
	late      int
	abort     chan struct{}
	idle      chan struct{}
	steer     chan string
	dialog    chan bool
}

func (f *fakePi) write(v any) {
	line, _ := json.Marshal(v)
	f.out.Lock()
	defer f.out.Unlock()
	fmt.Fprintf(os.Stdout, "%s\n", line)
}

func (f *fakePi) respond(cmd map[string]any, success bool, data any, errText string) {
	r := map[string]any{"type": "response", "id": cmd["id"], "command": cmd["type"], "success": success}
	if data != nil {
		r["data"] = data
	}
	if errText != "" {
		r["error"] = errText
	}
	f.write(r)
}

func (f *fakePi) handle(cmd map[string]any) {
	switch cmd["type"] {
	case "get_state":
		f.mu.Lock()
		state := map[string]any{"sessionFile": f.session, "isStreaming": f.streaming, "pendingMessageCount": f.queued,
			"model": map[string]any{"id": "fake-model", "provider": "fake"}}
		fail := f.ended && os.Getenv("GRASP_FAKE_PI_STATE_FAILS") != ""
		if f.ended && f.late > 0 {
			f.late--
			state["isStreaming"] = true
		}
		f.mu.Unlock()
		if fail {
			f.respond(cmd, false, nil, "state unavailable")
			return
		}
		f.respond(cmd, true, state, "")
	case "prompt":
		text, _ := cmd["message"].(string)
		f.mu.Lock()
		streaming := f.streaming
		if !streaming {
			f.streaming, f.abort, f.idle = true, make(chan struct{}), make(chan struct{})
		}
		f.mu.Unlock()
		switch {
		case streaming && cmd["streamingBehavior"] == nil:
			f.respond(cmd, false, nil, "Agent is already processing.")
		case strings.Contains(text, "slow"):
			go func() {
				time.Sleep(150 * time.Millisecond)
				if strings.Contains(text, "refuse") {
					f.respond(cmd, false, nil, "input rejected")
					return
				}
				f.respond(cmd, true, map[string]any{"disposition": "queued"}, "")
				f.steer <- text
			}()
		case streaming:
			f.respond(cmd, true, nil, "")
			f.steer <- text
		default:
			f.respond(cmd, true, nil, "")
			go f.run(text)
		}
	case "abort":
		if os.Getenv("GRASP_FAKE_PI_IGNORE_ABORT") != "" {
			return
		}
		f.mu.Lock()
		abort, idle := f.abort, f.idle
		if f.streaming && abort != nil {
			close(abort)
			f.abort = nil
		}
		f.mu.Unlock()
		go func() {
			if idle != nil {
				<-idle
			}
			f.respond(cmd, true, nil, "")
		}()
	case "switch_session":
		f.mu.Lock()
		f.queued = 0
		f.mu.Unlock()
		f.respond(cmd, true, map[string]any{"cancelled": false}, "")
	case "clear_queue":
		for drained := false; !drained; {
			select {
			case <-f.steer:
			default:
				drained = true
			}
		}
		f.respond(cmd, true, nil, "")
	case "extension_ui_response":
		f.dialog <- cmd["cancelled"] == true
	}
}

func (f *fakePi) user(text string) {
	if path := os.Getenv("GRASP_FAKE_PI_USERS"); path != "" {
		fh, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		_, _ = fmt.Fprintf(fh, "%q\n", text)
		_ = fh.Close()
	}
	m := map[string]any{"role": "user", "content": text}
	f.write(map[string]any{"type": "message_start", "message": m})
	f.write(map[string]any{"type": "message_end", "message": m})
}

func (f *fakePi) assistant(stop, errText string) {
	m := map[string]any{"role": "assistant", "model": "fake-model", "stopReason": stop,
		"usage": map[string]int{"input": 10, "output": 5, "cacheRead": 4, "cacheWrite": 1}}
	if errText != "" {
		m["errorMessage"] = errText
	}
	f.write(map[string]any{"type": "message_end", "message": m})
	f.write(map[string]any{"type": "turn_end", "message": m})
}

func (f *fakePi) delta(kind, text string) {
	f.write(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": kind, "delta": text}})
}

func (f *fakePi) run(text string) {
	f.write(map[string]any{"type": "agent_start"})
	f.user(text)
	agentEnd := true
	defer func() {
		if agentEnd {
			f.write(map[string]any{"type": "agent_end", "messages": []any{}})
		}
		linger, _ := strconv.Atoi(os.Getenv("GRASP_FAKE_PI_LINGER_MS"))
		time.Sleep(time.Duration(linger) * time.Millisecond)
		f.mu.Lock()
		f.streaming, f.ended = false, true
		idle := f.idle
		f.mu.Unlock()
		close(idle)
	}()
	f.mu.Lock()
	abort := f.abort
	f.mu.Unlock()
	switch {
	case strings.Contains(text, "hang"):
		f.write(map[string]any{"type": "tool_execution_start", "toolCallId": "call-1", "toolName": "bash", "args": map[string]string{"command": "sleep 30"}})
		select {
		case <-abort:
			f.write(map[string]any{"type": "tool_execution_end", "toolCallId": "call-1", "toolName": "bash", "isError": true,
				"result": map[string]any{"content": []map[string]string{{"type": "text", "text": "Command aborted"}}}})
			f.assistant("aborted", "Request was aborted.")
			return
		case steer := <-f.steer:
			f.write(map[string]any{"type": "tool_execution_end", "toolCallId": "call-1", "toolName": "bash",
				"result": map[string]any{"content": []map[string]string{{"type": "text", "text": "done"}}}})
			f.assistant("toolUse", "")
			f.user(steer)
		}
	case strings.Contains(text, "retry"):
		f.assistant("error", "503 Service Unavailable")
		f.write(map[string]any{"type": "agent_end", "messages": []any{}, "willRetry": true})
		f.write(map[string]any{"type": "auto_retry_start", "attempt": 1, "maxAttempts": 3, "delayMs": 50, "errorMessage": "503 Service Unavailable"})
		time.Sleep(50 * time.Millisecond)
		if strings.Contains(text, "giveup") {
			f.write(map[string]any{"type": "auto_retry_end", "success": false, "attempt": 1, "finalError": "Retry cancelled"})
			agentEnd = false
			return
		}
		f.write(map[string]any{"type": "agent_start"})
		f.delta("text_delta", "recovered")
		f.assistant("stop", "")
		f.write(map[string]any{"type": "auto_retry_end", "success": true, "attempt": 1})
		return
	case strings.Contains(text, "fail"):
		f.assistant("error", "upstream model failed")
		return
	case strings.Contains(text, "dialog"):
		f.write(map[string]any{"type": "extension_ui_request", "id": "ui-1", "method": "confirm", "title": "Allow?"})
		if !<-f.dialog {
			f.assistant("error", "dialog was not dismissed")
			return
		}
	case strings.Contains(text, "strand"):
		f.write(map[string]any{"type": "tool_execution_start", "toolCallId": "call-1", "toolName": "bash"})
		<-f.steer
		f.mu.Lock()
		f.queued = 1
		f.mu.Unlock()
	}
	f.delta("thinking_delta", "thinking it over")
	for _, d := range []string{"hello<|tool", "_call>x from ", "pi"} {
		f.delta("text_delta", d)
	}
	f.assistant("stop", "")
}
