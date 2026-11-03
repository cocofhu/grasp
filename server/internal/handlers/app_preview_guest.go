package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const appPreviewGuestProtocol = "grasp-preview-guest"

type previewGuestRequest struct {
	Text            string  `json:"text"`
	ModelID         *string `json:"modelId"`
	ReasoningEffort string  `json:"reasoningEffort"`
}

func (h *Handlers) writePreviewGuestModelCatalog(c *gin.Context, sb models.Sandbox) {
	if h.Sbx == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model catalog unavailable"})
		return
	}
	settings, err := h.Sbx.ChatModelSettings(c.Request.Context(), sb.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model catalog unavailable"})
		return
	}
	models := []sandbox.ChatModelOption{}
	if settings.Configured {
		models = append(models, settings.Models...)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"runtime": gin.H{"models": models, "default": settings.Default}})
}

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
	requests := make(chan previewGuestRequest, 1)
	go readPreviewGuestRequests(ctx, cancel, conn, requests)
	go h.watchPreviewGuest(ctx, cancel, conn, claims)
	var client *sandbox.ACPClient
	var closeChat func()
	var history []string
	var restoreHistory bool
	var selection *previewGuestRequest
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
		case request := <-requests:
			if active, _ := h.activeAppPreviewTicket(claims); active == nil {
				return
			}
			if h.Shutdown != nil && h.Shutdown.IsDraining() {
				writePreviewGuestUnavailable(conn, request)
				continue
			}
			if client == nil || !client.IsConnected() {
				if closeChat != nil {
					closeChat()
				}
				restoreHistory = len(history) > 0
				client, closeChat, err = h.Sbx.OpenPreviewGuest(ctx, sb)
				if err != nil {
					writePreviewGuestUnavailable(conn, request)
					continue
				}
				if selection != nil && request.ModelID == nil {
					if _, err := client.SetIndependentModelSettings(ctx, *selection.ModelID, selection.ReasoningEffort); err != nil {
						closeChat()
						client, closeChat = nil, nil
						writePreviewGuestUnavailable(conn, request)
						continue
					}
				}
			}
			if request.ModelID != nil {
				restarted, err := client.SetIndependentModelSettings(ctx, *request.ModelID, request.ReasoningEffort)
				restoreHistory = restoreHistory || restarted
				if err == nil {
					selection = &request
				}
				if writeChatJSON(conn, gin.H{"type": "model", "ok": err == nil}) != nil {
					return
				}
				continue
			}
			prompt := request.Text
			if restoreHistory && len(history) > 0 {
				prompt = "Earlier messages in this preview chat (context only):\n" +
					strings.Join(history, "\n\n") + "\n\nCurrent user message:\n" + prompt
			}
			narration, completed, sent := writePreviewGuestTurn(ctx, cancel, conn, client, prompt)
			if !sent {
				return
			}
			if completed {
				restoreHistory = false
				history = append(history, "User: "+textutil.TruncateBytes(request.Text, 2048, "…")+
					"\nAgent: "+textutil.TruncateBytes(narration, 2048, "…"))
				if len(history) > 8 {
					history = history[len(history)-8:]
				}
			}
		}
	}
}

func writePreviewGuestUnavailable(conn *websocket.Conn, request previewGuestRequest) {
	if request.ModelID != nil {
		_ = writeChatJSON(conn, gin.H{"type": "model", "ok": false})
		return
	}
	_ = writeChatJSON(conn, gin.H{"type": "error", "code": "unavailable"})
}

func readPreviewGuestRequests(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, requests chan<- previewGuestRequest) {
	defer cancel()
	for ctx.Err() == nil {
		var frame previewGuestRequest
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}
		frame.Text = strings.TrimSpace(frame.Text)
		if frame.ModelID == nil {
			if frame.Text == "" || len(frame.Text) > maxPreviewChatText {
				return
			}
		} else if frame.Text != "" || len(*frame.ModelID) > 512 || len(frame.ReasoningEffort) > 64 {
			return
		}
		select {
		case requests <- frame:
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

func writePreviewGuestTurn(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, client *sandbox.ACPClient, text string) (string, bool, bool) {
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
	narration := previewGuestNarration(result)
	return narration, !failed, writeChatJSON(conn, gin.H{"type": "done", "text": narration, "failed": failed}) == nil
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
