package services

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestOpenPreviewGuestUsesSeparateBridgeChats(t *testing.T) {
	bridge, _ := fakeACPServerFrames(t, providerTurnFrames("end_turn", "", "guest reply")...)
	defer bridge.Close()
	target, err := url.Parse(bridge.URL)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httputil.NewSingleHostReverseProxy(target)
	var created atomic.Int32
	connected := make(chan string, 2)
	deleted := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chats":
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":"guest-%d"}`, created.Add(1))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/chats/"):
			deleted <- strings.TrimPrefix(r.URL.Path, "/api/chats/")
		default:
			if r.URL.Path == "/ws" {
				connected <- r.URL.Query().Get("chat")
			}
			upstream.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()
	host, portString, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatal(err)
	}
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	row := models.Sandbox{Name: "shared-preview", Status: "running", Host: host, ACPPort: port, Token: "bridge-fixture", Profile: "agentA"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, expected := range []string{"guest-1", "guest-2"} {
		client, closeChat, err := s.OpenPreviewGuest(ctx, row)
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.ChatStreamResult(ctx, "guest prompt", nil, nil)
		closeChat()
		if err != nil || result == nil || result.Narration != "guest reply" {
			t.Fatalf("guest reply: %+v, %v", result, err)
		}
		if got := <-connected; got != expected {
			t.Fatalf("connected to %q, want %q", got, expected)
		}
		if got := <-deleted; got != expected {
			t.Fatalf("deleted %q, want %q", got, expected)
		}
	}
}
