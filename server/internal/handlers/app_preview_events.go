package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const appPreviewMainProtocol = "grasp-preview-main"

// PublicAppPreviewMainEvents serves a shared snapshot followed by changed tails.
// A main-mode ticket grants no access to raw Solo or workflow event streams.
func (h *Handlers) PublicAppPreviewMainEvents(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketPreviewEvents) {
		return
	}
	protocols := websocket.Subprotocols(c.Request)
	if !sameOriginRequest(c) || h.GateShareTickets == nil ||
		len(protocols) != 2 || protocols[0] != appPreviewMainProtocol {
		c.Status(http.StatusUnauthorized)
		return
	}
	claims, _ := h.GateShareTickets.Lookup(protocols[1])
	link, sb := h.activeAppPreviewTicket(claims)
	if link == nil || link.PreviewChatMode != models.PreviewChatMain {
		c.Status(http.StatusUnauthorized)
		return
	}
	if (sb.Purpose == services.SandboxPurposeSolo && h.Solo == nil) || (sb.Purpose != services.SandboxPurposeSolo && h.Eng == nil) {
		writePreviewMainError(c, errPreviewMainServiceUnavailable)
		return
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), link.ExpiresAt)
	defer cancel()
	updates, unsubscribe := h.subscribePreviewMain(link, sb)
	defer unsubscribe()
	var first previewMainUpdate
	select {
	case <-ctx.Done():
		c.Status(http.StatusGone)
		return
	case update, open := <-updates:
		if !open {
			writePreviewMainError(c, errPreviewMainServiceUnavailable)
			return
		}
		first = update
	}
	if first.err != nil {
		writePreviewMainError(c, first.err)
		return
	}
	u := upgrader
	u.Subprotocols = []string{appPreviewMainProtocol}
	conn, err := u.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	// Revocation cuts the socket immediately; the watcher also checks ticket
	// validity every ten seconds. No token/database lookup belongs on each tick.
	if h.GateShareSessions != nil {
		defer h.GateShareSessions.Register(link.TokenHash, conn)()
	}
	if active, _ := h.activeAppPreviewTicket(claims); active == nil {
		return
	}
	conn.SetReadLimit(1024)
	configureEventHeartbeat(conn, eventPongWait)
	go h.watchAppPreviewChat(ctx, cancel, conn, claims)
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	previous := first.snapshot
	if writeChatJSON(conn, previous.frameAfter(nil)) != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case update, open := <-updates:
			if !open {
				return
			}
			if update.err != nil {
				status := http.StatusServiceUnavailable
				if errors.Is(update.err, errPreviewMainUnavailable) {
					status = http.StatusConflict
				}
				_ = writeChatJSON(conn, gin.H{"type": "error", "status": status})
				return
			}
			if writeChatJSON(conn, update.snapshot.frameAfter(previous)) != nil {
				return
			}
			previous = update.snapshot
		}
	}
}
