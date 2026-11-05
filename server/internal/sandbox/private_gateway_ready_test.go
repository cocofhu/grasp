package sandbox

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const privateReadinessSecret = "synthetic-private-readiness-key"

type readinessRoundTripper func(*http.Request) (*http.Response, error)

func (f readinessRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPrivateGatewayWaitReadyRetriesUntilExpectedDriver(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/healthz" || r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" || r.Header.Get("Accept") != "application/json" {
			t.Error("health probe used a wrong route, method or credential")
		}
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, privateReadinessSecret)
			return
		}
		_, _ = io.WriteString(w, `{"status":"ok","driver":"e2b"}`)
	}))
	defer server.Close()
	gateway := NewGatewayClient(server.URL+"/", privateReadinessSecret)
	// The readiness barrier must not inherit the ordinary authenticated client's
	// proxy/transport configuration or use GatewayClient.do's body-bearing errors.
	gateway.http = &http.Client{Transport: readinessRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Error("health probe reused the authenticated gateway client")
		return nil, errors.New(privateReadinessSecret)
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := gateway.WaitReady(ctx, "e2b"); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 {
		t.Fatal("gateway readiness did not wait for successful driver confirmation")
	}
}

func TestPrivateGatewayWaitReadyRejectsUnhealthyAndUnboundedResponses(t *testing.T) {
	for name, body := range map[string]string{
		"wrong-driver":   `{"status":"ok","driver":"docker"}`,
		"wrong-status":   `{"status":"starting","driver":"e2b"}`,
		"missing-driver": `{"status":"ok"}`,
		"malformed":      privateReadinessSecret,
		"multiple-json":  `{"status":"ok","driver":"e2b"}{"secret":"` + privateReadinessSecret + `"}`,
		"oversized":      `{"status":"ok","driver":"e2b","extra":"` + strings.Repeat("x", 4096) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
			defer cancel()
			err := NewGatewayClient(server.URL, privateReadinessSecret).WaitReady(ctx, "e2b")
			if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), privateReadinessSecret) || strings.Contains(err.Error(), body) || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("readiness failed to reject response safely: %v", err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"status":"ok","driver":"e2b"}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if err := NewGatewayClient(server.URL, privateReadinessSecret).WaitReady(ctx, "e2b"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("non-200 health response accepted: %v", err)
	}
}

func TestPrivateGatewayWaitReadyNeverFollowsRedirectOrLeaksAPIKey(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		_, _ = io.WriteString(w, `{"status":"ok","driver":"e2b"}`)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" {
			t.Error("readiness request carried credentials")
		}
		http.Redirect(w, r, target.URL+"/?secret="+privateReadinessSecret, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err := NewGatewayClient(server.URL, privateReadinessSecret).WaitReady(ctx, "e2b")
	if !errors.Is(err, context.DeadlineExceeded) || redirected.Load() != 0 || strings.Contains(err.Error(), privateReadinessSecret) || strings.Contains(err.Error(), target.URL) {
		t.Fatalf("redirect or credential boundary failed: %v", err)
	}
}

func TestPrivateGatewayWaitReadyCancellationBoundsPendingRequest(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(finished)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- NewGatewayClient(server.URL, privateReadinessSecret).WaitReady(ctx, "e2b") }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("health request was not made")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("health request ignored cancellation")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("pending health connection did not close")
	}
}

func TestPrivateGatewayWaitReadyTransportFailureAndInvalidConfiguration(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err = NewGatewayClient("http://"+address, privateReadinessSecret).WaitReady(ctx, "e2b")
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), address) || strings.Contains(err.Error(), privateReadinessSecret) {
		t.Fatalf("transport error was not safely bounded: %v", err)
	}
	for _, endpoint := range []string{"", "file:///private/secret", "http://user:" + privateReadinessSecret + "@127.0.0.1", "http://127.0.0.1/?key=" + privateReadinessSecret, "http://127.0.0.1/#" + privateReadinessSecret, "http://%zz"} {
		if err := NewGatewayClient(endpoint, privateReadinessSecret).WaitReady(t.Context(), "e2b"); err == nil || strings.Contains(err.Error(), privateReadinessSecret) {
			t.Fatalf("invalid readiness endpoint accepted or disclosed: %v", err)
		}
	}
	var gateway *GatewayClient
	if err := gateway.WaitReady(t.Context(), "e2b"); err == nil {
		t.Fatal("nil gateway accepted")
	}
	if err := NewGatewayClient("http://127.0.0.1", privateReadinessSecret).WaitReady(t.Context(), ""); err == nil {
		t.Fatal("missing expected driver accepted")
	}
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	if err := NewGatewayClient("http://127.0.0.1", privateReadinessSecret).WaitReady(cancelled, "e2b"); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled readiness context ignored")
	}
}
