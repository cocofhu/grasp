package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/gorilla/websocket"
)

func TestPreviewGuestTierOnlyFramePreservesActualModelAndEffort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	requests := make(chan previewGuestRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		readPreviewGuestRequests(ctx, cancel, conn, requests, &previewGuestTurns{})
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.WriteJSON(map[string]any{"serviceTier": "fast"}); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requests:
		defer request.finish()
		high := "high"
		completePreviewGuestTierSelection(&request, sandbox.ChatModelSettings{Selected: "aux", ReasoningEffort: &high})
		if request.ModelID == nil || *request.ModelID != "aux" || request.ReasoningEffort != high || request.ServiceTier == nil || *request.ServiceTier != "fast" || request.Text != "" {
			t.Fatalf("tier-only frame changed independent selection: %+v", request)
		}
	case <-ctx.Done():
		t.Fatal("tier-only settings frame was discarded")
	}
}
