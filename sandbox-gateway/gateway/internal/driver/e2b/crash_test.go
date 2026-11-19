package e2b

import (
	"strings"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
)

func fastCrashConfirm(t *testing.T) {
	t.Helper()
	old := crashConfirmDelay
	crashConfirmDelay = time.Millisecond
	t.Cleanup(func() { crashConfirmDelay = old })
}

func crashTestDriver(t *testing.T, id string) (*lifecycleProvider, *Driver) {
	t.Helper()
	fastCrashConfirm(t)
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec(id)); err != nil {
		t.Fatal(err)
	}
	return f, d
}

func expireStatusCache(t *testing.T, d *Driver, id string) {
	t.Helper()
	e, err := d.lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.statusAt = time.Time{}
	e.mu.Unlock()
}

func TestProxyNotFoundWhileListedRunningIsACrash(t *testing.T) {
	f, d := crashTestDriver(t, "gw-crash")
	f.mu.Lock()
	f.goneProbes = -1
	f.mu.Unlock()
	expireStatusCache(t, d, "gw-crash")

	if st, err := d.Status(t.Context(), "gw-crash"); st != driver.StatusCrashed || err != nil {
		t.Fatalf("status = %q, %v; want crashed", st, err)
	}
	if h, err := d.Get(t.Context(), "gw-crash"); err != nil || h.Status != driver.StatusCrashed {
		t.Fatalf("get = %+v, %v; want crashed", h, err)
	}
	list, err := d.List(t.Context())
	if err != nil || len(list) != 1 || list[0].Status != driver.StatusCrashed {
		t.Fatalf("list = %+v, %v; want one crashed guest", list, err)
	}
	if err := d.Destroy(t.Context(), "gw-crash"); err != nil {
		t.Fatal(err)
	}
}

func TestSingleProxyNotFoundIsNotACrash(t *testing.T) {
	f, d := crashTestDriver(t, "gw-blip")
	f.mu.Lock()
	f.goneProbes = 1
	f.mu.Unlock()
	expireStatusCache(t, d, "gw-blip")

	if st, err := d.Status(t.Context(), "gw-blip"); st != driver.StatusError || err == nil {
		t.Fatalf("status = %q, %v; want an unconfirmed error", st, err)
	}
	if st, err := d.Status(t.Context(), "gw-blip"); st != driver.StatusRunning || err != nil {
		t.Fatalf("status = %q, %v; want running", st, err)
	}
}

func TestUnreachableRelayIsNotACrash(t *testing.T) {
	f, d := crashTestDriver(t, "gw-down")
	f.mu.Lock()
	f.healthStatus = 502
	f.mu.Unlock()
	expireStatusCache(t, d, "gw-down")

	if st, err := d.Status(t.Context(), "gw-down"); st != driver.StatusError || err == nil {
		t.Fatalf("status = %q, %v; want error", st, err)
	}
	if h, err := d.Get(t.Context(), "gw-down"); err == nil || h != nil {
		t.Fatalf("get = %+v, %v; want error", h, err)
	}
}

func TestProxyNotFoundMatchesOnlyTheRequestedSandbox(t *testing.T) {
	body := `{"sandboxId":"sbx-a","message":"The sandbox was not found","code":502}`
	for _, c := range []struct {
		body, id string
		want     bool
	}{
		{body, "sbx-a", true},
		{body, "sbx-b", false},
		{`{"sandboxId":"sbx-a","message":"Sandbox is still transitioning","code":502}`, "sbx-a", false},
		{`<html>The sandbox was not found</html>`, "sbx-a", false},
	} {
		if got := proxyNotFound(strings.NewReader(c.body), c.id); got != c.want {
			t.Errorf("proxyNotFound(%s, %s) = %v, want %v", c.body, c.id, got, c.want)
		}
	}
}
