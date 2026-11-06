package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
)

type lifecycleRequestLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *lifecycleRequestLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func concurrencyDriver(t *testing.T, f *lifecycleProvider) (*Driver, *lifecycleRequestLog) {
	t.Helper()
	requests := &lifecycleRequestLog{}
	opts := lifecycleOptions(t, f)
	opts.BootTimeout = 5 * time.Second
	opts.HTTPClient = &http.Client{Transport: controlRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.mu.Lock()
		requests.calls = append(requests.calls, r.Method+" "+r.URL.Path)
		requests.mu.Unlock()
		return http.DefaultTransport.RoundTrip(r)
	})}
	d := openLifecycleDriver(t, opts)
	// Tests drive the real maintenance operation explicitly. Retire the
	// periodic goroutine first so request order is controlled by channels.
	d.cancel()
	<-d.done
	return d, requests
}

func readLifecycleJournal(t *testing.T, d *Driver, id string) []byte {
	t.Helper()
	data, err := os.ReadFile(d.statePath(id))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// replaceRetiredEntry reproduces the point after another destroy/create has
// already retired old. The caller holds old.mu, as a blocked old operation
// would observe it. Only the fixture provider and private journal are changed.
func replaceRetiredEntry(t *testing.T, d *Driver, f *lifecycleProvider, old *entry) *entry {
	t.Helper()
	if err := d.tunnels.Close(old.record.Provider.ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	wire := *f.sandboxes[old.record.Provider.ID]
	delete(f.sandboxes, old.record.Provider.ID)
	f.deleted++
	f.created++
	wire.ID = fmt.Sprintf("guest-%d", f.created)
	wire.Metadata = maps.Clone(wire.Metadata)
	f.sandboxes[wire.ID] = &wire
	f.mu.Unlock()
	current := &entry{record: old.record}
	current.record.Provider = wire.sandbox()
	current.record.Ports = maps.Clone(old.record.Ports)
	current.record.Targets = maps.Clone(old.record.Targets)
	current.record.Phase = "running"
	current.mu.Lock()
	defer current.mu.Unlock()
	d.mu.Lock()
	d.entries[old.record.ID] = current
	d.mu.Unlock()
	if err := d.save(&current.record); err != nil {
		t.Fatal(err)
	}
	old.record.Phase = "deleting"
	return current
}

func assertCurrentLifecycle(t *testing.T, d *Driver, current *entry, before []byte) {
	t.Helper()
	got, err := d.lookup(current.record.ID)
	if err != nil || got != current {
		t.Fatalf("replacement entry lost: %v", err)
	}
	if after := readLifecycleJournal(t, d, current.record.ID); !bytes.Equal(after, before) {
		t.Fatal("retired generation changed the replacement journal")
	}
}

func TestDestroyWaitingOnRetiredEntryCannotOverwriteReplacement(t *testing.T) {
	f := newLifecycleProvider(t)
	d, requests := concurrencyDriver(t, f)
	spec := lifecycleSpec("destroy-generation")
	if _, err := d.Create(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	old, _ := d.lookup(spec.ID)
	old.mu.Lock()
	locked := true
	defer func() {
		if locked {
			old.mu.Unlock()
		}
	}()
	captured := make(chan struct{}, 1)
	d.mu.Lock()
	old.cancel = func() { captured <- struct{}{} }
	d.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- d.Destroy(t.Context(), spec.ID) }()
	select {
	case <-captured: // Destroy has the old pointer and is waiting for old.mu.
	case <-time.After(3 * time.Second):
		t.Fatal("Destroy did not capture the blocked old entry")
	}
	current := replaceRetiredEntry(t, d, f, old)
	before := readLifecycleJournal(t, d, spec.ID)
	requestCount := len(requests.snapshot())
	old.mu.Unlock()
	locked = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stale Destroy is not idempotent: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stale Destroy did not finish")
	}
	assertCurrentLifecycle(t, d, current, before)
	if len(requests.snapshot()) != requestCount {
		t.Fatal("stale Destroy touched the provider after replacement")
	}
	if err := d.Destroy(t.Context(), spec.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredMaintenanceAndCleanupCannotTouchReplacement(t *testing.T) {
	for _, phase := range []string{"deleting", "uncertain-create", "running"} {
		t.Run(phase, func(t *testing.T) {
			f := newLifecycleProvider(t)
			d, requests := concurrencyDriver(t, f)
			spec := lifecycleSpec("sweep-generation")
			if _, err := d.Create(t.Context(), spec); err != nil {
				t.Fatal(err)
			}
			old, _ := d.lookup(spec.ID)
			old.mu.Lock()
			current := replaceRetiredEntry(t, d, f, old)
			if phase == "uncertain-create" {
				// An expired unknown-create snapshot must not List/Delete the
				// replacement that shares its logical gateway metadata.
				old.record.Provider = Sandbox{}
			} else {
				old.record.Phase = phase
			}
			before := readLifecycleJournal(t, d, spec.ID)
			requestCount := len(requests.snapshot())
			if err := d.cleanup(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			old.mu.Unlock()
			// This is the same operation used by a previously captured sweep
			// snapshot; it must not renew or clean up a detached generation.
			d.sweepEntry(t.Context(), old)
			assertCurrentLifecycle(t, d, current, before)
			if len(requests.snapshot()) != requestCount {
				t.Fatal("stale maintenance/cleanup reached the provider")
			}
			d.sweep(t.Context())
			calls := requests.snapshot()[requestCount:]
			if len(calls) != 2 || calls[0] != "GET /sandboxes/"+current.record.Provider.ID || calls[1] != "POST /sandboxes/"+current.record.Provider.ID+"/timeout" {
				t.Fatalf("renewal did not target only the current provider: %v", calls)
			}
			if err := d.Destroy(t.Context(), spec.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentReinstallKeepsOneTrackedGuestAndRenewalTarget(t *testing.T) {
	f := newLifecycleProvider(t)
	d, requests := concurrencyDriver(t, f)
	spec := lifecycleSpec("reinstall-generation")
	if _, err := d.Create(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	old, _ := d.lookup(spec.ID)
	old.mu.Lock()
	locked := true
	defer func() {
		if locked {
			old.mu.Unlock()
		}
	}()
	captured := make(chan struct{}, 2)
	d.mu.Lock()
	old.cancel = func() { captured <- struct{}{} }
	d.mu.Unlock()
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- d.Reinstall(t.Context(), spec, false) }()
	}
	for range 2 {
		select {
		case <-captured:
		case <-time.After(3 * time.Second):
			t.Fatal("both Reinstalls must capture the same blocked old entry")
		}
	}
	old.mu.Unlock()
	locked = false
	succeeded := 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				succeeded++
			} else if !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("unexpected concurrent Reinstall result: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent Reinstall did not finish")
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected one replacement and one conflicting create, got %d successes", succeeded)
	}
	h, err := d.Get(t.Context(), spec.ID)
	if err != nil || h.Status != driver.StatusRunning {
		t.Fatalf("replacement guest became untracked: %v", err)
	}
	var journal record
	if err := json.Unmarshal(readLifecycleJournal(t, d, spec.ID), &journal); err != nil || journal.Phase != "running" || journal.Provider.ID != h.Name {
		t.Fatal("private journal does not track the active replacement")
	}
	f.mu.Lock()
	created, deleted, remaining := f.created, f.deleted, len(f.sandboxes)
	f.mu.Unlock()
	if created != 2 || deleted != 1 || remaining != 1 {
		t.Fatalf("concurrent Reinstall leaked or duplicated guests: created=%d deleted=%d remaining=%d", created, deleted, remaining)
	}
	requestCount := len(requests.snapshot())
	d.sweep(context.Background())
	calls := requests.snapshot()[requestCount:]
	if len(calls) != 2 || calls[0] != "GET /sandboxes/"+h.Name || calls[1] != "POST /sandboxes/"+h.Name+"/timeout" {
		t.Fatalf("current replacement is not independently renewable: %v", calls)
	}
	if err := d.Destroy(t.Context(), spec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.statePath(spec.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("final destroy retained a recovery journal")
	}
	f.mu.Lock()
	remaining = len(f.sandboxes)
	f.mu.Unlock()
	if remaining != 0 {
		t.Fatal("final destroy left an owned provider guest")
	}
}
