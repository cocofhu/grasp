package sandbox

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestBridgeLoginAcceptsAgentchatSessionCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/login" {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "tok123", Path: "/"})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	cookie, err := bridgeLogin(context.Background(), host, port, "secret")
	if err != nil {
		t.Fatalf("bridgeLogin: %v", err)
	}
	if cookie != "agentchat_session=tok123" {
		t.Fatalf("cookie = %q", cookie)
	}
}

func TestBridgeLoginRejectsOtherCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "other_session", Value: "x", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	if cookie, err := bridgeLogin(context.Background(), host, port, "secret"); err == nil {
		t.Fatalf("cookie=%q, want error", cookie)
	}
}

func TestBridgeLoginRequiresPassword(t *testing.T) {
	if _, err := bridgeLogin(context.Background(), "127.0.0.1", 1, " "); !errors.Is(err, errBridgePasswordRequired) {
		t.Fatalf("err=%v", err)
	}
	if err := NewACPClient("127.0.0.1", 1).Connect(context.Background()); !errors.Is(err, errBridgePasswordRequired) {
		t.Fatalf("Connect err=%v", err)
	}
	if err := WaitForACPReady(context.Background(), "127.0.0.1", 1, "", time.Second); !errors.Is(err, errBridgePasswordRequired) {
		t.Fatalf("WaitForACPReady err=%v", err)
	}
}
