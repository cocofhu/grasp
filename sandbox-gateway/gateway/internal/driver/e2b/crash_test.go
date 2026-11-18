package e2b

import (
	"testing"

	"sandbox-gateway/internal/driver"
)

func setProvider(f *lifecycleProvider, timeoutStatus, healthStatus int) {
	f.mu.Lock()
	f.timeoutStatus, f.healthStatus = timeoutStatus, healthStatus
	f.mu.Unlock()
}

func TestRejectedRenewalsWithDeadRelayReportCrash(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("gw-crash")); err != nil {
		t.Fatal(err)
	}
	setProvider(f, 500, 503)

	d.sweep(t.Context())
	if st, _ := d.Status(t.Context(), "gw-crash"); st == driver.StatusCrashed {
		t.Fatal("a single rejected renewal reported a crash")
	}
	d.sweep(t.Context())
	if st, err := d.Status(t.Context(), "gw-crash"); st != driver.StatusCrashed || err != nil {
		t.Fatalf("status = %q, %v; want crashed", st, err)
	}
	h, err := d.Get(t.Context(), "gw-crash")
	if err != nil || h.Status != driver.StatusCrashed {
		t.Fatalf("get = %+v, %v; want crashed", h, err)
	}

	// A provider that accepts renewals again clears the verdict.
	setProvider(f, 0, 0)
	d.sweep(t.Context())
	if st, err := d.Status(t.Context(), "gw-crash"); st != driver.StatusRunning || err != nil {
		t.Fatalf("status after recovery = %q, %v; want running", st, err)
	}

	if err := d.Destroy(t.Context(), "gw-crash"); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedRenewalsWithLiveRelayAreNotACrash(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("gw-api-fault")); err != nil {
		t.Fatal(err)
	}
	setProvider(f, 500, 0)
	for range crashRenewalFailures + 1 {
		d.sweep(t.Context())
	}
	if st, err := d.Status(t.Context(), "gw-api-fault"); st != driver.StatusRunning || err != nil {
		t.Fatalf("status = %q, %v; want running", st, err)
	}
	e, _ := d.lookup("gw-api-fault")
	e.mu.Lock()
	failures := e.renewFailures
	e.mu.Unlock()
	if failures != crashRenewalFailures+1 {
		t.Fatalf("renewFailures = %d, want %d", failures, crashRenewalFailures+1)
	}
}
