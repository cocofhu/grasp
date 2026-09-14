package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/gorilla/websocket"
)

// turnAction scripts what a fake ACP bridge does for a single chat turn. Zero
// value = emit an empty prompt_done (a no-op successful turn).
type turnAction struct {
	narration string            // agent_message_chunk text, streamed before prompt_done
	produces  map[string]string // artifact name -> content, written via the in-process MCP host
	questions []models.ReactQuestion
	outcome   bool   // call node_complete via ServeRPC during the turn
	dropConn  bool   // close the WS mid-turn without prompt_done -> ErrConnClosed
	stall     bool   // send nothing and never prompt_done -> ErrChatIdle (with a small idle timeout)
	sendError string // emit {op:error} -> a non-retryable agent error
	// oneshot-style provider failure: error_text frame then prompt_done{stopReason:failed}.
	// Distinct from sendError, which uses the top-level {op:error} envelope the
	// legacy fake path exercised (and which ChatStructured already turns into a Go error).
	errorText string
	failed    bool
}

// chatFunc returns the action for the turn-th chat on a given sandbox (0-based).
type chatFunc func(turn int) turnAction

// fakeBridge is an in-process cursor-acp WebSocket bridge: it speaks just enough
// of the protocol (connect/chat/cancel) for a real *sandbox.ACPClient to drive
// it, with scripted per-turn behavior. It serves only /ws; /api/* 404s, which
// the provider tolerates (snapshot falls back to the streamed aggregation).
type fakeBridge struct {
	srv    *httptest.Server
	host   *mcp.Host
	runID  string
	nodeID string
	token  string
	chat   chatFunc

	mu      sync.Mutex
	turns   int
	prompts []string // every chat prompt received (for rehydrate-prime assertions)
	images  []int    // image attachment count per chat turn
}

var fakeUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(*http.Request) bool { return true },
}

func startFakeBridge(t *testing.T, host *mcp.Host, runID, nodeID, token string, chat chatFunc) *fakeBridge {
	t.Helper()
	b := &fakeBridge{host: host, runID: runID, nodeID: nodeID, token: token, chat: chat}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", b.serveWS)
	b.srv = httptest.NewServer(mux)
	t.Cleanup(b.srv.Close)
	return b
}

func (b *fakeBridge) hostPort() (string, int) {
	addr := b.srv.Listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func (b *fakeBridge) promptAt(i int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i < 0 || i >= len(b.prompts) {
		return ""
	}
	return b.prompts[i]
}

func (b *fakeBridge) imageCountAt(i int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i < 0 || i >= len(b.images) {
		return -1
	}
	return b.images[i]
}

func (b *fakeBridge) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := fakeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(msg, &m) != nil {
			continue
		}
		switch fmt.Sprint(m["op"]) {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "fake-session"})
		case "cancel":
			// fire-and-forget, no ack
		case "chat":
			b.mu.Lock()
			turn := b.turns
			b.turns++
			content, _ := m["content"].(string)
			nImg := 0
			if imgs, ok := m["images"].([]any); ok {
				nImg = len(imgs)
			}
			b.prompts = append(b.prompts, content)
			b.images = append(b.images, nImg)
			b.mu.Unlock()
			if !b.applyTurn(conn, b.chat(turn)) {
				return // dropConn: leave the read loop so the socket closes
			}
		}
	}
}

// applyTurn plays one scripted action; returns false when the connection must
// be dropped (simulating a mid-turn sandbox/ACP crash).
func (b *fakeBridge) applyTurn(conn *websocket.Conn, act turnAction) bool {
	if act.dropConn {
		return false
	}
	if act.sendError != "" {
		if act.narration != "" {
			_ = conn.WriteJSON(agentMessageFrame(act.narration))
		}
		_ = conn.WriteJSON(map[string]any{"op": "error", "message": act.sendError})
		return true
	}
	if act.stall {
		return true // no frames; the client's idle watchdog trips
	}
	for name, content := range act.produces {
		_, _ = b.host.WriteArtifact(b.runID, b.token, b.nodeID, name, content, kindForName(name))
	}
	if act.outcome {
		b.host.SetActiveNode(b.runID, b.nodeID, "approve")
		_, _ = b.host.ServeRPC(b.runID, b.token, []byte(
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success","summary":"turn outcome"}}}`))
	}
	if len(act.questions) > 0 {
		b.host.SetPendingQuestions(b.runID, b.nodeID, act.questions)
	}
	if act.narration != "" {
		_ = conn.WriteJSON(agentMessageFrame(act.narration))
	}
	if act.errorText != "" {
		_ = conn.WriteJSON(map[string]any{"op": "event", "data": map[string]any{
			"type": "error_text", "text": act.errorText,
		}})
	}
	if act.failed {
		_ = conn.WriteJSON(map[string]any{"op": "event", "data": map[string]any{
			"type": "prompt_done", "stopReason": "failed",
		}})
		return true
	}
	_ = conn.WriteJSON(promptDoneFrame())
	return true
}

func agentMessageFrame(text string) map[string]any {
	return map[string]any{"op": "event", "data": map[string]any{
		"type": "session_update",
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"text": text},
		},
	}}
}

