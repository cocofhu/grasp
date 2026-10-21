package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pagebridge"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// RunEvents streams a run's live updates over WebSocket. On connect it sends a
// "snapshot" frame, then relays broker messages: "trace" (state-trace entries),
// "status" (run status changes), "react" (react-dialogue turns), "acp"
// (a running node's in-progress agent events, pushed via publishAcp), and
// "review" (queue_state / turn_begin / turn_done / error for the platform
// review session controller).
//
// Client → server control frames (SandboxChat-aligned):
//
//	{"type":"review_chat","nodeId":"<producer>","content":"…","images":[],"annotations":[],"gateNodeId":""}
//	{"type":"review_cancel","nodeId":"<producer>"}
//
// review_chat with gateNodeId set enqueues via GateReactRevise semantics;
// otherwise via node-inline EnqueueReviewTurn.
func (h *Handlers) RunEvents(c *gin.Context) {
	owner := ""
	if h.Auth != nil {
		sess, ok := h.Auth.RequireSession(c)
		if !ok {
			return
		}
		owner = pagebridge.UserOwner(sess.Username)
	}
	runID := c.Param("id")
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade writes its own HTTP error response; log at debug since a
		// failed handshake is usually a client/proxy issue, not a server fault.
		log.Debug().Str("run_id", runID).Err(err).Msg("run events websocket upgrade failed")
		return
	}
	defer func() { _ = conn.Close() }()
	w := &wsWriter{conn: conn}

	ch, unsub := h.Eng.Broker().Subscribe(runID)
	defer unsub()

	// Send a snapshot of the current run on connect.
	if run, ok := h.Runs.Get(runID); ok {
		if b, err := json.Marshal(gin.H{"type": "snapshot", "run": h.runDetailDTO(run)}); err == nil {
			_ = w.write(b)
		}
	}
	// Re-emit authoritative queue/busy so refresh can resume mid-stream.
	h.Eng.BroadcastReviewSessions(runID)
	if h.PageBridge != nil && owner != "" {
		defer h.PageBridge.Watch(runID, owner, w.write)()
	}

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	// Reader pump: detect disconnect + handle review control frames.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				_ = conn.Close()
				return
			}
			h.handleRunWSControl(runID, owner, data, w.write)
		}
	}()

	for {
		select {
		case <-readDone:
			return
		case msg, open := <-ch:
			if !open {
				return
			}
			if err := w.write(msg); err != nil {
				return
			}
		case <-ping.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

// reply sends a frame to the connection that sent data only.
func (h *Handlers) handleRunWSControl(runID, owner string, data []byte, reply func([]byte) error) {
	var m struct {
		// ClientMessageID: optional client UUID for a chat turn (see reactReplyBody).
		ClientMessageID string                   `json:"clientMessageId"`
		Type            string                   `json:"type"`
		NodeID          string                   `json:"nodeId"`
		Content         string                   `json:"content"`
		Images          []models.PromptImage     `json:"images"`
		Annotations     []models.ReactAnnotation `json:"annotations"`
		GateNodeID      string                   `json:"gateNodeId"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	switch m.Type {
	case "review_cancel":
		nodeID := strings.TrimSpace(m.NodeID)
		if nodeID == "" {
			return
		}
		clearQueue := true
		if run, ok := h.Runs.Get(runID); ok {
			if n := run.Graph.FindNode(nodeID); n != nil && n.Caps.Clarify() {
				clearQueue = false
			}
		}
		var err error
		if clearQueue {
			err = h.Eng.CancelReviewSession(runID, nodeID)
		} else {
			err = h.Eng.CancelClarifyTurn(runID, nodeID)
		}
		if err != nil {
			h.publishReviewWSError(runID, nodeID, err.Error())
		}
	case "review_chat", "chat":
		nodeID := strings.TrimSpace(m.NodeID)
		if nodeID == "" {
			return
		}
		gateID := strings.TrimSpace(m.GateNodeID)
		if gateID != "" {
			if err := h.Eng.GateReactReviseAs(owner, runID, gateID, m.Content, m.Images, m.Annotations); err != nil {
				h.publishReviewWSError(runID, nodeID, err.Error())
			}
			return
		}
		// Classic react → clarify FIFO; review-capable nodes → review FIFO.
		if run, ok := h.Runs.Get(runID); ok {
			if n := run.Graph.FindNode(nodeID); n != nil && n.Caps.Clarify() {
				cid := m.ClientMessageID
				if !validClientMessageID(cid) {
					cid = "" // opaque best-effort id; an invalid one is ignored
				}
				if _, err := h.Eng.EnqueueClarifyTurnWithIDAs(owner, runID, nodeID, cid, m.Content, m.Images, m.Annotations); err != nil {
					var ve *engine.ClarifyVisitEndedError
					if errors.As(err, &ve) {
						// The reject carries the sender's own content, so it
						// goes back on this connection only, never the broker.
						h.replyReviewWSFrame(reply, runID, nodeID, ve.FrameExtra(true))
					} else {
						h.publishReviewWSError(runID, nodeID, err.Error())
					}
				}
				return
			}
		}
		if _, err := h.Eng.EnqueueReviewTurnAs(owner, runID, nodeID, m.Content, m.Images, m.Annotations, "node", ""); err != nil {
			h.publishReviewWSError(runID, nodeID, err.Error())
		}
	}
}

// publishReviewWSError pushes a type:"review" event:"error" frame so WS clients
// see enqueue/cancel failures (aligned with SandboxChat error frames).
func (h *Handlers) publishReviewWSError(runID, nodeID, message string) {
	h.publishReviewWSFrame(runID, nodeID, map[string]any{"message": message})
}

// publishReviewWSFrame is publishReviewWSError with extra fields (code,
// autoRetryOnResume, dropped, ...); extra keys may not change type/event.
func (h *Handlers) publishReviewWSFrame(runID, nodeID string, extra map[string]any) {
	msg, err := reviewErrorFrame(runID, nodeID, extra)
	if err != nil {
		return
	}
	h.Eng.Broker().Publish(runID, msg)
}

// replyReviewWSFrame sends a review error frame to one connection only.
func (h *Handlers) replyReviewWSFrame(reply func([]byte) error, runID, nodeID string, extra map[string]any) {
	msg, err := reviewErrorFrame(runID, nodeID, extra)
	if err != nil || reply == nil {
		return
	}
	_ = reply(msg)
}

func reviewErrorFrame(runID, nodeID string, extra map[string]any) ([]byte, error) {
	frame := map[string]any{"runId": runID, "nodeId": nodeID}
	for k, v := range extra {
		frame[k] = v
	}
	frame["type"], frame["event"] = "review", "error"
	return json.Marshal(frame)
}
