package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const appPreviewMainProtocol = "grasp-preview-main"

// PublicAppPreviewMainEvents pushes the same sanitized snapshot as main-chat.
// A main-mode ticket grants no access to raw Solo or workflow event streams.
func (h *Handlers) PublicAppPreviewMainEvents(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketTicket) {
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

	// Subscribe before taking the seed so a turn cannot disappear between them.
	var soloEvents <-chan chatsession.Event
	var workflowEvents <-chan []byte
	var unsubscribe func()
	if sb.Purpose == services.SandboxPurposeSolo && h.Solo != nil {
		var err error
		soloEvents, unsubscribe, err = h.Solo.Subscribe(sb.ThreadID, link.CreatedBy, -1)
		if err != nil {
			c.Status(http.StatusConflict)
			return
		}
	} else if h.Eng != nil {
		workflowEvents, unsubscribe = h.Eng.Broker().Subscribe(link.RunID)
	} else {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	defer unsubscribe()
	seed, err := h.appPreviewMainHistory(link, sb)
	if err != nil {
		c.Status(http.StatusConflict)
		return
	}
	u := upgrader
	u.Subprotocols = []string{appPreviewMainProtocol}
	conn, err := u.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(1024)
	configureEventHeartbeat(conn, eventPongWait)
	ctx, cancel := context.WithDeadline(c.Request.Context(), link.ExpiresAt)
	defer cancel()
	go h.watchAppPreviewChat(ctx, cancel, conn, claims)
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	write := func(history *previewMainHistory) error {
		return writeChatJSON(conn, gin.H{"type": "snapshot", "history": history})
	}
	if write(seed) != nil {
		return
	}
	// Coalesce token bursts without losing tool-only/thought-only updates or
	// the final snapshot. A dropped subscription closes the socket for recovery.
	tick := time.NewTicker(125 * time.Millisecond)
	defer tick.Stop()
	dirty := false
	for {
		select {
		case <-ctx.Done():
			return
		case _, open := <-soloEvents:
			if !open {
				return
			}
			dirty = true
		case raw, open := <-workflowEvents:
			if !open {
				return
			}
			if isDialogueFrameFor(raw, link.NodeID, "") {
				dirty = true
			}
		case <-tick.C:
			if !dirty {
				continue
			}
			if active, _ := h.activeAppPreviewTicket(claims); active == nil {
				return
			}
			history, err := h.appPreviewMainHistory(link, sb)
			if err != nil || write(history) != nil {
				return
			}
			dirty = false
		}
	}
}
