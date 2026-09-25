package handlers

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pagebridge"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const wsWriteTimeout = 10 * time.Second

// wsWriter serializes writes on a websocket shared by the event loop and
// page-bridge frames sent from tool-call goroutines.
type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) write(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	return w.conn.WriteMessage(websocket.TextMessage, b)
}

// sessionTurnOwner is the page owner for a logged-in request.
func sessionTurnOwner(c *gin.Context) string {
	return pagebridge.UserOwner(c.GetString("auth_username"))
}

// publicTurnOwner is the page owner behind a share token or drawer token. A
// drawer minted by a logged-in user belongs to that user, so the same person
// can chat from the workbench and have the agent act on their preview page.
func (h *Handlers) publicTurnOwner(token string) string {
	token = strings.TrimSpace(token)
	if !embed.IsSessionToken(token) {
		return pagebridge.TokenOwner("share", token)
	}
	if h.Embed != nil {
		if c, ok := h.Embed.LookupSession(token); ok && c.Kind == models.EmbedKindSession {
			if o := pagebridge.UserOwner(c.Username); o != "" {
				return o
			}
		}
	}
	return pagebridge.TokenOwner("embed", token)
}

// publicPageFrame is a drawer → server page-bridge frame.
//
//	{"type":"page_control","on":true,"visible":true}
//	{"type":"page_result","id":"c7","ok":true,"state":{...}}
type publicPageFrame struct {
	Type    string         `json:"type"`
	On      bool           `json:"on"`
	Visible bool           `json:"visible"`
	ID      string         `json:"id"`
	OK      bool           `json:"ok"`
	Error   string         `json:"error"`
	Note    string         `json:"note"`
	State   map[string]any `json:"state"`
}

func handlePublicPageFrame(pc *pagebridge.Conn, data []byte) {
	if pc == nil {
		return
	}
	var f publicPageFrame
	if json.Unmarshal(data, &f) != nil {
		return
	}
	switch f.Type {
	case "page_control":
		pc.SetControl(f.On, f.Visible)
	case "page_result":
		if f.ID != "" {
			pc.Deliver(f.ID, pagebridge.Result{OK: f.OK, Error: f.Error, Note: f.Note, State: f.State})
		}
	}
}
