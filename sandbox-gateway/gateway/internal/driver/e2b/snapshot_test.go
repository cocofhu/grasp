package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
)

func TestPauseSnapshotWaitsForDurabilityAndForwardsRetention(t *testing.T) {
	provider := newLifecycleProvider(t)
	provider.snapshotReplies = []SnapshotStatus{{State: "pending"}, {State: "ready", BuildID: "durable-build", Keep: 5, WorkloadReleased: true}}
	observed := make(chan struct{}, 1)
	provider.beforeRequest = func(r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/snapshot-status") {
			select {
			case observed <- struct{}{}:
			default:
			}
		}
	}
	drv := openLifecycleDriver(t, lifecycleOptions(t, provider))
	if _, err := drv.Create(t.Context(), lifecycleSpec("durability")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- drv.PauseSnapshot(t.Context(), "durability", 5) }()
	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		t.Fatal("did not check snapshot persistence")
	}
	select {
	case err := <-done:
		t.Fatalf("pause returned before snapshot became durable: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pause did not finish after snapshot became ready")
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.snapshotKeep != 5 || provider.snapshotRequests != 2 || provider.paused != 1 || provider.deleted != 0 {
		t.Fatalf("wrong snapshot operations: keep=%d checks=%d pauses=%d deletes=%d", provider.snapshotKeep, provider.snapshotRequests, provider.paused, provider.deleted)
	}
}

func TestPauseSnapshotFailureRetainsGuestAndRetryChecksExistingSnapshot(t *testing.T) {
	for _, reason := range []string{"upload-failed", "provider-has-no-proof", "runtime-exit-unconfirmed"} {
		t.Run(reason, func(t *testing.T) {
			provider := newLifecycleProvider(t)
			if reason == "provider-has-no-proof" {
				provider.snapshotHTTPStatus = http.StatusNotFound
			} else if reason == "runtime-exit-unconfirmed" {
				provider.snapshotReplies = []SnapshotStatus{{State: "ready", BuildID: "durable-build", WorkloadReleased: false}}
			} else {
				provider.snapshotReplies = []SnapshotStatus{{State: "failed", LastError: testProviderSecret}}
			}
			drv := openLifecycleDriver(t, lifecycleOptions(t, provider))
			if _, err := drv.Create(t.Context(), lifecycleSpec("failed-save")); err != nil {
				t.Fatal(err)
			}
			err := drv.PauseSnapshot(t.Context(), "failed-save", 1)
			assertSafeControlError(t, err)
			provider.mu.Lock()
			if provider.paused != 1 || provider.deleted != 0 || len(provider.sandboxes) != 1 {
				provider.mu.Unlock()
				t.Fatal("failed snapshot destroyed the recoverable guest")
			}
			provider.snapshotHTTPStatus = 0
			provider.snapshotReplies = nil
			provider.mu.Unlock()
			entry, err := drv.lockEntry("failed-save")
			if err != nil {
				t.Fatal(err)
			}
			phase := entry.record.Phase
			entry.mu.Unlock()
			if phase != "paused" {
				t.Fatalf("lost recoverable provider identity: phase=%s", phase)
			}
			if err := drv.PauseSnapshot(t.Context(), "failed-save", 1); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
			provider.mu.Lock()
			defer provider.mu.Unlock()
			if provider.paused != 1 || provider.snapshotRequests != 2 {
				t.Fatal("retry made another snapshot instead of confirming the pending one")
			}
		})
	}
}

func TestWaitSnapshotReadyRejectsUnprovenOrFailedSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     SnapshotStatus
		httpStatus int
	}{
		{"ready-without-build", SnapshotStatus{State: "ready"}, http.StatusOK},
		{"ready-without-runtime-exit", SnapshotStatus{State: "ready", BuildID: "durable-build"}, http.StatusOK},
		{"ready-with-error", SnapshotStatus{State: "ready", BuildID: "durable-build", WorkloadReleased: true, LastError: testProviderSecret}, http.StatusOK},
		{"pending-with-error", SnapshotStatus{State: "pending", LastError: testProviderSecret}, http.StatusOK},
		{"failed", SnapshotStatus{State: "failed", LastError: testProviderSecret}, http.StatusOK},
		{"unknown-state", SnapshotStatus{State: "paused", BuildID: "not-persisted"}, http.StatusOK},
		{"unsupported", SnapshotStatus{}, http.StatusNotImplemented},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/sandboxes/guest/snapshot-status" {
					t.Error("wrong snapshot proof route")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.httpStatus)
				_ = json.NewEncoder(w).Encode(tc.status)
			})
			assertSafeControlError(t, client.WaitSnapshotReady(t.Context(), "guest"))
		})
	}
}

func TestWaitSnapshotReadyHonorsCancellationAndValidatesInput(t *testing.T) {
	var calls atomic.Int32
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SnapshotStatus{State: "pending"})
	})
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if err := client.WaitSnapshotReady(ctx, "guest"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait ignored deadline: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected proof requests: %d", calls.Load())
	}
	if _, err := client.SnapshotStatus(t.Context(), "../invalid"); err == nil {
		t.Fatal("invalid snapshot identity accepted")
	}
	for _, keep := range []int{0, -1} {
		if err := client.Pause(t.Context(), "guest", keep); err == nil {
			t.Fatalf("invalid keep accepted: %d", keep)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("invalid inputs reached provider")
	}
}

