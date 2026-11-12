package e2b

import (
	"testing"
	"time"
)

func TestCreateRequestsConfiguredAutoPause(t *testing.T) {
	for _, autoPause := range []bool{true, false} {
		f := newLifecycleProvider(t)
		opts := lifecycleOptions(t, f)
		opts.AutoPause = autoPause
		d := openLifecycleDriver(t, opts)
		if _, err := d.Create(t.Context(), lifecycleSpec("gw-pause")); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		got := f.autoPause
		f.mu.Unlock()
		if got != autoPause {
			t.Fatalf("create sent autoPause=%v, want %v", got, autoPause)
		}
	}
}

func TestRenewalRecordsProviderCapOnce(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("gw-cap")); err != nil {
		t.Fatal(err)
	}
	capAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	f.mu.Lock()
	f.maxEndAt = capAt
	f.mu.Unlock()

	// The first renewal is silently capped; the next sweep observes the cap.
	d.sweep(t.Context())
	d.sweep(t.Context())
	e, _ := d.lookup("gw-cap")
	e.mu.Lock()
	first := e.leaseCapAt
	e.mu.Unlock()
	if !first.Equal(capAt) {
		t.Fatalf("capped renewal not recorded: got %v, want %v", first, capAt)
	}

	d.sweep(t.Context())
	e.mu.Lock()
	second := e.leaseCapAt
	e.mu.Unlock()
	if !second.Equal(first) {
		t.Fatal("unchanged cap was recorded again")
	}
}

func TestRenewalWithinLeaseIsNotACap(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("gw-ok")); err != nil {
		t.Fatal(err)
	}
	d.sweep(t.Context())
	d.sweep(t.Context())
	f.mu.Lock()
	renewed := f.renewed
	f.mu.Unlock()
	if renewed < 1 {
		t.Fatal("running lease was not renewed")
	}
	e, _ := d.lookup("gw-ok")
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.leaseCapAt.IsZero() {
		t.Fatalf("full renewal reported as capped at %v", e.leaseCapAt)
	}
}
