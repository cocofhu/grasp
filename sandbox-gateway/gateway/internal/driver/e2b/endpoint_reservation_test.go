package e2b

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestDriverPausedEndpointsRemainReserved(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	h, err := d.Create(t.Context(), lifecycleSpec("pause-reservation"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	for port, address := range h.Endpoints {
		if listener, err := net.Listen("tcp4", address); err == nil {
			_ = listener.Close()
			t.Errorf("paused guest port %d lost its reserved endpoint %s", port, address)
		}
	}
	if _, err := d.Endpoints(t.Context(), h.ID); err == nil {
		t.Fatal("paused guest advertised endpoints")
	}
	if err := d.Start(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	endpoints, err := d.Endpoints(t.Context(), h.ID)
	if err != nil || !reflect.DeepEqual(endpoints, h.Endpoints) {
		t.Fatalf("resume changed reserved endpoints: %v, %v", endpoints, err)
	}
}

func TestTunnelSuspendRetainsEndpointsAndClosesStreams(t *testing.T) {
	m, f, creds := fixtureManager(t)
	address, err := m.Start(t.Context(), creds, 22, 0)
	if err != nil {
		t.Fatal(err)
	}
	conn := echoAt(t, address, "before pause")
	m.Suspend(creds.SandboxID)
	if _, ok := m.Get(creds.SandboxID, 22); ok || len(m.List(creds.SandboxID)) != 0 {
		t.Fatal("suspended tunnel advertised an active endpoint")
	}
	if listener, err := net.Listen("tcp4", address); err == nil {
		_ = listener.Close()
		t.Fatal("suspending released the stable binding")
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("suspending left the old stream open")
	}
	probe, err := net.DialTimeout("tcp4", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = probe.Close() }()
	_ = probe.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := probe.Read(make([]byte, 1)); err == nil {
		t.Fatal("suspended binding accepted traffic")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("suspended binding did not promptly reject traffic")
	}
	f.mu.Lock()
	f.token = "resumed-traffic"
	f.mu.Unlock()
	creds.TrafficToken = "resumed-traffic"
	_, port, _ := net.SplitHostPort(address)
	local, _ := strconv.Atoi(port)
	resumed, err := m.Start(t.Context(), creds, 22, local)
	if err != nil || resumed != address {
		t.Fatalf("resume changed binding: %s, %v", resumed, err)
	}
	echoAt(t, resumed, "new stream and credentials after pause")
}

func TestTunnelPublicationReservesBeforeControlAndReleasesFailure(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			occupied, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = occupied.Close() }()
			address := occupied.Addr().String()
			local := occupied.Addr().(*net.TCPAddr).Port
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if listener, err := net.Listen("tcp4", address); err == nil {
					_ = listener.Close()
					t.Error("relay publication preceded local endpoint reservation")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(server.Close)
			m, err := NewTunnelManager(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.CloseAll() })
			_ = occupied.Close()
			creds := RelayCredentials{SandboxID: "guest", TrafficToken: "traffic", RelayKey: "key"}
			got, err := m.Start(t.Context(), creds, 22, local)
			if status == http.StatusOK {
				if err != nil || got != address {
					t.Fatalf("publication did not retain binding: %s, %v", got, err)
				}
			} else {
				if err == nil || len(m.List(creds.SandboxID)) != 0 {
					t.Fatal("failed publication advertised an endpoint")
				}
				listener, err := net.Listen("tcp4", address)
				if err != nil {
					t.Fatalf("failed publication leaked a binding: %v", err)
				}
				_ = listener.Close()
			}
		})
	}
}

func TestTunnelSuspendDuringPublicationKeepsReservationDisabled(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	m, err := NewTunnelManager(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.CloseAll() })
	creds := RelayCredentials{SandboxID: "guest", TrafficToken: "traffic", RelayKey: "key"}
	done := make(chan error, 1)
	go func() {
		_, err := m.Start(t.Context(), creds, 22, 0)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("publication did not reach relay")
	}
	m.Suspend(creds.SandboxID)
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale publication re-enabled a suspended tunnel")
	}
	if _, ok := m.Get(creds.SandboxID, 22); ok {
		t.Fatal("suspended reservation advertised an endpoint")
	}
	m.mu.Lock()
	tunnel := m.tunnels[creds.SandboxID][22]
	m.mu.Unlock()
	if tunnel == nil {
		t.Fatal("stale publication released the suspended reservation")
	}
	if listener, err := net.Listen("tcp4", tunnel.listener.Addr().String()); err == nil {
		_ = listener.Close()
		t.Fatal("suspended reservation was available to another connection")
	}
	if _, err := m.Start(t.Context(), creds, 22, 0); err != nil {
		t.Fatal(err)
	}
}

func TestTunnelFailedResumeRetainsReservation(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	m, err := NewTunnelManager(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.CloseAll() })
	creds := RelayCredentials{SandboxID: "guest", TrafficToken: "traffic", RelayKey: "key"}
	address, err := m.Start(t.Context(), creds, 22, 0)
	if err != nil {
		t.Fatal(err)
	}
	m.Suspend(creds.SandboxID)
	status.Store(http.StatusInternalServerError)
	if _, err := m.Start(t.Context(), creds, 22, 0); err == nil {
		t.Fatal("failed resume succeeded")
	}
	if listener, err := net.Listen("tcp4", address); err == nil {
		_ = listener.Close()
		t.Fatal("failed resume released the stable binding")
	}
	status.Store(http.StatusOK)
	if got, err := m.Start(t.Context(), creds, 22, 0); err != nil || got != address {
		t.Fatalf("retry changed binding: %s, %v", got, err)
	}
}

func TestDriverRestoreReservesEndpointsBeforeProviderRequests(t *testing.T) {
	for _, paused := range []bool{false, true} {
		name := "running"
		if paused {
			name = "paused"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			f := newLifecycleProvider(t)
			opts := lifecycleOptions(t, f)
			d := openLifecycleDriver(t, opts)
			expected := make(map[string]map[int]string)
			for _, id := range []string{"restore-one", "restore-two"} {
				h, err := d.Create(t.Context(), lifecycleSpec(id))
				if err != nil {
					t.Fatal(err)
				}
				expected[id] = h.Endpoints
				if paused {
					if err := d.Stop(t.Context(), id); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			// Simulate a provider HTTP connection taking any still-free local
			// endpoint. Keep it occupied so the old restore order fails every time.
			var stolen []net.Listener
			t.Cleanup(func() {
				f.mu.Lock()
				defer f.mu.Unlock()
				for _, listener := range stolen {
					_ = listener.Close()
				}
			})
			f.mu.Lock()
			f.beforeRequest = func(*http.Request) {
				for id, endpoints := range expected {
					for port, address := range endpoints {
						if listener, err := net.Listen("tcp4", address); err == nil {
							stolen = append(stolen, listener)
							t.Errorf("provider request preceded reservation of %s port %d", id, port)
						}
					}
				}
			}
			f.mu.Unlock()
			restored := openLifecycleDriver(t, opts)
			for id, want := range expected {
				if paused {
					if err := restored.Start(ctx, id); err != nil {
						t.Fatal(err)
					}
				}
				got, err := restored.Endpoints(ctx, id)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("restart changed %s endpoints: %v, %v", id, got, err)
				}
			}
		})
	}
}
