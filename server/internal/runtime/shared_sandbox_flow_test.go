package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// recordingRegistry captures register calls and serves a scripted durable
// binding for restart paths.
type recordingRegistry struct {
	countingRegistry
	infos        []RunSandboxInfo
	binding      *RunSandboxInfo
	replacements int
}

func (r *recordingRegistry) RegisterRunSandbox(info RunSandboxInfo) {
	r.countingRegistry.RegisterRunSandbox(info)
	r.mu.Lock()
	r.infos = append(r.infos, info)
	r.mu.Unlock()
}

func (r *recordingRegistry) FindRunSandbox(string) (RunSandboxInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.binding == nil {
		return RunSandboxInfo{}, false
	}
	return *r.binding, true
}

func (r *recordingRegistry) MarkRunSandboxReplacement(string, int, string) {
	r.mu.Lock()
	r.replacements++
	r.mu.Unlock()
}

func (r *recordingRegistry) FinalizeRunSandbox(string, string) {}

func (r *recordingRegistry) lastInfo(nodeID string) (RunSandboxInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.infos) - 1; i >= 0; i-- {
		if r.infos[i].NodeID == nodeID {
			return r.infos[i], true
		}
	}
	return RunSandboxInfo{}, false
}

func writeAgents(t *testing.T, agents map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for profile, agentJSON := range agents {
		dir := filepath.Join(root, profile)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type sharedFlow struct {
	host  *mcp.Host
	store *memStore
	mgr   *fakeManager
	coord *sharedSandboxCoordinator
	reg   *recordingRegistry
	opts  Options
	runID string
	token string
}

func newSharedFlow(t *testing.T, agents map[string]string, chatFor func(attempt int) chatFunc) *sharedFlow {
	t.Helper()
	store := newMemStore()
	host := mcp.NewHost(store)
	runID := "run-shared"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	mgr := newFakeManager(t, host, runID, "node-a", tok, chatFor)
	mgr.chatAPI = true
	opts := testOpts()
	opts.ProfilesRoot = writeAgents(t, agents)
	return &sharedFlow{host: host, store: store, mgr: mgr, coord: newSharedSandboxCoordinator(), reg: &recordingRegistry{}, opts: opts, runID: runID, token: tok}
}

func (f *sharedFlow) provider(t *testing.T, backend AcpBackend) *acpProvider {
	p, _ := newTestProviderBackend(t, f.host, f.opts, f.mgr, backend)
	p.coordinator = f.coord
	p.registry = f.reg
	return p
}

func (f *sharedFlow) req(nodeID, profile string) NodeReq {
	return NodeReq{RunID: f.runID, NodeID: nodeID, NodeType: "agent", Caps: testPlainCaps, Token: f.token,
		Config: map[string]any{"prompt": "do it", "produces": "report.md", "agent_profile": profile}, Vars: map[string]any{}}
}

func idleChat(int) chatFunc { return func(int) turnAction { return turnAction{} } }

// Nodes with equal container inputs share one container; each gets its own
// bridge chat carrying its own model.
func TestSharedFlowSameConfigReusesContainerWithPerNodeModel(t *testing.T) {
	f := newSharedFlow(t, map[string]string{
		"dev-a": `{"acpBackend":"cursor","env":{"ACP_BRIDGE_MODEL":"model-a"}}`,
		"dev-b": `{"acpBackend":"cursor","env":{"ACP_BRIDGE_MODEL":"model-b"}}`,
	}, idleChat)
	p := f.provider(t, BackendCursor)

	sbA, acpA, homeA, err := p.openSandbox(context.Background(), f.req("node-a", "dev-a"))
	if err != nil {
		t.Fatal(err)
	}
	p.retireRunSandbox(sbA, acpA, homeA)
	sbB, acpB, homeB, err := p.openSandbox(context.Background(), f.req("node-b", "dev-b"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.retireRunSandbox(sbB, acpB, homeB)

	if sbA != sbB || f.mgr.createCount() != 1 {
		t.Fatalf("createCount=%d, want one shared container", f.mgr.createCount())
	}
	models, deleted := f.mgr.bridge(0).chatStats()
	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-b" {
		t.Fatalf("chat models=%v, want per-node models", models)
	}
	if deleted != 1 {
		t.Fatalf("chats deleted=%d, want node A's chat removed", deleted)
	}
	info, _ := f.reg.lastInfo("node-b")
	if info.Scope != sandboxScopeRun || info.EnvFingerprint == "" {
		t.Fatalf("shared registration=%+v", info)
	}
}

// A node of another backend must not run inside a container whose image and
// AGENT_PROVIDER belong to the first node; it gets a private container at once
// even while the shared lease is held.
func TestSharedFlowBackendMismatchUsesPrivateSandbox(t *testing.T) {
	f := newSharedFlow(t, map[string]string{
		"cur": `{"acpBackend":"cursor"}`,
		"cc":  `{"acpBackend":"claude-code"}`,
	}, idleChat)
	pCursor := f.provider(t, BackendCursor)
	pClaude := f.provider(t, BackendClaudeCode)

	sbA, acpA, homeA, err := pCursor.openSandbox(context.Background(), f.req("node-a", "cur"))
	if err != nil {
		t.Fatal(err)
	}
	defer pCursor.retireRunSandbox(sbA, acpA, homeA)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sbB, acpB, homeB, err := pClaude.openSandbox(ctx, f.req("node-b", "cc"))
	if err != nil {
		t.Fatalf("mismatched node must not wait for the shared lease: %v", err)
	}
	if sbB == sbA || f.mgr.createCount() != 2 {
		t.Fatalf("createCount=%d, want a private container", f.mgr.createCount())
	}
	if info, _ := f.reg.lastInfo("node-b"); info.Scope != sandboxScopeNode {
		t.Fatalf("private registration scope=%q", info.Scope)
	}
	pClaude.retireRunSandbox(sbB, acpB, homeB)
	f.reg.mu.Lock()
	unregistered := f.reg.unregistered
	f.reg.mu.Unlock()
	if unregistered != 1 {
		t.Fatalf("private container must be torn down normally, unregistered=%d", unregistered)
	}
	if !f.coord.managed(f.runID) {
		t.Fatal("shared container must stay bound")
	}
}

// Same backend but a different Agent MCP config lands in a different
// ConfigHome, so it must not reuse the shared container either.
func TestSharedFlowAgentConfigMismatchUsesPrivateSandbox(t *testing.T) {
	f := newSharedFlow(t, map[string]string{
		"plain":    `{"acpBackend":"cursor"}`,
		"with-mcp": `{"acpBackend":"cursor","mcp":[{"name":"extra","url":"http://extra.example/mcp"}]}`,
	}, idleChat)
	p := f.provider(t, BackendCursor)
	sbA, acpA, homeA, err := p.openSandbox(context.Background(), f.req("node-a", "plain"))
	if err != nil {
		t.Fatal(err)
	}
	p.retireRunSandbox(sbA, acpA, homeA)
	sbB, acpB, homeB, err := p.openSandbox(context.Background(), f.req("node-b", "with-mcp"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.retireRunSandbox(sbB, acpB, homeB)
	if sbB == sbA || f.mgr.createCount() != 2 {
		t.Fatalf("createCount=%d, want private container for a different ConfigHome", f.mgr.createCount())
	}
}

// A node-level idle fault in the shared container retries in the same
// container instead of destroying the run's workspace.
func TestSharedFlowIdleRetryKeepsWorkspace(t *testing.T) {
	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{stall: true}
			}
			return turnAction{narration: "second try", produces: map[string]string{"report.md": "ok"}}
		}
	})
	p := f.provider(t, BackendCursor)
	p.opts.SandboxMaxAttempts = 2
	if _, err := p.RunAgent(context.Background(), f.req("node-a", "dev")); err != nil {
		t.Fatalf("idle fault should retry in the shared container: %v", err)
	}
	if f.mgr.createCount() != 1 {
		t.Fatalf("createCount=%d, want the shared container kept", f.mgr.createCount())
	}
	f.reg.mu.Lock()
	unregistered := f.reg.unregistered
	f.reg.mu.Unlock()
	if unregistered != 0 {
		t.Fatalf("healthy shared container destroyed, unregistered=%d", unregistered)
	}
}

// When the shared bridge no longer answers, the generation is replaced.
func TestSharedFlowUnhealthyFaultReplacesGeneration(t *testing.T) {
	prev := sharedSandboxHealthy
	sharedSandboxHealthy = func(*sandbox.Sandbox) bool { return false }
	t.Cleanup(func() { sharedSandboxHealthy = prev })

	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, func(attempt int) chatFunc {
		return func(int) turnAction {
			if attempt == 0 {
				return turnAction{stall: true}
			}
			return turnAction{narration: "fresh", produces: map[string]string{"report.md": "ok"}}
		}
	})
	p := f.provider(t, BackendCursor)
	p.opts.SandboxMaxAttempts = 2
	if _, err := p.RunAgent(context.Background(), f.req("node-a", "dev")); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if f.mgr.createCount() != 2 {
		t.Fatalf("createCount=%d, want replacement container", f.mgr.createCount())
	}
	info, _ := f.reg.lastInfo("node-a")
	if info.Generation != 2 || f.reg.replacements != 1 {
		t.Fatalf("generation=%d replacements=%d, want generation 2", info.Generation, f.reg.replacements)
	}
}