func promptDoneFrame() map[string]any {
	return map[string]any{"op": "event", "data": map[string]any{"type": "prompt_done"}}
}

func kindForName(name string) string {
	switch {
	case strings.HasSuffix(name, ".json"):
		return "json"
	case strings.HasSuffix(name, ".yaml"), strings.HasSuffix(name, ".yml"):
		return "yaml"
	default:
		return "markdown"
	}
}

// fakeManager is the sandboxManager seam: every Create starts a fresh fake
// bridge whose per-turn behavior is chosen by chatFor(attempt). createErr, when
// set and non-nil for an attempt, fails the create instead (openSandbox wraps it
// as a retryable errSandboxSetup). It records bridges for attempt-count and
// rehydrate-prompt assertions.
type fakeManager struct {
	t         *testing.T
	host      *mcp.Host
	runID     string
	nodeID    string
	token     string
	chatFor   func(attempt int) chatFunc
	createErr func(attempt int) error

	mu      sync.Mutex
	bridges []*fakeBridge
}

func newFakeManager(t *testing.T, host *mcp.Host, runID, nodeID, token string, chatFor func(attempt int) chatFunc) *fakeManager {
	return &fakeManager{t: t, host: host, runID: runID, nodeID: nodeID, token: token, chatFor: chatFor}
}

func (m *fakeManager) Create(ctx context.Context, spec sandbox.Spec) (*sandbox.Sandbox, error) {
	m.mu.Lock()
	attempt := len(m.bridges)
	if m.createErr != nil {
		if err := m.createErr(attempt); err != nil {
			m.bridges = append(m.bridges, nil) // count the attempt even on failure
			m.mu.Unlock()
			return nil, err
		}
	}
	m.mu.Unlock()

	b := startFakeBridge(m.t, m.host, m.runID, m.nodeID, m.token, m.chatFor(attempt))
	m.mu.Lock()
	m.bridges = append(m.bridges, b)
	m.mu.Unlock()

	host, port := b.hostPort()
	return &sandbox.Sandbox{Name: fmt.Sprintf("fake-sb-%d", attempt), Host: host, Port: port, WorkspaceDir: "/root/workspace"}, nil
}

func (m *fakeManager) createCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.bridges)
}

func (m *fakeManager) bridge(i int) *fakeBridge {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.bridges) {
		return nil
	}
	return m.bridges[i]
}

// countingRegistry is a SandboxRegistry (no retirer) that counts register vs
// unregister so tests can assert no sandbox is leaked across retries.
type countingRegistry struct {
	mu           sync.Mutex
	registered   int
	unregistered int
}

func (r *countingRegistry) RegisterRunSandbox(RunSandboxInfo) {
	r.mu.Lock()
	r.registered++
	r.mu.Unlock()
}

func (r *countingRegistry) UnregisterRunSandbox(string) {
	r.mu.Lock()
	r.unregistered++
	r.mu.Unlock()
}

func (r *countingRegistry) balanced() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registered == r.unregistered
}

// retiringRegistry additionally implements RunSandboxRetirer so retireRunSandbox
// hands the container to the store instead of destroying it inline.
type retiringRegistry struct {
	countingRegistry
	retired int
}

func (r *retiringRegistry) RetireRunSandbox(string) {
	r.mu.Lock()
	r.retired++
	r.mu.Unlock()
}

// newTestProvider builds a real acpProvider with a fake manager injected.
func newTestProvider(t *testing.T, host *mcp.Host, opts Options, mgr sandboxManager) (*acpProvider, *countingRegistry) {
	return newTestProviderBackend(t, host, opts, mgr, BackendCursor)
}

const testAgentProfile = "test-agent"

func ensureTestProfiles(t *testing.T, opts Options) Options {
	if opts.ProfilesRoot != "" {
		return opts
	}
	opts.ProfilesRoot = writeAgent(t, testAgentProfile, `{"env":{"GRASP_CURSOR_API_KEY":"fake","GRASP_CLAUDE_API_KEY":"fake","GRASP_CODEBUDDY_API_KEY":"fake","GRASP_TRAE_API_KEY":"fake"}}`)
	return opts
}

func reqWithProfile(req NodeReq) NodeReq {
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	if str2(req.Config["agent_profile"]) == "" {
		req.Config["agent_profile"] = testAgentProfile
	}
	return req
}

func newTestProviderBackend(t *testing.T, host *mcp.Host, opts Options, mgr sandboxManager, backend AcpBackend) (*acpProvider, *countingRegistry) {
	opts = ensureTestProfiles(t, opts)
	p := newBaseACPProvider(host, opts, backend).(*acpProvider)
	p.mgr = mgr
	reg := &countingRegistry{}
	p.registry = reg
	return p, reg
}
