package handlers

import (
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Keep this schema metadata-only. Never accept arbitrary error messages,
// URLs, native frames, prompts or headers from the browser into server logs.
type soloChatDiagnostic struct {
	ViewID         string `json:"viewId"`
	Event          string `json:"event"`
	Connection     int64  `json:"connection"`
	Request        int64  `json:"request"`
	ElapsedMS      int64  `json:"elapsedMs"`
	Received       int64  `json:"received"`
	Processed      int64  `json:"processed"`
	Buffered       int64  `json:"buffered"`
	PendingHistory int64  `json:"pendingHistory"`
	LastReceived   int64  `json:"lastReceived"`
	Acknowledged   int64  `json:"acknowledged"`
	HighWatermark  int64  `json:"highWatermark"`
	QueueMS        int64  `json:"queueMs"`
	ResponseMS     int64  `json:"responseMs"`
	HTTPStatus     int64  `json:"httpStatus"`
	CloseCode      int64  `json:"closeCode"`
	HistoryReady   bool   `json:"historyReady"`
	Busy           bool   `json:"busy"`
	ServerBusy     bool   `json:"serverBusy"`
	Turns          int64  `json:"turns"`
	AgentTurns     int64  `json:"agentTurns"`
	LiveParts      int64  `json:"liveParts"`
	LiveTextChars  int64  `json:"liveTextChars"`
}

var soloDiagnosticViewID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (h *Handlers) SoloChatDiagnostics(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	if _, err := h.Solo.Require(c.Param("id"), user); err != nil {
		writeSoloErr(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	var body soloChatDiagnostic
	if err := c.ShouldBindJSON(&body); err != nil || !soloDiagnosticViewID.MatchString(body.ViewID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_chat_diagnostic"})
		return
	}
	switch body.Event {
	case "history_slow", "history_done", "history_failed", "ws_connecting", "ws_waiting", "ws_snapshot", "ws_closed", "replay_drained", "replay_caught_up", "sequence_gap", "frame_failed":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_chat_diagnostic"})
		return
	}
	for _, n := range []int64{body.Connection, body.Request, body.ElapsedMS, body.Received, body.Processed, body.Buffered, body.PendingHistory, body.LastReceived, body.Acknowledged, body.HighWatermark, body.QueueMS, body.ResponseMS, body.HTTPStatus, body.CloseCode, body.Turns, body.AgentTurns, body.LiveParts, body.LiveTextChars} {
		if n < -1 || n > 1<<53-1 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_chat_diagnostic"})
			return
		}
	}
	log.Info().Str("session", c.Param("id")).Interface("diagnostic", body).Msg("solo browser diagnostic")
	c.Status(http.StatusNoContent)
}

func traceSoloHistory(c *gin.Context) func(int, bool) {
	started, requestID := time.Now(), uuid.NewString()
	log.Info().Str("session", c.Param("id")).Str("request_id", requestID).Msg("solo history read started")
	return func(count int, hasMore bool) {
		log.Info().Str("session", c.Param("id")).Str("request_id", requestID).
			Int("status", c.Writer.Status()).Int("messages", count).Bool("has_more", hasMore).
			Bool("cancelled", c.Request.Context().Err() != nil).Dur("elapsed", time.Since(started)).Msg("solo history read finished")
	}
}
