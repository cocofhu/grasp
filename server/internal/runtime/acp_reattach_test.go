package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// survivorBridge is a bridge that outlived the control plane: it holds the
// event log of turns sent before the restart and may still be running one.
type survivorBridge struct {
	srv *httptest.Server

	mu       sync.Mutex
	log      []json.RawMessage
	running  string        // opId still running when a client connects
	finishIn time.Duration // when running, prompt_done follows this long after connect
	onFinish func()
	chats    []string
	connects int
}

func (b *survivorBridge) start(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	handleTestBridgeLogin(mux)
	mux.HandleFunc("/ws", b.serveWS)
	b.srv = httptest.NewServer(mux)
	t.Cleanup(b.srv.Close)
}

func (b *survivorBridge) appendTurn(opID, narration string) {
	raw, _ := json.Marshal(map[string]any{"type": "prompt_begin", "opId": opID})
	b.log = append(b.log, raw)
	b.appendTurnEnd(opID, narration)
}

func (b *survivorBridge) appendTurnEnd(opID, narration string) {
	frames := []map[string]any{
		{"type": "session_update", "opId": opID, "update": map[string]any{
			"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": narration}}},
		{"type": "prompt_done", "opId": opID},
	}
	for _, f := range frames {
		raw, _ := json.Marshal(f)
		b.log = append(b.log, raw)
	}
}

func (b *survivorBridge) queueState() map[string]any {
	m := map[string]any{"op": "queue_state", "busy": b.running != "", "queue_length": 0, "running": nil}
	if b.running != "" {
		m["running"] = map[string]any{"opId": b.running}
	}
	return m
}

func (b *survivorBridge) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := fakeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	var wmu sync.Mutex
	write := func(v any) {
		wmu.Lock()
		defer wmu.Unlock()
		_ = conn.WriteJSON(v)
	}
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(msg, &m) != nil {
			continue
		}
		switch m["op"] {
		case "connect":
			b.mu.Lock()
			b.connects++
			logCopy := append([]json.RawMessage(nil), b.log...)
			qs := b.queueState()
			running, finishIn := b.running, b.finishIn
			b.mu.Unlock()
			write(map[string]any{"op": "connected", "sessionId": "kept-session", "eventLog": logCopy, "totalTurns": 1})
			write(qs)
			if running != "" {
				go func() {
					for {
						time.Sleep(100 * time.Millisecond)
						b.mu.Lock()
						still := b.running == running
						b.mu.Unlock()
						if !still {
							return
						}
						write(map[string]any{"op": "liveness", "opId": running, "data": map[string]any{"active": true}})
					}
				}()
			}
			if running != "" && finishIn > 0 {
				go func() {
					time.Sleep(finishIn)
					b.mu.Lock()
					b.appendTurnEnd(running, "finished after restart")
					b.running = ""
					fin := b.onFinish
					qs := b.queueState()
					b.mu.Unlock()
					if fin != nil {
						fin()
					}
					write(map[string]any{"op": "event", "opId": running, "data": map[string]any{"type": "prompt_done"}})
					write(qs)
				}()
			}
		case "cancel":
			b.mu.Lock()
			op := b.running
			b.running = ""
			qs := b.queueState()
			b.mu.Unlock()
			if op != "" {
				write(map[string]any{"op": "event", "opId": op, "data": map[string]any{"type": "prompt_done", "stopReason": "cancelled"}})
				write(qs)
			}
		case "chat":
			opID, _ := m["opId"].(string)
			content, _ := m["content"].(string)
			b.mu.Lock()
			b.chats = append(b.chats, content)
			b.appendTurn(opID, "fresh turn")
			b.mu.Unlock()
			write(withOpID(agentMessageFrame("fresh turn"), opID))
			write(withOpID(promptDoneFrame(), opID))
		}
	}
}

func (b *survivorBridge) chatCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.chats)
}

// reattachManager adds AttachNoEnsure to the fake manager: known names resolve
// to their surviving bridge, anything else is gone.
type reattachManager struct {
	*fakeManager
	survivors map[string]*survivorBridge
	attached  []string
}

func (m *reattachManager) AttachNoEnsure(_ context.Context, id string) (*sandbox.Sandbox, error) {
	m.mu.Lock()
	m.attached = append(m.attached, id)
	m.mu.Unlock()
	b := m.survivors[id]
	if b == nil {
		return nil, fmt.Errorf("sandbox %s not found", id)
	}
	addr := strings.TrimPrefix(b.srv.URL, "http://")
	var port int
	_, _ = fmt.Sscanf(addr[strings.LastIndex(addr, ":")+1:], "%d", &port)
	return &sandbox.Sandbox{Name: id, ID: id, Host: "127.0.0.1", Port: port, WorkspaceDir: "/root/workspace"}, nil
}

