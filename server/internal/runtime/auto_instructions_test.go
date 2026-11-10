package runtime

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

type boundaryInstructionFixture struct {
	pending    []*AutoInstruction
	late       *AutoInstruction
	states     map[string]string
	sealed     bool
	restarts   int
	ackFailure bool
}

func (q *boundaryInstructionFixture) Next(_ context.Context, seal bool) (*AutoInstruction, error) {
	if seal && q.late != nil {
		q.pending = append(q.pending, q.late)
		q.late = nil
	}
	if len(q.pending) > 0 {
		item := q.pending[0]
		q.pending = q.pending[1:]
		return item, nil
	}
	q.sealed = q.sealed || seal
	return nil, nil
}
func (q *boundaryInstructionFixture) Complete(_ context.Context, id, state, _ string) error {
	if q.ackFailure {
		return errors.New("ack storage unavailable")
	}
	q.states[id] = state
	return nil
}
func (q *boundaryInstructionFixture) RestartAttempt(context.Context) error { q.restarts++; return nil }

func TestAutoInstructionsStayInNativeSessionAndRevalidateOutcome(t *testing.T) {
	restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) { return nil, nil })
	defer restore()
	store := newMemStore()
	host := mcp.NewHost(store)
	runID, nodeID := "instruction-native", "implementation"
	token := host.RegisterRun(runID)
	// The shared scripted bridge marks outcomes through its clarify tool
	// surface. Open that fixture surface; the request itself is an auto Agent.
	host.SetOutcomeAllowed(runID, true)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	queue := &boundaryInstructionFixture{pending: []*AutoInstruction{{ID: "first", Text: "first supplement"}}, late: &AutoInstruction{ID: "late", Text: "late supplement"}, states: map[string]string{}}
	mgr := newFakeManager(t, host, runID, nodeID, token, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 1 || turn == 3 {
				if host.HasOutcome(runID, nodeID) {
					t.Error("an earlier node_complete was reused for a later human instruction")
				}
			}
			// First supplement deliberately omits node_complete. Validation must
			// prompt again before the late instruction can be sealed.
			return turnAction{narration: "native reply", produces: map[string]string{mcp.ImplementationResultArtifactName: `{"summary":"done"}`}, outcome: turn != 1}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: runID, NodeID: nodeID, NodeType: "agent", Caps: testImplementCaps, Token: token, Instructions: queue, Config: map[string]any{"prompt": "initial task"}})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if mgr.createCount() != 1 || !queue.sealed || queue.states["first"] != "applied" || queue.states["late"] != "applied" {
		t.Fatalf("native session or receipts lost: guests=%d sealed=%v states=%v", mgr.createCount(), queue.sealed, queue.states)
	}
	mgr.mu.Lock()
	bridge := mgr.bridges[0]
	mgr.mu.Unlock()
	bridge.mu.Lock()
	prompts := append([]string(nil), bridge.prompts...)
	bridge.mu.Unlock()
	if len(prompts) != 4 || !strings.Contains(prompts[1], "first supplement") || !strings.Contains(prompts[3], "late supplement") {
		t.Fatalf("supplement order or contract revalidation lost: %q", prompts)
	}
}

func TestAutoInstructionUncertainDispatchDoesNotReplay(t *testing.T) {
	for _, failure := range []string{"native-disconnect", "ack-storage"} {
		t.Run(failure, func(t *testing.T) {
			restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) { return nil, nil })
			defer restore()
			store := newMemStore()
			host := mcp.NewHost(store)
			runID, nodeID := "instruction-uncertain", "implementation"
			token := host.RegisterRun(runID)
			host.SetOutcomeAllowed(runID, true)
			t.Cleanup(func() { host.UnregisterRun(runID) })
			queue := &boundaryInstructionFixture{pending: []*AutoInstruction{{ID: "uncertain", Text: "may have external effects"}}, states: map[string]string{}, ackFailure: failure == "ack-storage"}
			mgr := newFakeManager(t, host, runID, nodeID, token, func(int) chatFunc {
				return func(turn int) turnAction {
					return turnAction{narration: "native reply", produces: map[string]string{mcp.ImplementationResultArtifactName: `{"summary":"done"}`}, outcome: true, dropConn: turn == 1 && failure == "native-disconnect"}
				}
			})
			p, _ := newTestProvider(t, host, testOpts(), mgr)
			req := reqWithProfile(NodeReq{RunID: runID, NodeID: nodeID, NodeType: "agent", Caps: testImplementCaps, Token: token, Instructions: queue, Config: map[string]any{"prompt": "initial task"}})
			_, err := p.RunAgent(context.Background(), req)
			if !errors.Is(err, ErrInstructionDeliveryUncertain) || mgr.createCount() != 1 || queue.restarts != 0 {
				t.Fatalf("uncertain instruction was retried: guests=%d restarts=%d err=%v", mgr.createCount(), queue.restarts, err)
			}
			if failure == "native-disconnect" && queue.states["uncertain"] != "unknown" {
				t.Fatalf("lost dispatch was falsely acknowledged: %v", queue.states)
			}
		})
	}
}
