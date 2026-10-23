package runtime

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// reprimingManager is a fakeManager that also re-primes (counts the calls).
type reprimingManager struct {
	*fakeManager
	mu       sync.Mutex
	reprimes []sandbox.Spec
	prevKeys [][]string
	failNext bool
}

func (m *reprimingManager) Reprime(_ context.Context, _ *sandbox.Sandbox, spec sandbox.Spec, prev []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failNext {
		m.failNext = false
		return io.ErrUnexpectedEOF
	}
	m.reprimes = append(m.reprimes, spec)
	m.prevKeys = append(m.prevKeys, prev)
	return nil
}

func (m *reprimingManager) reprimeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reprimes)
}

type sharedHarness struct {
	p     *acpProvider
	mgr   *reprimingManager
	reg   *countingRegistry
	runID string
	tok   string
}

func newSharedHarness(t *testing.T, chat func(attempt int) chatFunc) *sharedHarness {
	t.Helper()
	restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return nil, nil
	})
	t.Cleanup(restore)
	host := mcp.NewHost(newMemStore())
	runID := "shared-run"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	if chat == nil {
		chat = func(int) chatFunc { return func(int) turnAction { return turnAction{narration: "ok"} } }
	}
	mgr := &reprimingManager{fakeManager: newFakeManager(t, host, runID, "n1", tok, chat)}
	p, reg := newTestProvider(t, host, testOpts(), mgr)
	p.shared = newSharedSandboxes()
	return &sharedHarness{p: p, mgr: mgr, reg: reg, runID: runID, tok: tok}
}

func (h *sharedHarness) req(node, mode string) NodeReq {
	return reqWithProfile(NodeReq{RunID: h.runID, NodeID: node, NodeType: "agent", Token: h.tok,
		Config: map[string]any{"prompt": "go"}, Vars: map[string]any{}, SandboxMode: mode})
}

func (h *sharedHarness) run(t *testing.T, req NodeReq) {
	t.Helper()
	if _, err := h.p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent %s: %v", req.NodeID, err)
	}
}

func TestSharedSandboxReusedAcrossNodes(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1", models.SandboxModeShared))
	h.run(t, h.req("n2", models.SandboxModeShared))
	h.run(t, h.req("n3", models.SandboxModeShared))
	if got := h.mgr.createCount(); got != 1 {
		t.Fatalf("containers created = %d, want 1", got)
	}
	// Every other node differs at least in GRASP_NODE_ID.
	if got := h.mgr.reprimeCount(); got != 2 {
		t.Fatalf("re-primes = %d, want 2", got)
	}
	if env := h.mgr.reprimes[1].Env; env["GRASP_NODE_ID"] != "n3" {
		t.Fatalf("second re-prime node = %q", env["GRASP_NODE_ID"])
	}
	if len(h.mgr.prevKeys[0]) == 0 {
		t.Fatal("re-prime got no previous env keys")
	}
	ss := h.p.shared.get(h.runID)
	if ss == nil || ss.owner != "" {
		t.Fatalf("shared sandbox after nodes: %+v", ss)
	}
	if h.reg.unregistered != 0 {
		t.Fatalf("shared sandbox deregistered mid-run: %d", h.reg.unregistered)
	}
}

func TestSharedSandboxSameNodeSkipsReprime(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1", models.SandboxModeShared))
	h.run(t, h.req("n1", models.SandboxModeShared))
	if h.mgr.createCount() != 1 || h.mgr.reprimeCount() != 0 {
		t.Fatalf("creates=%d reprimes=%d, want 1/0", h.mgr.createCount(), h.mgr.reprimeCount())
	}
}

func TestPerNodeModeUnchanged(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1", ""))
	h.run(t, h.req("n2", models.SandboxModePerNode))
	if h.mgr.createCount() != 2 || h.mgr.reprimeCount() != 0 {
		t.Fatalf("creates=%d reprimes=%d, want 2/0", h.mgr.createCount(), h.mgr.reprimeCount())
	}
	if h.p.shared.get(h.runID) != nil {
		t.Fatal("per-node run recorded a shared sandbox")
	}
	// Without a retirer the per-node path destroys and deregisters as before.
	if !h.reg.balanced() {
		t.Fatalf("registry unbalanced: %+v", h.reg)
	}
}