func setupReattach(t *testing.T, b *survivorBridge) (*acpProvider, *reattachManager, NodeReq) {
	t.Helper()
	restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte("deepseek/deepseek-chat\n"), nil
	})
	t.Cleanup(restore)
	p, _, _, fm, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{narration: "fresh sandbox", outcome: true} }
	})
	mgr := &reattachManager{fakeManager: fm, survivors: map[string]*survivorBridge{}}
	if b != nil {
		b.start(t)
		mgr.survivors["sb-old"] = b
	}
	p.mgr = mgr
	req.Config["produces"] = ""
	req.ReattachSandbox = "sb-old"
	return p, mgr, req
}

func markComplete(p *acpProvider, req NodeReq) {
	p.host.SetActiveNode(req.RunID, req.NodeID, req.Caps)
	_, _ = p.host.ServeRPC(req.RunID, req.Token, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success","summary":"done"}}}`))
}

func TestRunAgentReattachTurnAlreadyDone(t *testing.T) {
	b := &survivorBridge{}
	b.appendTurn("g-old", "work finished before restart")
	p, mgr, req := setupReattach(t, b)
	markComplete(p, req)

	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !strings.Contains(res.OutputMd, "work finished before restart") {
		t.Errorf("narration = %q, want the pre-restart turn", res.OutputMd)
	}
	if n := b.chatCount(); n != 0 {
		t.Errorf("chats = %d, want 0 (main prompt must not be resent)", n)
	}
	if mgr.createCount() != 0 {
		t.Errorf("created %d sandboxes, want 0", mgr.createCount())
	}
	if res.Outputs["content"] == nil {
		t.Error("outputs missing content")
	}
}

func TestRunAgentReattachWaitsForRunningTurn(t *testing.T) {
	b := &survivorBridge{running: "g-old", finishIn: 300 * time.Millisecond}
	p, mgr, req := setupReattach(t, b)
	b.onFinish = func() { markComplete(p, req) }
	raw, _ := json.Marshal(map[string]any{"type": "prompt_begin", "opId": "g-old"})
	b.log = []json.RawMessage{raw}

	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !strings.Contains(res.OutputMd, "finished after restart") {
		t.Errorf("narration = %q", res.OutputMd)
	}
	if n := b.chatCount(); n != 0 {
		t.Errorf("chats = %d, want 0", n)
	}
	if mgr.createCount() != 0 {
		t.Errorf("created %d sandboxes, want 0", mgr.createCount())
	}
}

func TestRunAgentReattachSendsPromptWhenNoneSent(t *testing.T) {
	b := &survivorBridge{}
	p, mgr, req := setupReattach(t, b)
	markComplete(p, req)

	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if n := b.chatCount(); n != 1 {
		t.Fatalf("chats = %d, want the main prompt once", n)
	}
	if !strings.Contains(b.chats[0], "do it") {
		t.Errorf("prompt = %q, want the node prompt", b.chats[0])
	}
	if !strings.Contains(res.OutputMd, "fresh turn") || mgr.createCount() != 0 {
		t.Errorf("narration=%q created=%d", res.OutputMd, mgr.createCount())
	}
}

func TestRunAgentReattachGoneFallsBackToFreshSandbox(t *testing.T) {
	p, mgr, req := setupReattach(t, nil)
	p.opts.SandboxRetryBackoff = time.Millisecond

	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if mgr.createCount() != 1 {
		t.Errorf("created %d sandboxes, want 1 fresh retry", mgr.createCount())
	}
	if len(mgr.attached) != 1 || mgr.attached[0] != "sb-old" {
		t.Errorf("attached = %v", mgr.attached)
	}
	if !strings.Contains(res.OutputMd, "fresh sandbox") {
		t.Errorf("narration = %q", res.OutputMd)
	}
}

func TestRunAgentReattachCancelStopsRunningTurn(t *testing.T) {
	b := &survivorBridge{running: "g-old"}
	p, _, req := setupReattach(t, b)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)

	_, err := p.RunAgent(ctx, req)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := b.chatCount(); n != 0 {
		t.Errorf("chats = %d, want 0", n)
	}
}

func TestAttachSandboxUnsupportedManager(t *testing.T) {
	p, _, _, _, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{} }
	})
	req.ReattachSandbox = "sb-old"
	if _, _, err := p.attachSandbox(context.Background(), req); !errors.Is(err, errSandboxSetup) {
		t.Fatalf("err = %v, want errSandboxSetup", err)
	}
}