func TestDestroyRetainsJournalUntilSnapshotCleanupCompletes(t *testing.T) {
	provider := newLifecycleProvider(t)
	provider.deleteCleanupPending = 3
	opts := lifecycleOptions(t, provider)
	drv := openLifecycleDriver(t, opts)
	if _, err := drv.Create(t.Context(), lifecycleSpec("cleanup-pending")); err != nil {
		t.Fatal(err)
	}
	if err := drv.PauseSnapshot(t.Context(), "cleanup-pending", 5); err != nil {
		t.Fatal(err)
	}
	if err := drv.Destroy(t.Context(), "cleanup-pending"); err == nil {
		t.Fatal("delete acknowledged before snapshot cleanup completed")
	} else {
		var pending *driver.CleanupPendingError
		if !errors.As(err, &pending) || !pending.WorkloadReleased {
			t.Fatalf("lost confirmed runtime release while disk GC is pending: %v", err)
		}
	}
	entry, err := drv.lockEntry("cleanup-pending")
	if err != nil {
		t.Fatalf("pending cleanup lost tracked identity: %v", err)
	}
	phase, providerID := entry.record.Phase, entry.record.Provider.ID
	entry.mu.Unlock()
	if phase != "deleting" || providerID == "" {
		t.Fatalf("lost cleanup journal: %s %s", phase, providerID)
	}
	provider.mu.Lock()
	if len(provider.sandboxes) != 0 || !provider.deletedSnapshots[providerID] {
		provider.mu.Unlock()
		t.Fatal("fixture did not retire guest but retain snapshots")
	}
	provider.mu.Unlock()
	if err := drv.Close(); err != nil {
		t.Fatal(err)
	}
	// A gateway restart must retain this ID even though GET /sandboxes/id is 404.
	restarted := openLifecycleDriver(t, opts)
	if err := restarted.Destroy(t.Context(), "cleanup-pending"); err == nil {
		t.Fatal("restart mistook missing live guest for successful disk cleanup")
	}
	provider.mu.Lock()
	provider.deleteCleanupPending = 0
	provider.mu.Unlock()
	if err := restarted.Destroy(t.Context(), "cleanup-pending"); err != nil {
		t.Fatalf("retry failed after provider completed GC: %v", err)
	}
	if entry, err := restarted.lockEntry("cleanup-pending"); err == nil {
		entry.mu.Unlock()
		t.Fatal("successful cleanup retained journal")
	}
}

func TestUncertainCreateCleanupKeepsDiscoveredIdentityAcrossRestart(t *testing.T) {
	provider := newLifecycleProvider(t)
	provider.deleteCleanupPending = 2
	opts := lifecycleOptions(t, provider)
	drv := openLifecycleDriver(t, opts)
	if _, err := drv.Create(t.Context(), lifecycleSpec("uncertain-cleanup")); err != nil {
		t.Fatal(err)
	}
	entry, err := drv.lockEntry("uncertain-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	providerID := entry.record.Provider.ID
	entry.record.Provider = Sandbox{}
	entry.record.UncertainUntil = time.Now().Add(time.Minute)
	err = drv.save(&entry.record)
	entry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Destroy(t.Context(), "uncertain-cleanup"); err == nil {
		t.Fatal("asynchronous deletion was reported complete")
	}
	if err := drv.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := openLifecycleDriver(t, opts)
	if err := restarted.Destroy(t.Context(), "uncertain-cleanup"); err == nil {
		t.Fatal("empty live list lost pending snapshot cleanup")
	}
	entry, err = restarted.lockEntry("uncertain-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	ids := append([]string(nil), entry.record.CleanupIDs...)
	entry.mu.Unlock()
	if len(ids) != 1 || ids[0] != providerID {
		t.Fatalf("lost discovered identity: %v", ids)
	}
	provider.mu.Lock()
	provider.deleteCleanupPending = 0
	provider.mu.Unlock()
	if err := restarted.Destroy(t.Context(), "uncertain-cleanup"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteConfirmedRequiresRuntimeReleaseAndAllSnapshotCleanup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		released bool
		complete bool
	}{
		{"complete", `{"state":"deleted","pendingCleanup":0,"workloadReleased":true}`, true, true},
		{"historical-builds-pending", `{"state":"deleted","pendingCleanup":3,"workloadReleased":true}`, true, false},
		{"runtime-exit-not-proven", `{"state":"deleted","pendingCleanup":0,"workloadReleased":false}`, false, false},
		{"older-provider-missing-proof", `{"state":"deleted","pendingCleanup":0}`, false, false},
		{"snapshot-still-pending", `{"state":"pending","pendingCleanup":0,"workloadReleased":true}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch {
				case r.Method == http.MethodDelete && r.URL.Path == "/sandboxes/guest":
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == "/sandboxes/guest/snapshot-status":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.body))
				default:
					t.Errorf("unexpected provider call: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			})
			drv := &Driver{client: client}
			err := drv.deleteConfirmed(t.Context(), "guest")
			if tc.complete {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var pending *driver.CleanupPendingError
				if !errors.As(err, &pending) || pending.WorkloadReleased != tc.released {
					t.Fatalf("wrong cleanup/runtime proof: %#v %v", pending, err)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("delete did not independently verify cleanup: calls=%d", calls.Load())
			}
		})
	}
}
