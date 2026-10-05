package runtime

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// TestRunAgentEventSinkAndRetry covers streamChat's live-emit branch (emit != nil
// -> ChatStreamResult) and emitRetryNotice's emit branch, by pairing a live sink
// with one transient failure before success.
func TestRunAgentEventSinkAndRetry(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	mgr := newFakeManager(t, host, "r", "n", tok, func(attempt int) chatFunc {
		return func(int) turnAction {
			if attempt == 0 {
				return turnAction{dropConn: true}
			}
			return turnAction{narration: "ok", produces: map[string]string{"report.md": "x"}}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	var emitted int
	p.SetEventSink(func(string, string, []models.AcpEvent, bool) { emitted++ })
	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testPlainCaps, Token: tok,
		Config: map[string]any{"prompt": "go", "produces": "report.md"}, Vars: map[string]any{}})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if emitted == 0 {
		t.Error("expected the live event sink to be invoked")
	}
}

// TestEnsureStructuredReprompt: a framework (research) node whose bridge writes
// the structured product only on the re-prompt turn exercises ensureStructured's
// retry loop.
func TestEnsureStructuredReprompt(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	mgr := newFakeManager(t, host, "r", "n", tok, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "researching"} // no structured product yet
			}
			return turnAction{narration: "wrote it", produces: map[string]string{
				mcp.ResearchArtifactName: `{"summary":"s","findings":[{"id":"r1","title":"t"}]}`}}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testCapsWriting(models.SchemaResearch), Token: tok,
		Config: map[string]any{"prompt": "research"}, Vars: map[string]any{}})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if _, ok := store.Get("r", mcp.ResearchArtifactName); !ok {
		t.Error("structured product not written via re-prompt")
	}
}

// TestEnsureStructuredIgnoresUpstreamOwner: a same-named artifact written by
// another node must not satisfy ensureStructured — the agent is re-prompted
// until this node rewrites it (aligns with engine.finalizeProducts).
func TestEnsureStructuredIgnoresUpstreamOwner(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	if _, err := host.WriteArtifact("r", tok, "upstream", mcp.ResearchArtifactName,
		`{"summary":"old","findings":[]}`, "json"); err != nil {
		t.Fatal(err)
	}
	mgr := newFakeManager(t, host, "r", "n", tok, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "see upstream leftover"}
			}
			return turnAction{narration: "rewrote", produces: map[string]string{
				mcp.ResearchArtifactName: `{"summary":"new","findings":[{"id":"r1","title":"t"}]}`,
			}}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testCapsWriting(models.SchemaResearch), Token: tok,
		Config: map[string]any{"prompt": "research"}, Vars: map[string]any{}})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	infos, err := host.ListArtifacts("r", tok)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name == mcp.ResearchArtifactName && info.Node != "n" {
			t.Fatalf("expected rewrite owned by n, got owner %q", info.Node)
		}
	}
}

func implementProvider(t *testing.T, planJSON string, chat chatFunc) (*acpProvider, *memStore, string, NodeReq) {
	restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte(""), nil
	})
	t.Cleanup(restore)
	store := newMemStore()
	host := mcp.NewHost(store)
	runID, nodeID := "impl", "n"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	if planJSON != "" {
		if _, err := host.WriteArtifact(runID, tok, nodeID, mcp.PlanArtifactName, planJSON, "json"); err != nil {
			t.Fatal(err)
		}
	}
	mgr := newFakeManager(t, host, runID, nodeID, tok, func(int) chatFunc { return chat })
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: runID, NodeID: nodeID, NodeType: "agent", Caps: testImplementCaps, Token: tok,
		Config: map[string]any{"prompt": "build", "max_rounds": 2}, Vars: map[string]any{}})
	return p, store, tok, req
}

func TestEnsurePlanCompleteHappy(t *testing.T) {
	plan := `{"goals":[{"id":"g1","title":"G","status":"done"}]}`
	p, store, _, req := implementProvider(t, plan, func(int) turnAction {
		return turnAction{narration: "done", produces: map[string]string{
			mcp.ImplementationResultArtifactName: `{"summary":"done"}`}}
	})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if _, ok := store.Get("impl", mcp.ImplementationResultArtifactName); !ok {
		t.Error("implementation_result missing")
	}
}

func TestEnsurePlanCompleteBlocksIncomplete(t *testing.T) {
	plan := `{"goals":[{"id":"g1","title":"G","status":"pending"}]}`
	p, _, _, req := implementProvider(t, plan, func(int) turnAction {
		// Never marks the plan done, so after the re-prompt rounds the node fails.
		return turnAction{narration: "still working"}
	})
	_, err := p.RunAgent(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "计划未全部完成") {
		t.Fatalf("expected plan-incomplete failure, got %v", err)
	}
}

