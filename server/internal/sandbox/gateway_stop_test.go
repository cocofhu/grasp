package sandbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGatewayStopCanOutlastOrdinaryControlRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("stop lost its gateway credentials")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client := NewGatewayClient(server.URL, "fixture-key")
	client.http.Timeout = 5 * time.Millisecond
	if err := client.Stop(t.Context(), "retained"); err != nil {
		t.Fatalf("stop inherited the shorter ordinary request timeout: %v", err)
	}
	if _, err := client.Get(t.Context(), "retained"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ordinary requests lost their original timeout: %v", err)
	}
}

type stopRequestRoundTripper func(*http.Request) (*http.Response, error)

func (f stopRequestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGatewayStopHasBoundedTimeForConfirmedProcessExit(t *testing.T) {
	client := NewGatewayClient("http://gateway.example.test", "fixture-key")
	client.http.Transport = stopRequestRoundTripper(func(r *http.Request) (*http.Response, error) {
		deadline, bounded := r.Context().Deadline()
		if remaining := time.Until(deadline); !bounded || remaining < 2*time.Minute || remaining > 3*time.Minute {
			t.Errorf("stop cannot cover the driver's two-minute confirmation: bounded=%v remaining=%v", bounded, remaining)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := client.Stop(context.Background(), "retained"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.http.Transport = stopRequestRoundTripper(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})
	if err := client.Stop(ctx, "retained"); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop ignored caller cancellation: %v", err)
	}
}
