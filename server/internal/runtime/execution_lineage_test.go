package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/mcp"
)

func TestExecutionAuthorityDenialPrecedesSandboxCreateAndNativePrompt(t *testing.T) {
	denied := errors.New("external execution generation changed")
	calls := 0
	provider := newACPProvider(mcp.NewHost(newMemStore()), Options{CheckExecution: func(ctx context.Context, req NodeReq) error {
		calls++
		if req.RunID != "expected-run" {
			t.Fatal("execution check lost the concrete resource scope")
		}
		return denied
	}}).(*acpProvider)
	request := NodeReq{RunID: "expected-run"}
	if sb, acp, _, err := provider.openSandbox(t.Context(), request); !errors.Is(err, denied) || sb != nil || acp != nil {
		t.Fatal("sandbox create ran after execution revocation", err)
	}
	// A nil client deliberately proves denial happens before any native call.
	if result, err := provider.streamChat(t.Context(), nil, request, "late prompt", nil); !errors.Is(err, denied) || result != nil {
		t.Fatal("native prompt ran after execution revocation", err)
	}
	if calls != 2 {
		t.Fatal("each native boundary requires a fresh execution check", calls)
	}
}

func TestExecutionAuthorityMonitorCancelsOnlyItsOriginalOperation(t *testing.T) {
	var revoked atomic.Bool
	provider := newACPProvider(mcp.NewHost(newMemStore()), Options{CheckExecution: func(ctx context.Context, req NodeReq) error {
		if req.NodeID == "old-operation" && revoked.Load() {
			return executionlineage.ErrConflict
		}
		return nil
	}}).(*acpProvider)
	old, stopOld, err := provider.executionContextWithTiming(t.Context(), NodeReq{RunID: "run", NodeID: "old-operation"}, time.Second, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer stopOld()
	newer, stopNew, err := provider.executionContextWithTiming(t.Context(), NodeReq{RunID: "run", NodeID: "new-operation"}, time.Second, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer stopNew()
	revoked.Store(true)
	select {
	case <-old.Done():
		if !errors.Is(context.Cause(old), executionlineage.ErrConflict) {
			t.Fatal("lost the exact generation-revocation cause", context.Cause(old))
		}
	case <-time.After(time.Second):
		t.Fatal("already flying native operation ignored generation revocation")
	}
	if newer.Err() != nil || t.Context().Err() != nil {
		t.Fatal("revoked invocation cancelled another operation or its parent")
	}
}

func TestExecutionAuthorityMonitorStopsAtLastAcknowledgedDeadline(t *testing.T) {
	var calls atomic.Int32
	started, completed := make(chan struct{}), make(chan struct{})
	provider := newACPProvider(mcp.NewHost(newMemStore()), Options{CheckExecution: func(ctx context.Context, req NodeReq) error {
		if calls.Add(1) == 1 {
			return nil
		}
		close(started)
		<-ctx.Done()
		close(completed)
		return ctx.Err()
	}}).(*acpProvider)
	work, stop, err := provider.executionContextWithTiming(t.Context(), NodeReq{RunID: "run"}, 80*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fresh authority monitor never checked")
	}
	select {
	case <-work.Done():
		if !errors.Is(context.Cause(work), errExecutionAuthorityLost) {
			t.Fatal(context.Cause(work))
		}
	case <-time.After(time.Second):
		t.Fatal("blocked authority backend extended the last acknowledged deadline")
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("timed out authority read was left running")
	}
}

func TestRunAgentExecutionRevocationCancelsFlyingNativeTurnWithoutRetry(t *testing.T) {
	started := make(chan struct{})
	provider, _, _, manager, _, request := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction {
			close(started)
			return turnAction{stall: true, ackCancel: true}
		}
	})
	var revoked atomic.Bool
	provider.opts.CheckExecution = func(context.Context, NodeReq) error {
		if revoked.Load() {
			return executionlineage.ErrInactive
		}
		return nil
	}
	work, stop, err := provider.executionContextWithTiming(t.Context(), request, time.Second, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	result := make(chan error, 1)
	go func() { _, err := provider.RunAgent(work, request); result <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("native turn never reached the bridge")
	}
	revoked.Store(true)
	select {
	case err := <-result:
		if !errors.Is(err, executionlineage.ErrInactive) {
			t.Fatal("flying native turn did not preserve revocation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revoked native process kept its original turn running")
	}
	if manager.createCount() != 1 {
		t.Fatal("revoked native execution retried in a new sandbox", manager.createCount())
	}
	bridge := manager.bridge(0)
	bridge.mu.Lock()
	remaining := bridge.stalledOp
	bridge.mu.Unlock()
	if remaining != "" {
		t.Fatal("native cancellation was not acknowledged for the original operation")
	}
}