// TestEnsurePlanCompleteNudgeTimeout returns a timeout error (not a contract
// error) when a plan nudge re-prompt hits the per-turn chat deadline.
func TestEnsurePlanCompleteNudgeTimeout(t *testing.T) {
	plan := `{"goals":[{"id":"g1","title":"G","status":"pending"}]}`
	turn := 0
	p, _, _, req := implementProvider(t, plan, func(int) turnAction {
		turn++
		if turn == 1 {
			return turnAction{narration: "started"}
		}
		return turnAction{stall: true}
	})
	p.opts.ChatTimeout = time.Second
	p.opts.ChatIdleTimeout = 5 * time.Second
	p.opts.ChatTimeout = time.Second
	_, err := p.RunAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected nudge timeout failure")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected timeout semantics, got %v", err)
	}
	if strings.Contains(err.Error(), "计划未全部完成") {
		t.Fatalf("nudge timeout must not be reported as contract failure: %v", err)
	}
}

// TestEnsureStructuredNudgeTimeout returns a timeout error (not a contract
// error) when a structured re-prompt hits the per-turn chat deadline.
func TestEnsureStructuredNudgeTimeout(t *testing.T) {
	turn := 0
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	mgr := newFakeManager(t, host, "r", "n", tok, func(int) chatFunc {
		return func(int) turnAction {
			turn++
			if turn == 1 {
				return turnAction{narration: "researching"}
			}
			return turnAction{stall: true}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testCapsWriting(models.SchemaResearch), Token: tok,
		Config: map[string]any{"prompt": "research"}, Vars: map[string]any{}})
	p.opts.ChatIdleTimeout = 5 * time.Second
	p.opts.ChatTimeout = time.Second
	_, err := p.RunAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected structured nudge timeout failure")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected timeout semantics, got %v", err)
	}
	if strings.Contains(err.Error(), "结构化") || strings.Contains(err.Error(), "research.json") {
		t.Fatalf("nudge timeout must not be reported as contract failure: %v", err)
	}
}

// TestEnsureOutcomeNudgeTimeout returns a timeout error when node_complete
// re-prompt hits the per-turn chat deadline.
func TestEnsureOutcomeNudgeTimeout(t *testing.T) {
	turn := 0
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	mgr := newFakeManager(t, host, "r", "n", tok, func(int) chatFunc {
		return func(int) turnAction {
			turn++
			if turn == 1 {
				return turnAction{narration: "done", produces: map[string]string{"report.md": "x"}}
			}
			return turnAction{stall: true}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testPlainCaps, Token: tok,
		Config: map[string]any{"prompt": "go", "produces": "report.md"},
		Vars:   map[string]any{}})
	p.opts.ChatIdleTimeout = 5 * time.Second
	p.opts.ChatTimeout = time.Second
	_, err := p.RunAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected node_complete nudge timeout failure")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected timeout semantics, got %v", err)
	}
}

// TestEnsureStructuredRepromptTransportError propagates Cursor API / ACP
// transport faults from the structured-product nudge instead of fail-closing
// as a silent contract miss (so the engine can auto-retry the node).
func TestEnsureStructuredRepromptTransportError(t *testing.T) {
	turn := 0
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	mgr := newFakeManager(t, host, "r", "n", tok, func(int) chatFunc {
		return func(int) turnAction {
			turn++
			if turn == 1 {
				return turnAction{narration: "researching"}
			}
			return turnAction{sendError: "Failed to reach the Cursor API. If you are behind a corporate proxy, set the HTTPS_PROXY environment variable."}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testCapsWriting(models.SchemaResearch), Token: tok,
		Config: map[string]any{"prompt": "research"}, Vars: map[string]any{}})
	_, err := p.RunAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected transport error from structured re-prompt")
	}
	if !strings.Contains(err.Error(), "Failed to reach the Cursor API") {
		t.Fatalf("want Cursor API transport error, got %v", err)
	}
	if isRetryableSandboxErr(err) != true {
		t.Fatalf("Cursor API fault must classify as retryable sandbox err, got %v", err)
	}
}

// TestRunAgentChatFailureSurfacesError: an error-only first turn fails the
// node with the provider message instead of a zero-value success.
func TestRunAgentChatFailureSurfacesError(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	mgr := newFakeManager(t, host, "r", "n", tok, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{sendError: "model refused"}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testPlainCaps, Token: tok,
		Config: map[string]any{"prompt": "go"}, Vars: map[string]any{}})
	_, err := p.RunAgent(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "agent chat") || !strings.Contains(err.Error(), "model refused") {
		t.Fatalf("expected agent chat failure with provider text, got %v", err)
	}
}
