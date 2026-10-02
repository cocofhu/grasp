package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

const headerShareVisitor = "X-Gate-Share-Visitor"

// publicLane resolves which dialogue lane a public credential talks on. A
// share token gets one lane per browser visitor id; a share drawer inherits
// the lane it was minted for; a logged-in drawer uses the node's own dialogue.
func (h *Handlers) publicLane(token string, lookup *gateshare.LookupResult, visitor string) (string, bool) {
	token = strings.TrimSpace(token)
	if embed.IsSessionToken(token) {
		if h.Embed != nil {
			if cl, ok := h.Embed.LookupSession(token); ok && cl.Kind == models.EmbedKindShare {
				return cl.Lane, true
			}
		}
		return "", true
	}
	if lookup == nil {
		return "", false
	}
	if h.Eng == nil || !h.Eng.VisitorLanesSupported() {
		return "", true
	}
	lane := gateshare.VisitorLane(lookup.Link.ID, visitor)
	return lane, lane != ""
}

// requestLane is publicLane for the X-Gate-Share-Visitor header; it writes
// 400 visitor_required when a share token arrives without a valid visitor id.
func (h *Handlers) requestLane(c *gin.Context, token string, lookup *gateshare.LookupResult) (string, bool) {
	lane, ok := h.publicLane(token, lookup, c.GetHeader(headerShareVisitor))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "visitor_required", "message": "缺少访客标识，请刷新页面后重试"})
	}
	return lane, ok
}

func writeVisitorsFull(c *gin.Context, err error) bool {
	if !errors.Is(err, engine.ErrVisitorsFull) {
		return false
	}
	c.JSON(http.StatusTooManyRequests, gin.H{"error": "visitors_full", "message": "同时对话的访客已满，请稍后再试"})
	return true
}

// publicLaneTurns is the transcript a lane sees.
func (h *Handlers) publicLaneTurns(lookup *gateshare.LookupResult, producerID, lane string) []models.ReactMessage {
	if lane != "" && h.Eng != nil {
		return h.Eng.VisitorTurns(lookup.Link.ID, lane, lookup.Link.RunID, producerID)
	}
	if conv := h.publicConversation(lookup.Link.RunID, producerID); conv != nil {
		return conv.Turns()
	}
	return nil
}

// publicLaneSession fills the queue / busy / live fields of preview extras.
func (h *Handlers) publicLaneSession(ex *gateshare.PreviewExtras, runID, producerID, lane string) {
	if h.Eng == nil || producerID == "" {
		return
	}
	if lane == "" {
		if snap, ok := h.Eng.ReviewSessionSnapshotFor(runID, producerID); ok {
			ex.Waiting = snap.Waiting
			ex.QueueItems = snap.Items
			ex.ActiveItem = snap.ActiveItem
			ex.SessionBusy = snap.Busy || snap.Waiting > 0 || !h.Eng.ReviewSessionReady(runID, producerID)
		} else {
			waiting, thinking := h.Eng.ReviewSessionState(runID, producerID)
			ex.Waiting = waiting
			ex.SessionBusy = thinking || waiting > 0 || !h.Eng.ReviewSessionReady(runID, producerID)
		}
		if ex.SessionBusy {
			ex.LiveEvents = h.publicLiveACP(runID, producerID)
		}
		return
	}
	if snap, ok := h.Eng.VisitorSessionSnapshot(runID, producerID, lane); ok {
		ex.Waiting = snap.Waiting
		ex.QueueItems = snap.Items
		ex.ActiveItem = snap.ActiveItem
		ex.SessionBusy = snap.Busy || snap.Waiting > 0
	}
	if ex.SessionBusy {
		ex.LiveEvents = h.Eng.VisitorLiveEvents(runID, producerID, lane)
	}
}

// publicLaneQueue returns the active item and queue a lane's image indexes
// continue into.
func (h *Handlers) publicLaneQueue(runID, producerID, lane string) (map[string]any, []map[string]any) {
	if h.Eng == nil {
		return nil, nil
	}
	var snap engine.ReviewSessionSnapshot
	var ok bool
	if lane == "" {
		snap, ok = h.Eng.ReviewSessionSnapshotFor(runID, producerID)
	} else {
		snap, ok = h.Eng.VisitorSessionSnapshot(runID, producerID, lane)
	}
	if !ok {
		return nil, nil
	}
	return snap.ActiveItem, snap.Items
}
