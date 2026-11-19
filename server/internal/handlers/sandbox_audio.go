package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var audioDialer = &websocket.Dialer{HandshakeTimeout: 5 * time.Second}

// serveDesktopAudio relays the sandbox's PCM frames (vnc-ws /audio) to the
// viewer. The upstream is dialed before upgrading so an image without sound
// answers with a plain error instead of an empty stream.
func serveDesktopAudio(c *gin.Context, upstreamURL string) {
	upstream, _, err := audioDialer.DialContext(c.Request.Context(), upstreamURL, nil)
	if err != nil {
		c.String(http.StatusServiceUnavailable, "desktop audio unavailable")
		return
	}
	defer func() { _ = upstream.Close() }()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Err(err).Msg("desktop audio websocket upgrade failed")
		return
	}
	defer func() { _ = conn.Close() }()

	go func() {
		defer func() { _ = upstream.Close() }()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	for {
		msgType, data, err := upstream.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.BinaryMessage {
			continue
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			return
		}
	}
}
