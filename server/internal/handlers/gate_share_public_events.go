package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const publicEventsAuthTimeout = 5 * time.Second

type publicEventsAuth struct {
	Token string `json:"token"`
}

// PublicGateEvents streams leak-free review/ACP frames for the share-link
// workbench. Token is sent as the first JSON message (never in the URL).
func (h *Handlers) PublicGateEvents(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketPreview) {
		return
	}
	if h.GateShare == nil || h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Err(err).Msg("public gate events websocket upgrade failed")
		return
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(publicEventsAuthTimeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var auth publicEventsAuth
	if json.Unmarshal(data, &auth) != nil {
		_ = conn.WriteJSON(gin.H{"type": "error", "status": "invalid"})
		return
	}
	token := strings.TrimSpace(auth.Token)
	if token == "" || !gateshare.ValidCredentialShape(token) {
		_ = conn.WriteJSON(gin.H{"type": "error", "status": "invalid"})
		return
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st != models.ShareLinkStateActive {
		status := st
		if status == "" {
			status = "invalid"
		}
		_ = conn.WriteJSON(gin.H{"type": "error", "status": status})
		return
	}
	producerID := h.publicDialogueProducerID(lookup)
	if producerID == "" {
		producerID = strings.TrimSpace(lookup.Link.NodeID)
	}
	if producerID == "" {
		_ = conn.WriteJSON(gin.H{"type": "error", "status": "invalid"})
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	// Subscribe before advertising ready so events published right after the
	// client sees ready are not lost to a subscribe race.
	runID := lookup.Link.RunID
	ch, unsub := h.Eng.Broker().Subscribe(runID)
	defer unsub()
	if err := conn.WriteJSON(gin.H{"type": "ready"}); err != nil {
		return
	}
	h.seedPublicDialogue(conn, lookup, producerID)

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	for {
		select {
		case msg, open := <-ch:
			if !open {
				return
			}
			out, ok := gateshare.FilterPublicBrokerFrame(msg, producerID)
			if !ok {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, out); err != nil {
				return
			}
		case <-ping.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

func (h *Handlers) seedPublicDialogue(conn *websocket.Conn, lookup *gateshare.LookupResult, producerID string) {
	if h.Eng == nil || lookup == nil || conn == nil {
		return
	}
	runID := lookup.Link.RunID
	if snap, ok := h.Eng.ReviewSessionSnapshotFor(runID, producerID); ok {
		payload := map[string]any{
			"type":    "review",
			"runId":   runID,
			"nodeId":  producerID,
			"event":   "queue_state",
			"waiting": snap.Waiting,
			"items":   snap.Items,
			"busy":    snap.Busy,
		}
		if snap.ActiveItem != nil {
			payload["activeItem"] = snap.ActiveItem
		}
		raw, err := json.Marshal(payload)
		if err == nil {
			if out, ok := gateshare.FilterPublicBrokerFrame(raw, producerID); ok {
				_ = conn.WriteMessage(websocket.TextMessage, out)
			}
		}
	}
	if ev := h.publicLiveACP(runID, producerID); len(ev) > 0 {
		raw, err := json.Marshal(map[string]any{
			"type":   "acp",
			"runId":  runID,
			"nodeId": producerID,
			"events": ev,
			"busy":   true,
		})
		if err == nil {
			if out, ok := gateshare.FilterPublicBrokerFrame(raw, producerID); ok {
				_ = conn.WriteMessage(websocket.TextMessage, out)
			}
		}
	}
}
