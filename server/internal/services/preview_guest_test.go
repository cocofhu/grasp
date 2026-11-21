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
	var selected atomic.Value
	selected.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chats":
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":"guest-%d"}`, created.Add(1))
		case r.Method == http.MethodGet && r.URL.Path == "/api/models":
			_, _ = fmt.Fprintf(w, `{"configured":true,"models":[{"id":"guest-model"}],"selected":%q,"reasoningEffort":""}`, selected.Load())
		case r.Method == http.MethodPost && r.URL.Path == "/api/model":
			selected.Store("guest-model")
			modelWrites <- r.URL.Query().Get("chat")
			_, _ = w.Write([]byte(`{"selected":"guest-model","resumed":false}`))
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
		selected.Store("")
		client, closeChat, err := s.OpenPreviewGuest(ctx, row)
		if err != nil {
			t.Fatal(err)
		}
		if changed, err := client.SetIndependentModelSettings(ctx, "unlisted-model", "", nil); changed || err == nil {
			t.Fatalf("unlisted model: changed=%v err=%v", changed, err)
		}
		if changed, err := client.SetIndependentModelSettings(ctx, "guest-model", "", nil); !changed || err != nil {
			t.Fatalf("guest model: changed=%v err=%v", changed, err)
		}
		if got := <-modelWrites; got != expected {
			t.Fatalf("switched %q, want %q", got, expected)
		}
		if execution := s.PreviewGuestExecution(ctx, row, client.ChatID()); execution.ModelID != "guest-model" || execution.AgentName != "agentA" {
			t.Fatalf("guest execution = %+v", execution)
		}
		file := models.PromptImage{MimeType: "text/plain", Name: "guest.txt", Data: "Z3Vlc3Q="}
		result, err := s.ChatPreviewGuestTurn(ctx, row, "run-shared", "preview-node", client, "guest prompt", []models.PromptImage{file}, nil)
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
	// Visitor turns consume the owner's tokens and are ledgered like the
	// workflow node's own visitor lanes.
	var events []models.TokenUsageEvent
	if err := db.Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("guest usage events = %+v", events)
	}
	for _, e := range events {
		if e.Source != models.TokenLedgerSourceWorkflow || e.Phase != models.TokenLedgerPhaseInteractive || e.RunID != "run-shared" || e.NodeID != "preview-node" ||
			e.SandboxID != row.ID || e.ModelKey != "fixture-model" || e.InputTokens != 7 || e.OutputTokens != 3 {
			t.Fatalf("guest usage event = %+v", e)
		}
	}
}

func TestPreviewGuestUsageFollowsPreviewOwner(t *testing.T) {
	solo := previewGuestUsage(&models.Sandbox{ID: 4, Purpose: SandboxPurposeSolo, ThreadID: "session-1"}, "solo-run", "solo-node")
	if solo.Source != models.TokenLedgerSourceSolo || solo.Phase != models.TokenLedgerPhaseChat || solo.ThreadID != "session-1" || solo.SandboxID != 4 || solo.RunID != "" {
		t.Fatalf("solo preview usage = %+v", solo)
	}
	run := previewGuestUsage(&models.Sandbox{ID: 5, Purpose: "run"}, "run-1", "node-1")
	if run.Source != models.TokenLedgerSourceWorkflow || run.Phase != models.TokenLedgerPhaseInteractive || run.RunID != "run-1" || run.NodeID != "node-1" || run.SandboxID != 5 {
		t.Fatalf("workflow preview usage = %+v", run)
	}
}
