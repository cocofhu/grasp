package runtime

import (
	"context"
	"errors"
	"github.com/cocofhu/grasp/internal/sandbox"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAgentDurableDispatchLossRetainsOriginalAttemptWithoutNativeReplay(t *testing.T) {
	p, _, _, manager, _, req := setupProvider(t, func(int) chatFunc { return func(int) turnAction { return turnAction{dropConn: true} } })
	var attempts, dispatches atomic.Int32
	req.PrepareProviderAttempt = func(context.Context) (sandbox.OperationIdentity, error) {
		return sandbox.OperationIdentity{ID: "g-test", Attempt: int(attempts.Add(1))}, nil
	}
	req.BeforeProviderDispatch = func(_ context.Context, operation sandbox.OperationIdentity) error {
		attempt := operation.Attempt
		if attempt != 1 {
			t.Fatal("incorrect durable provider attempt", attempt)
		}
		dispatches.Add(1)
		return nil
	}
	_, err := p.RunAgent(context.Background(), req)
	if !errors.Is(err, ErrProviderDispatchUnknown) {
		t.Fatal("lost native response treated as retryable completion", err)
	}
	if attempts.Load() != 1 || dispatches.Load() != 1 || manager.createCount() != 1 {
		t.Fatal("dispatched native operation replayed", attempts.Load(), dispatches.Load(), manager.createCount())
	}
}

func TestRunAgentDurableAttemptRefusalPrecedesAnyNativeSetup(t *testing.T) {
	p, _, _, manager, _, req := setupProvider(t, func(int) chatFunc { return func(int) turnAction { return turnAction{} } })
	req.PrepareProviderAttempt = func(context.Context) (sandbox.OperationIdentity, error) {
		return sandbox.OperationIdentity{}, ErrProviderDispatchUnknown
	}
	_, err := p.RunAgent(context.Background(), req)
	if !errors.Is(err, ErrProviderDispatchUnknown) || manager.createCount() != 0 {
		t.Fatal("refused native attempt was executed", err, manager.createCount())
	}
	req.PrepareProviderAttempt = func(context.Context) (sandbox.OperationIdentity, error) {
		return sandbox.OperationIdentity{ID: "g-test", Attempt: 1}, nil
	}
	req.ReattachSandbox = "retained-resource"
	_, err = p.RunAgent(context.Background(), req)
	if !errors.Is(err, ErrProviderDispatchUnknown) || manager.createCount() != 0 {
		t.Fatal("unknown reattach resent prompt", err, manager.createCount())
	}
}

func TestRunAgentWritesExactlyThePersistedNativeOperationAfterReceiptCommit(t *testing.T) {
	var committed, earlyWrite atomic.Bool
	p, _, _, manager, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction {
			if !committed.Load() {
				earlyWrite.Store(true)
			}
			return turnAction{narration: "exact original", produces: map[string]string{"report.md": "proof"}}
		}
	})
	req.PrepareProviderAttempt = func(context.Context) (sandbox.OperationIdentity, error) {
		return sandbox.OperationIdentity{ID: "g-persisted-exactly-once", Attempt: 1}, nil
	}
	p.opts.NativeAuthorization = func(context.Context, NodeReq) (string, error) { return "current-grant", nil }
	req.BeforeProviderDispatch = func(_ context.Context, op sandbox.OperationIdentity) error {
		if !op.Valid() || op.ID != "g-persisted-exactly-once" || op.SessionID != "native-stable-session" || op.Workspace != "/root/workspace" {
			t.Fatal("dispatch receipt was not native-acknowledged", op)
		}
		committed.Store(true)
		return nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := p.RunAgent(ctx, req); err != nil {
		t.Fatal(err)
	}
	if earlyWrite.Load() {
		t.Fatal("native prompt arrived before receipt commit")
	}
	bridge := manager.bridge(0)
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if len(bridge.opIDs) == 0 || bridge.opIDs[0] != "g-persisted-exactly-once" {
		t.Fatal("first native prompt used a different operation", bridge.opIDs)
	}
	// Follow-up contract nudges are independent authorized prompts. They must
	// never reuse the durable identity of the original main prompt.
	seen := map[string]bool{}
	for _, id := range bridge.opIDs {
		if seen[id] {
			t.Fatal("native operation was replayed", bridge.opIDs)
		}
		seen[id] = true
	}
}