// A KeepAliveForReview node parks without holding the lease, so a later node
// (e.g. between the producer and its approval gate) is not blocked; retiring
// the parked session later must not free that node's lease.
func TestSharedFlowParkedReviewReleasesLease(t *testing.T) {
	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "done", produces: map[string]string{"report.md": "ok"}}
		}
	})
	p := f.provider(t, BackendCursor)
	reqA := f.req("node-a", "dev")
	reqA.KeepAliveForReview = true
	if _, err := p.RunAgent(context.Background(), reqA); err != nil {
		t.Fatal(err)
	}
	if !p.HasLiveSession(f.runID, "node-a") {
		t.Fatal("node A should be parked for review")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sbB, acpB, homeB, err := p.openSandbox(ctx, f.req("node-b", "dev"))
	if err != nil {
		t.Fatalf("node B blocked by parked review session: %v", err)
	}
	defer p.retireRunSandbox(sbB, acpB, homeB)

	p.RetireSession(f.runID, "node-a")
	ls, err := p.specLayered(f.req("node-c", "dev"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	short, cancelShort := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShort()
	if _, err := f.coord.acquire(short, f.runID, ls.fingerprint, mustNotCreate(t)); err == nil {
		t.Fatal("retiring the parked session freed node B's lease")
	}
	if f.mgr.createCount() != 1 {
		t.Fatalf("createCount=%d, want one shared container", f.mgr.createCount())
	}
}

// Every Codex node remembers its login so a refreshed auth.json is written
// back, not only the node that created the shared container.
func TestSharedFlowCodexReuseRemembersLogin(t *testing.T) {
	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"codex"}`}, idleChat)
	ws := filepath.Join(f.opts.ProfilesRoot, "dev", "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f.opts.ProjectIDForWorkflow = func(string) string { return "proj-1" }
	f.opts.ProjectCredentialsForProject = func(string) map[string]string {
		return map[string]string{envauth.EnvCodexAuthFile: chatgptLogin}
	}
	p := f.provider(t, BackendCodex)

	reqA, reqB := f.req("node-a", "dev"), f.req("node-b", "dev")
	sbA, acpA, homeA, err := p.openSandbox(context.Background(), reqA)
	if err != nil {
		t.Fatal(err)
	}
	p.forgetCodexLogin(codexLoginKey(reqA))
	p.retireRunSandbox(sbA, acpA, homeA)
	sbB, acpB, homeB, err := p.openSandbox(context.Background(), reqB)
	if err != nil {
		t.Fatal(err)
	}
	defer p.retireRunSandbox(sbB, acpB, homeB)
	if sbA != sbB {
		t.Fatal("codex nodes with the same login credential should share")
	}
	if _, ok := p.takeCodexLogin(codexLoginKey(reqB)); !ok {
		t.Fatal("reusing node must remember its Codex login for write-back")
	}
}

// After a restart the coordinator has no entry for the run; AbortRun must
// still tear down the durable binding's container and ConfigHome.
func TestAbortRunDestroysDetachedBindingAfterRestart(t *testing.T) {
	f := newSharedFlow(t, map[string]string{"dev": `{"acpBackend":"cursor"}`}, idleChat)
	p := f.provider(t, BackendCursor)
	home := t.TempDir()
	f.reg.binding = &RunSandboxInfo{Name: "sb-from-previous-process", RunID: f.runID, HomeDir: home, Scope: sandboxScopeRun}

	p.AbortRun(f.runID)

	f.reg.mu.Lock()
	unregistered := f.reg.unregistered
	f.reg.mu.Unlock()
	if unregistered != 1 {
		t.Fatalf("detached binding not torn down, unregistered=%d", unregistered)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("detached ConfigHome should be removed, stat err=%v", err)
	}
}
