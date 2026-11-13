package e2b

import (
	"context"
	"net/http"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
)

func TestDriverStatusOfPublishedGuestDoesNotWaitForServices(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("busy-guest")); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.ready = false
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started := time.Now()
	status, err := d.Status(ctx, "busy-guest")
	if err != nil || status != driver.StatusRunning {
		t.Fatalf("status = %q, %v", status, err)
	}
	h, err := d.Get(ctx, "busy-guest")
	if err != nil || h.Status != driver.StatusRunning || h.Endpoints[8765] == "" {
		t.Fatalf("published endpoints withheld while the IDE is down: %+v %v", h, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("status read waited %s for guest services", elapsed)
	}
}

func TestDriverStatusOfHangingRelayFailsWithinObserveBudget(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("hung-relay")); err != nil {
		t.Fatal(err)
	}
	budget := observeTimeout
	observeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { observeTimeout = budget })
	f.mu.Lock()
	f.beforeRequest = func(r *http.Request) {
		if r.URL.Path == "/_bridge/health" {
			<-r.Context().Done()
		}
	}
	f.mu.Unlock()
	e, _ := d.lookup("hung-relay")
	e.mu.Lock()
	e.statusAt = time.Time{}
	e.mu.Unlock()

	started := time.Now()
	if status, err := d.Status(t.Context(), "hung-relay"); err == nil || status != driver.StatusError {
		t.Fatalf("hanging relay reported %q, %v", status, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("status read held the entry for %s", elapsed)
	}
	f.mu.Lock()
	f.beforeRequest = nil
	f.mu.Unlock()
	if status, err := d.Status(t.Context(), "hung-relay"); err != nil || status != driver.StatusRunning {
		t.Fatalf("recovered relay reported %q, %v", status, err)
	}
}
