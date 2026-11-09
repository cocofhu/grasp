package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestRunAgentDurableDispatchLossRetainsOriginalAttemptWithoutNativeReplay(t *testing.T) {
	p, _, _, manager, _, req := setupProvider(t, func(int) chatFunc { return func(int) turnAction { return turnAction{dropConn: true} } })
	var attempts, dispatches atomic.Int32
	req.PrepareProviderAttempt = func(context.Context) (int, error) { return int(attempts.Add(1)), nil }
	req.BeforeProviderDispatch = func(_ context.Context, attempt int) error {
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
	req.PrepareProviderAttempt = func(context.Context) (int, error) { return 0, ErrProviderDispatchUnknown }
	_, err := p.RunAgent(context.Background(), req)
	if !errors.Is(err, ErrProviderDispatchUnknown) || manager.createCount() != 0 {
		t.Fatal("refused native attempt was executed", err, manager.createCount())
	}
	req.PrepareProviderAttempt = func(context.Context) (int, error) { return 1, nil }
	req.ReattachSandbox = "retained-resource"
	_, err = p.RunAgent(context.Background(), req)
	if !errors.Is(err, ErrProviderDispatchUnknown) || manager.createCount() != 0 {
		t.Fatal("unknown reattach resent prompt", err, manager.createCount())
	}
}
