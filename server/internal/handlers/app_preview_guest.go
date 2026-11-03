package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const appPreviewGuestProtocol = "grasp-preview-guest"

// PublicAppPreviewGuestChat owns one independent bridge chat per open page.
// Only narration crosses this socket. Each turn is scoped to the shared app.
func (h *Handlers) PublicAppPreviewGuestChat(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketTicket) {
		return
	}
	protocols := websocket.Subprotocols(c.Request)
	if !sameOriginRequest(c) || h.GateShareTickets == nil || h.Sbx == nil ||
		len(protocols) != 2 || protocols[0] != appPreviewGuestProtocol {
		c.Status(http.StatusUnauthorized)
		return
	}
	claims, _ := h.GateShareTickets.Lookup(protocols[1])
	link, sb := h.activeAppPreviewTicket(claims)
	if link == nil || link.PreviewChatMode != models.PreviewChatIsolated {
		c.Status(http.StatusUnauthorized)
		return
	}
	u := upgrader
	u.Subprotocols = []string{appPreviewGuestProtocol}
	conn, err := u.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(32 << 10)
	configureEventHeartbeat(conn, eventPongWait)
	ctx, cancel := context.WithDeadline(c.Request.Context(), link.ExpiresAt)
	prompts := make(chan string, 1)
	go readPreviewGuestPrompts(ctx, cancel, conn, prompts)
	go h.watchPreviewGuest(ctx, cancel, conn, claims)
	var client *sandbox.ACPClient
	var closeChat func()
	defer func() {
		cancel()
		_ = conn.Close()
		if closeChat != nil {
			closeChat()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case text := <-prompts:
			if active, _ := h.activeAppPreviewTicket(claims); active == nil {
				return
			}
			if h.Shutdown != nil && h.Shutdown.IsDraining() {
				_ = writeChatJSON(conn, gin.H{"type": "error", "code": "unavailable"})
				continue
			}
			if client == nil || !client.IsConnected() {
				if closeChat != nil {
					closeChat()
				}
				client, closeChat, err = h.Sbx.OpenPreviewGuest(ctx, sb)
				if err != nil {
					_ = writeChatJSON(conn, gin.H{"type": "error", "code": "unavailable"})
					continue
				}
			}
			if !writePreviewGuestTurn(ctx, cancel, conn, client, text) {
				return
			}
		}
	}
}

func readPreviewGuestPrompts(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, prompts chan<- string) {
	defer cancel()
	for ctx.Err() == nil {
		var frame struct {
			Text string `json:"text"`
		}
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}
		text := strings.TrimSpace(frame.Text)
		if text == "" || len(text) > maxPreviewChatText {
			return
		}
		select {
		case prompts <- text:
		default:
			return
		}
	}
}

func (h *Handlers) watchPreviewGuest(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, claims *gateshare.PreviewTicketClaims) {
	defer cancel()
	defer func() { _ = conn.Close() }()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if link, _ := h.activeAppPreviewTicket(claims); link == nil {
				return
			}
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(chatWriteWait)); err != nil {
				return
			}
		}
	}
}

func writePreviewGuestTurn(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, client *sandbox.ACPClient, text string) bool {
	last := time.Time{}
	result, err := client.ChatStreamResult(ctx, text, nil, func(progress *sandbox.ChatResult) {
		if time.Since(last) < 125*time.Millisecond {
			return
		}
		last = time.Now()
		if writeChatJSON(conn, gin.H{"type": "reply", "text": previewGuestNarration(progress)}) != nil {
			cancel()
		}
	})
	failed := err != nil || result == nil || result.Failed || result.Interrupted || result.ErrorText != ""
	return writeChatJSON(conn, gin.H{"type": "done", "text": previewGuestNarration(result), "failed": failed}) == nil
}

func previewGuestNarration(result *sandbox.ChatResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Segments)+1)
	for _, segment := range result.Segments {
		if segment.Narration != "" {
			parts = append(parts, segment.Narration)
		}
	}
	return strings.Join(append(parts, result.Narration), "\n\n")
}
