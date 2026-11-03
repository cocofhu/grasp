package services

import (
	"context"
	"encoding/json"
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
	wireTurns := make(chan []byte, 2)
	bridge, _ := fakeACPServerFramesOnChat(t, func(turn []byte) { wireTurns <- turn }, providerTurnFrames("end_turn", "", "guest reply")...)
	defer bridge.Close()
	target, err := url.Parse(bridge.URL)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httputil.NewSingleHostReverseProxy(target)
	var created atomic.Int32
	connected := make(chan string, 2)
	deleted := make(chan string, 2)
	modelWrites := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chats":
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":"guest-%d"}`, created.Add(1))
		case r.Method == http.MethodGet && r.URL.Path == "/api/models":
			_, _ = fmt.Fprint(w, `{"configured":true,"models":[{"id":"guest-model"}],"reasoningEffort":""}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/model":
			modelWrites <- r.URL.Query().Get("chat")
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
		if changed, err := client.SetIndependentModelSettings(ctx, "unlisted-model", ""); changed || err == nil {
			t.Fatalf("unlisted model: changed=%v err=%v", changed, err)
		}
		if changed, err := client.SetIndependentModelSettings(ctx, "guest-model", ""); !changed || err != nil {
			t.Fatalf("guest model: changed=%v err=%v", changed, err)
		}
		if got := <-modelWrites; got != expected {
			t.Fatalf("switched %q, want %q", got, expected)
		}
		file := models.PromptImage{MimeType: "text/plain", Name: "guest.txt", Data: "Z3Vlc3Q="}
		result, err := ChatPreviewGuestTurn(ctx, client, "guest prompt", []models.PromptImage{file}, nil)
		closeChat()
		if err != nil || result == nil || result.Narration != "guest reply" {
			t.Fatalf("guest reply: %+v, %v", result, err)
		}
		var turn struct {
			Images []models.PromptImage `json:"images"`
		}
		if err := json.Unmarshal(<-wireTurns, &turn); err != nil {
			t.Fatal(err)
		}
		if len(turn.Images) != 1 || turn.Images[0].Data != file.Data || turn.Images[0].Name != file.Name || turn.Images[0].MimeType != file.MimeType {
			t.Fatalf("bridge attachments: %+v", turn.Images)
		}
		if got := <-connected; got != expected {
			t.Fatalf("connected to %q, want %q", got, expected)
		}
		if got := <-deleted; got != expected {
			t.Fatalf("deleted %q, want %q", got, expected)
		}
	}
}