func TestSharedSandboxHeldByParkedSessionFallsBack(t *testing.T) {
	h := newSharedHarness(t, nil)
	first := h.req("n1", models.SandboxModeShared)
	first.KeepAliveForReview = true
	h.run(t, first)
	h.run(t, h.req("gate", models.SandboxModeShared))
	if h.mgr.createCount() != 2 {
		t.Fatalf("creates = %d, want 2 (parked holder forces own sandbox)", h.mgr.createCount())
	}
	if h.mgr.reprimeCount() != 0 {
		t.Fatal("held shared sandbox was re-primed")
	}
	// Closing the parked session frees the shared sandbox for the next node.
	h.p.RetireSession(h.runID, "n1")
	h.run(t, h.req("n3", models.SandboxModeShared))
	if h.mgr.createCount() != 2 || h.mgr.reprimeCount() != 1 {
		t.Fatalf("creates=%d reprimes=%d, want 2/1", h.mgr.createCount(), h.mgr.reprimeCount())
	}
}

func TestSharedSandboxRetryKeepsContainer(t *testing.T) {
	h := newSharedHarness(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{dropConn: true}
			}
			return turnAction{narration: "ok"}
		}
	})
	h.p.opts.SandboxMaxAttempts = 3
	h.run(t, h.req("n1", models.SandboxModeShared))
	if got := h.mgr.createCount(); got != 1 {
		t.Fatalf("containers created = %d, want 1 (retry reuses the shared sandbox)", got)
	}
}

func TestSharedSandboxReprimeFailureReplacesContainer(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1", models.SandboxModeShared))
	h.mgr.failNext = true
	h.run(t, h.req("n2", models.SandboxModeShared))
	if got := h.mgr.createCount(); got != 2 {
		t.Fatalf("containers created = %d, want 2", got)
	}
	ss := h.p.shared.get(h.runID)
	if ss == nil || ss.sb.Name != "fake-sb-1" {
		t.Fatalf("shared sandbox not replaced: %+v", ss)
	}
}

func TestSharedSandboxRetiredAtRunEnd(t *testing.T) {
	h := newSharedHarness(t, nil)
	reg := &retiringRegistry{}
	h.p.registry = reg
	h.run(t, h.req("n1", models.SandboxModeShared))
	h.run(t, h.req("n2", models.SandboxModeShared))
	if reg.retired != 0 {
		t.Fatalf("retired mid-run: %d", reg.retired)
	}
	h.p.AbortRun(h.runID)
	if reg.retired != 1 {
		t.Fatalf("retired = %d, want 1", reg.retired)
	}
	if h.p.shared.get(h.runID) != nil {
		t.Fatal("shared sandbox still tracked after run end")
	}
	// Another provider's AbortRun after the retire must not destroy it.
	if !h.p.shared.release("fake-sb-0") {
		t.Fatal("retired shared sandbox not recognised")
	}
	h.p.AbortRun(h.runID)
	if reg.retired != 1 || reg.unregistered != 0 {
		t.Fatalf("second AbortRun: retired=%d unregistered=%d", reg.retired, reg.unregistered)
	}
}

func TestSharedSandboxIncompatibleWorkspaceFallsBack(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1", models.SandboxModeShared))
	h.p.shared.get(h.runID).sb.WorkspaceDir = "/elsewhere"
	h.run(t, h.req("n2", models.SandboxModeShared))
	if h.mgr.createCount() != 2 || h.mgr.reprimeCount() != 0 {
		t.Fatalf("creates=%d reprimes=%d, want 2/0", h.mgr.createCount(), h.mgr.reprimeCount())
	}
}

func TestAdoptSharedSandbox(t *testing.T) {
	h := newSharedHarness(t, nil)
	sb := &sandbox.Sandbox{Name: "adopted"}
	h.p.adoptSharedSandbox(h.req("n1", ""), sb)
	if h.p.shared.get(h.runID) != nil {
		t.Fatal("per-node reattach adopted as shared")
	}
	h.p.adoptSharedSandbox(h.req("n1", models.SandboxModeShared), sb)
	ss := h.p.shared.get(h.runID)
	if ss == nil || ss.fingerprint != "" || ss.owner == "" {
		t.Fatalf("adopted: %+v", ss)
	}
	h.p.adoptSharedSandbox(h.req("n2", models.SandboxModeShared), &sandbox.Sandbox{Name: "other"})
	if h.p.shared.get(h.runID).sb.Name != "adopted" {
		t.Fatal("second adopt replaced the shared sandbox")
	}
}

func TestReleaseSharedSandboxNilSafe(t *testing.T) {
	p := &acpProvider{}
	if p.releaseSharedSandbox(&sandbox.Sandbox{Name: "x"}, nil) {
		t.Fatal("provider without shared store released a sandbox")
	}
	p.retireSharedRunSandbox("r")
	s := newSharedSandboxes()
	if s.release("") || s.take("r", nil) != nil {
		t.Fatal("empty store")
	}
}
